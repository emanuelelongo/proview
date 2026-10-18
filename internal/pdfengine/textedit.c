//go:build cgo && !nocgo && !extlib

/* Native text editing prototype. MuPDF exceptions never cross Go frames. */
#include "textedit.h"
#include "mupdf/fitz.h"
#include "mupdf/pdf.h"
#include <math.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

typedef struct pv_text_filter_state pv_text_filter_state;
typedef struct {
    int blend, soft_mask, fill_alpha, group;
} pv_text_effects;

static int pv_has_text_effects(pv_text_effects effects)
{
    return effects.blend || effects.soft_mask || effects.fill_alpha || effects.group;
}
typedef struct {
    float character, word, font_size;
    int single_byte_space;
} pv_text_spacing;
typedef struct {
    fz_stext_char **chars;
    int *seen;
    int count, pending, inserted;
    pv_text_filter_state *filters;
    pdf_obj *replacement;
    fz_matrix page_ctm;
    fz_rect page_box;
    const int *replacement_codes;
    int replacement_count, have_spacing;
    float dx, dy, character_spacing, word_spacing;
    pdf_obj *form_spacing;
    pv_text_effects form_effects;
    int duplicate, obstacle_count;
    fz_rect *obstacles;
    fz_point *obstacle_origins;
} pv_text_target;

struct pv_text_filter_state {
    pv_text_filter_state *next;
    pv_text_target *target;
    pdf_sanitize_filter_options options;
    pdf_processor *processor;
    int kept, spacing_depth, has_saved_spacing;
    float saved_font_size,saved_character,saved_word;
    pv_text_spacing spacing, spacing_stack[64];
    pv_text_effects effects, effects_stack[64];
    void (*original_gs_begin)(fz_context *, pdf_processor *, const char *, pdf_obj *);
    void (*original_Tc)(fz_context *, pdf_processor *, float);
    void (*original_Tw)(fz_context *, pdf_processor *, float);
    void (*original_Tf)(fz_context *, pdf_processor *, const char *, pdf_font_desc *, float);
    void (*original_q)(fz_context *, pdf_processor *);
    void (*original_Q)(fz_context *, pdf_processor *);
    void (*original_dquote)(fz_context *, pdf_processor *, float, float, char *, size_t);
    void (*original_Do_form)(fz_context *, pdf_processor *, const char *, pdf_obj *);
};

static void pv_text_silent(void *opaque, const char *message)
{
    (void)opaque; (void)message;
}

static void pv_text_save(fz_context *ctx, pdf_document *doc, pv_result *result)
{
    fz_buffer *buffer = NULL;
    fz_output *output = NULL;
    fz_var(buffer); fz_var(output);
    fz_try(ctx) {
        pdf_write_options write = pdf_default_write_options;
        write.do_garbage = 4; write.do_compress = 1;
        buffer = fz_new_buffer(ctx, 1024);
        output = fz_new_output_with_buffer(ctx, buffer);
        pdf_write_document(ctx, doc, output, &write);
        fz_close_output(ctx, output);
        unsigned char *bytes;
        result->length = fz_buffer_storage(ctx, buffer, &bytes);
        result->bytes = malloc(result->length);
        if (!result->bytes) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate text editing output");
        memcpy(result->bytes, bytes, result->length);
    }
    fz_always(ctx) { fz_drop_output(ctx, output); fz_drop_buffer(ctx, buffer); }
    fz_catch(ctx) { fz_rethrow(ctx); }
}

/* Wrap only public processor callbacks. Context-local lookup keeps concurrent
 * edits isolated without relying on MuPDF's private sanitize-processor layout. */
static pv_text_filter_state *pv_spacing_state(fz_context *ctx, pdf_processor *processor)
{
    pv_text_target *target = fz_user_context(ctx);
    for (pv_text_filter_state *state = target->filters; state; state = state->next)
        if (state->processor == processor) return state;
    fz_throw(ctx, FZ_ERROR_GENERIC, "Missing text spacing filter state");
}
static void pv_spacing_Tc(fz_context *ctx,pdf_processor *proc,float value)
{
    pv_text_filter_state *state=pv_spacing_state(ctx,proc);
    state->spacing.character=value;
    state->original_Tc(ctx,proc,value);
}
static void pv_spacing_Tw(fz_context *ctx,pdf_processor *proc,float value)
{
    pv_text_filter_state *state=pv_spacing_state(ctx,proc);
    state->spacing.word=value;
    state->original_Tw(ctx,proc,value);
}
static void pv_spacing_Tf(fz_context *ctx,pdf_processor *proc,const char *name,pdf_font_desc *font,float size)
{
    pv_text_filter_state *state=pv_spacing_state(ctx,proc);
    state->spacing.font_size=size;
    state->spacing.single_byte_space=0;
    /* PDF word spacing applies only to the single-byte character code 32,
     * not to a two-byte CID that happens to map to a Unicode space. */
    if (font && font->encoding)
        for (int i=0;i<font->encoding->codespace_len;++i)
            if (font->encoding->codespace[i].n==1 && font->encoding->codespace[i].low<=32 && font->encoding->codespace[i].high>=32)
                state->spacing.single_byte_space=1;
    state->original_Tf(ctx,proc,name,font,size);
}
static void pv_spacing_q(fz_context *ctx,pdf_processor *proc)
{
    pv_text_filter_state *state=pv_spacing_state(ctx,proc);
    if(state->spacing_depth==64) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Text graphics state nesting is too deep for text editing");
    state->effects_stack[state->spacing_depth]=state->effects;
    state->spacing_stack[state->spacing_depth++]=state->spacing;
    state->original_q(ctx,proc);
}
static void pv_spacing_Q(fz_context *ctx,pdf_processor *proc)
{
    pv_text_filter_state *state=pv_spacing_state(ctx,proc);
    if(state->spacing_depth) {
        state->spacing=state->spacing_stack[--state->spacing_depth];
        state->effects=state->effects_stack[state->spacing_depth];
    }
    state->original_Q(ctx,proc);
}
static void pv_spacing_dquote(fz_context *ctx,pdf_processor *proc,float word,float character,char *text,size_t length)
{
    pv_text_filter_state *state=pv_spacing_state(ctx,proc);
    state->spacing.word=word;state->spacing.character=character;
    state->original_dquote(ctx,proc,word,character,text,length);
}
static void pv_text_gs(fz_context *ctx,pdf_processor *proc,const char *name,pdf_obj *gs)
{
    pv_text_filter_state *state=pv_spacing_state(ctx,proc);
    pdf_obj *blend=pdf_dict_get(ctx,gs,PDF_NAME(BM));
    pdf_obj *mask=pdf_dict_get(ctx,gs,PDF_NAME(SMask));
    if (blend) state->effects.blend=!pdf_name_eq(ctx,blend,PDF_NAME(Normal));
    if (mask) state->effects.soft_mask=!pdf_name_eq(ctx,mask,PDF_NAME(None));
    if (pdf_dict_get(ctx,gs,PDF_NAME(ca)))
        state->effects.fill_alpha=pdf_dict_get_real(ctx,gs,PDF_NAME(ca))!=1;
    state->original_gs_begin(ctx,proc,name,gs);
}
static void pv_spacing_Do_form(fz_context *ctx,pdf_processor *proc,const char *name,pdf_obj *form)
{
    pv_text_filter_state *state=pv_spacing_state(ctx,proc);
    /* MuPDF 1.24.9 filters each Form with a fresh text state. Do not pretend
     * inherited spacing has been preserved across that boundary. Local Form
     * spacing is supported, and the parent's state remains unchanged. */
    if (state->spacing.character!=0 || state->spacing.word!=0)
        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Inherited Form text spacing is not supported for text editing");
    pv_text_target *target=state->target;
    pdf_obj *previous=target->form_spacing;
    pv_text_effects previous_effects=target->form_effects;
    target->form_effects=state->effects;
    if (pdf_name_eq(ctx,pdf_dict_get(ctx,pdf_dict_get(ctx,form,PDF_NAME(Group)),PDF_NAME(S)),PDF_NAME(Transparency)))
        target->form_effects.group=1;
    target->form_spacing=pdf_dict_gets(ctx,form,"ProviewTextSpacing");
    fz_try(ctx) {state->original_Do_form(ctx,proc,name,form);}
    fz_always(ctx) {target->form_spacing=previous;target->form_effects=previous_effects;}
    fz_catch(ctx) {fz_rethrow(ctx);}
}

static int pv_text_remove(fz_context *ctx, void *opaque, int *ucs, int len,
    fz_matrix trm, fz_matrix ctm, fz_rect bbox)
{
    pv_text_filter_state *state = opaque;
    pv_text_target *target = state->target;
    fz_point origin = fz_transform_point((fz_point){trm.e, trm.f}, fz_concat(ctm, target->page_ctm));
    (void)bbox;
    for (int i = 0; i < target->count; ++i) {
        fz_stext_char *ch = target->chars[i];
        if (fabsf(origin.x - ch->origin.x) > 0.02f || fabsf(origin.y - ch->origin.y) > 0.02f)
            continue;
        if (len != 1 || ucs[0] != ch->c)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Ambiguous glyph or ligature in text editing");
        if (pv_has_text_effects(state->effects))
            fz_throw(ctx,FZ_ERROR_ARGUMENT,"Transparency effects on selected text are not supported for text editing");
        if (++target->seen[i] != 1)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Ambiguous duplicate source glyphs in text editing");
        fz_matrix matrix = fz_concat(fz_concat(trm, ctm), target->page_ctm);
        if (fabsf(matrix.a - ch->size) > 0.02f || fabsf(matrix.d + ch->size) > 0.02f ||
            fabsf(matrix.b) > 0.02f || fabsf(matrix.c) > 0.02f)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Scaled or skewed text is not supported");
        float character=0,word=0;
        if (state->has_saved_spacing) {
            float scale=matrix.a/state->saved_font_size;
            character=state->saved_character*scale;word=state->saved_word*scale;
        } else if (state->spacing.character!=0 || state->spacing.word!=0) {
            if (!isfinite(state->spacing.font_size) || fabsf(state->spacing.font_size)<.000001f)
                fz_throw(ctx,FZ_ERROR_ARGUMENT,"Cannot determine text spacing scale");
            float scale=matrix.a/state->spacing.font_size;
            character=state->spacing.character*scale;
            if(state->spacing.single_byte_space) word=state->spacing.word*scale;
        }
        if (!isfinite(character) || !isfinite(word))
            fz_throw(ctx,FZ_ERROR_ARGUMENT,"Invalid text spacing");
        if (target->have_spacing && (fabsf(character-target->character_spacing)>.00001f || fabsf(word-target->word_spacing)>.00001f))
            fz_throw(ctx,FZ_ERROR_ARGUMENT,"Mixed spacing within selected text is not supported for text editing");
        target->character_spacing=character;target->word_spacing=word;target->have_spacing=1;
        /* Preflight guarantees no live path at BT. If no previous glyph was
         * retained, flush the pending CTM before MuPDF can elide this whole BT.
         * n is emitted before the still-pending BT, with an empty path. */
        if (!state->kept) { state->processor->op_n(ctx,state->processor); state->kept = 1; }
        target->pending = 1;
        return target->duplicate ? 0 : 1;
    }
    state->kept = 1;
    return 0;
}

/* Conservative page-wide eligibility checks. This prototype accepts ordinary
 * filled text only, rather than approximating unsupported text effects. */
typedef struct {
    pdf_processor super;
    pdf_document *doc;
    pdf_obj *resources[32];
    int depth, recursion, path_active, in_text;
} pv_text_guard;

static void pv_guard_BT(fz_context *ctx,pdf_processor *p)
{ pv_text_guard *g=(pv_text_guard *)p;if(g->path_active)fz_throw(ctx,FZ_ERROR_ARGUMENT,"A live graphics path crosses text content");g->in_text=1; }
static void pv_guard_ET(fz_context *ctx,pdf_processor *p)
{ (void)ctx;((pv_text_guard *)p)->in_text=0; }
static void pv_guard_path(fz_context *ctx,pdf_processor *p)
{ pv_text_guard *g=(pv_text_guard *)p;if(g->in_text)fz_throw(ctx,FZ_ERROR_ARGUMENT,"Graphics paths inside text are not supported");g->path_active=1; }
static void pv_guard_xy(fz_context *ctx,pdf_processor *p,float x,float y)
{ (void)x;(void)y;pv_guard_path(ctx,p); }
static void pv_guard_curve(fz_context *ctx,pdf_processor *p,float a,float b,float c,float d,float e,float f)
{ (void)a;(void)b;(void)c;(void)d;(void)e;(void)f;pv_guard_path(ctx,p); }
static void pv_guard_rect(fz_context *ctx,pdf_processor *p,float a,float b,float c,float d)
{ (void)a;(void)b;(void)c;(void)d;pv_guard_path(ctx,p); }
static void pv_guard_paint(fz_context *ctx,pdf_processor *p)
{ (void)ctx;((pv_text_guard *)p)->path_active=0; }

static void pv_guard_push(fz_context *ctx, pdf_processor *proc, pdf_obj *res)
{
    pv_text_guard *g=(pv_text_guard *)proc;
    if (g->depth==32) fz_throw(ctx,FZ_ERROR_ARGUMENT,"PDF resource nesting is too deep for text editing");
    g->resources[g->depth++]=pdf_keep_obj(ctx,res);
}
static pdf_obj *pv_guard_pop(fz_context *ctx, pdf_processor *proc)
{
    pv_text_guard *g=(pv_text_guard *)proc;
    (void)ctx;
    return g->resources[--g->depth];
}
static void pv_guard_drop(fz_context *ctx,pdf_processor *proc)
{
    pv_text_guard *g=(pv_text_guard *)proc;
    while(g->depth) pdf_drop_obj(ctx,g->resources[--g->depth]);
}
static void pv_guard_render(fz_context *ctx,pdf_processor *proc,int mode)
{ (void)proc; if(mode!=0) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Only filled visible text is supported for text editing"); }
static void pv_guard_scale(fz_context *ctx,pdf_processor *proc,float value)
{ (void)proc; if(fabsf(value-100)>.001f) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Horizontal text scaling is not supported for text editing"); }
static void pv_guard_font(fz_context *ctx,pdf_processor *proc,const char *name,pdf_font_desc *font,float size)
{
    (void)proc;(void)name;(void)size;
    if (!font || !font->font) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Missing text font");
    fz_font_flags_t *flags=fz_font_flags(font->font);
    if (fz_font_t3_procs(ctx,font->font) || flags->fake_bold || flags->fake_italic || flags->ft_substitute)
        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Type 3, substituted or synthetic fonts are not supported for text editing");
}
static void pv_guard_form(fz_context *ctx,pdf_processor *proc,const char *name,pdf_obj *form)
{
    pv_text_guard *g=(pv_text_guard *)proc;
    (void)name;
    if (++g->recursion>16) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Recursive form content is not supported for text editing");
    int saved_path=g->path_active,saved_text=g->in_text;
    g->path_active=0;g->in_text=0;
    pdf_obj *res=pdf_xobject_resources(ctx,form);
    if(!res && g->depth) res=g->resources[g->depth-1];
    pdf_process_contents(ctx,proc,g->doc,res,form,NULL,NULL);
    g->path_active=saved_path;g->in_text=saved_text;
    --g->recursion;
}
static void pv_guard_tags(fz_context *ctx,pdf_processor *proc,const char *tag,pdf_obj *raw,pdf_obj *cooked)
{
    (void)proc;(void)tag;(void)raw;
    if(pdf_dict_get(ctx,cooked,PDF_NAME(ActualText)) || pdf_dict_get(ctx,cooked,PDF_NAME(Alt)))
        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Alternative text is not supported for text editing");
}
static void pv_check_text_page(fz_context *ctx,pdf_document *doc,pdf_page *page)
{
    pdf_processor *proc=pdf_new_processor(ctx,sizeof(pv_text_guard));
    pv_text_guard *g=(pv_text_guard *)proc;
    g->doc=doc;
    proc->drop_processor=pv_guard_drop;
    proc->push_resources=pv_guard_push;proc->pop_resources=pv_guard_pop;
    proc->op_Tr=pv_guard_render;proc->op_Tz=pv_guard_scale;
    proc->op_Tf=pv_guard_font;
    proc->op_Do_form=pv_guard_form;proc->op_BDC=pv_guard_tags;
    proc->op_BT=pv_guard_BT;proc->op_ET=pv_guard_ET;
    proc->op_m=pv_guard_xy;proc->op_l=pv_guard_xy;proc->op_c=pv_guard_curve;
    proc->op_v=pv_guard_rect;proc->op_y=pv_guard_rect;proc->op_re=pv_guard_rect;proc->op_h=pv_guard_path;
    proc->op_S=pv_guard_paint;proc->op_s=pv_guard_paint;proc->op_f=pv_guard_paint;proc->op_F=pv_guard_paint;proc->op_fstar=pv_guard_paint;
    proc->op_B=pv_guard_paint;proc->op_Bstar=pv_guard_paint;proc->op_b=pv_guard_paint;proc->op_bstar=pv_guard_paint;proc->op_n=pv_guard_paint;
    fz_try(ctx) {
        if(pdf_dict_get(ctx,pdf_dict_get(ctx,pdf_trailer(ctx,doc),PDF_NAME(Root)),PDF_NAME(StructTreeRoot)))
            fz_throw(ctx,FZ_ERROR_ARGUMENT,"Tagged PDFs are not yet supported for text editing");
        pdf_process_contents(ctx,proc,doc,pdf_page_resources(ctx,page),pdf_page_contents(ctx,page),NULL,NULL);
        pdf_close_processor(ctx,proc);
    }
    fz_always(ctx) {pdf_drop_processor(ctx,proc);}
    fz_catch(ctx) {fz_rethrow(ctx);}
}

/* Compare actual glyph bounds instead of full ascender/descender line boxes,
 * which can intersect in normally spaced paragraphs. Keep a separate origin
 * check because coincident source origins are ambiguous even for disjoint ink. */
static void pv_text_obstacles(fz_context *ctx,pv_text_target *target,fz_stext_page *layout,int start,int count)
{
    int total=0,index=0;
    for(fz_stext_block *b=layout->first_block;b;b=b->next)
        if(b->type==FZ_STEXT_BLOCK_TEXT)
            for(fz_stext_line *l=b->u.t.first_line;l;l=l->next)
                for(fz_stext_char *ch=l->first_char;ch;ch=ch->next) ++total;
    target->obstacles=fz_malloc_array(ctx,total,fz_rect);
    target->obstacle_origins=fz_malloc_array(ctx,total,fz_point);
    for(fz_stext_block *b=layout->first_block;b;b=b->next) {
        if(b->type!=FZ_STEXT_BLOCK_TEXT) continue;
        for(fz_stext_line *l=b->u.t.first_line;l;l=l->next) {
            for(fz_stext_char *ch=l->first_char;ch;ch=ch->next,++index) {
                if(!target->duplicate && index>=start && index-start<count) continue;
                if(ch->c<=32 || ch->c==160) continue;
                fz_rect box=fz_rect_from_quad(ch->quad);
                int gid=fz_encode_character(ctx,ch->font,ch->c);
                if(gid && !l->wmode && fabsf(l->dir.x-1)<.001f && fabsf(l->dir.y)<.001f) {
                    fz_matrix pen=fz_scale(ch->size,-ch->size);
                    pen.e=ch->origin.x;pen.f=ch->origin.y;
                    box=fz_bound_glyph(ctx,ch->font,gid,pen);
                }
                target->obstacles[target->obstacle_count]=box;
                target->obstacle_origins[target->obstacle_count++]=ch->origin;
            }
        }
    }
}

static void pv_text_check_overlap(fz_context *ctx,pv_text_target *target,fz_rect box,fz_point origin)
{
    for(int i=0;i<target->obstacle_count;++i) {
        fz_rect overlap=fz_intersect_rect(box,target->obstacles[i]);
        if ((overlap.x1-overlap.x0>.02f && overlap.y1-overlap.y0>.02f) ||
            (fabsf(origin.x-target->obstacle_origins[i].x)<=.02f && fabsf(origin.y-target->obstacle_origins[i].y)<=.02f))
            fz_throw(ctx,FZ_ERROR_ARGUMENT,"Text edit would overlap existing text");
    }
}

static void pv_text_replacement(fz_context *ctx,pdf_document *doc,pv_text_target *target)
{
    fz_text *text=NULL;
    fz_device *device=NULL;
    fz_buffer *content=NULL;
    pdf_obj *resources=NULL,*form=NULL,*form_ref=NULL;
    fz_var(text);fz_var(device);fz_var(content);fz_var(resources);fz_var(form);fz_var(form_ref);
    fz_try(ctx) {
        fz_stext_char *first = target->chars[0];
        if (fz_font_t3_procs(ctx, first->font))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Type 3 font not supported by text editing");
        if (!pdf_font_writing_supported(ctx, first->font))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Original font cannot be written by text editing");
        /* Isolate emitted font resource names from the original page resources. */
        resources = pdf_new_dict(ctx, doc, 2);
        content = fz_new_buffer(ctx, 256);
        /* Reset inherited text spacing in the isolated replacement Form.
         * The explicit glyph origins already include the effective spacing. */
        fz_append_string(ctx,content,"q\nBT 0 Tc 0 Tw ET\n");
        device = pdf_new_pdf_device(ctx, doc, fz_invert_matrix(target->page_ctm), resources, content);
        float rgb[] = {((first->color >> 16) & 255) / 255.f, ((first->color >> 8) & 255) / 255.f, (first->color & 255) / 255.f};
        fz_matrix pen = fz_scale(first->size, -first->size);
        pen.e = first->origin.x + target->dx; pen.f = first->origin.y + target->dy;
        for (int i = 0; i < target->replacement_count; ++i) {
            if ((target->replacement_codes[i] > 0x024f && target->replacement_codes[i] != 0x20ac) || target->replacement_codes[i] < 32)
                fz_throw(ctx,FZ_ERROR_ARGUMENT,"Only simple Latin replacement text is supported");
            int gid = fz_encode_character(ctx, first->font, target->replacement_codes[i]);
            if (!gid)
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Original font lacks replacement glyph U+%04X", target->replacement_codes[i]);
            /* Subsets can retain a Unicode cmap entry for a removed glyph.
             * A nonzero GID alone is therefore not evidence of usable artwork. */
            fz_rect glyph_box = fz_bound_glyph(ctx, first->font, gid, fz_identity);
            /* MuPDF caches empty glyph bounds as a 1e-7 by 1e-7 rectangle. */
            if (target->replacement_codes[i] != 32 && (fz_is_empty_rect(glyph_box) ||
                (glyph_box.x1 - glyph_box.x0 < 0.000001f && glyph_box.y1 - glyph_box.y0 < 0.000001f)))
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Original font lacks replacement outline U+%04X", target->replacement_codes[i]);
            /* One positioned span per glyph avoids MuPDF's integer TJ
             * rounding of fractional spacing between glyphs. */
            if(target->replacement_codes[i]!=32)
                pv_text_check_overlap(ctx,target,fz_bound_glyph(ctx,first->font,gid,pen),(fz_point){pen.e,pen.f});
            text=fz_new_text(ctx);
            fz_show_glyph(ctx, text, first->font, pen, gid, target->replacement_codes[i], 0, 0, FZ_BIDI_NEUTRAL, FZ_LANG_UNSET);
            fz_fill_text(ctx,device,text,fz_identity,fz_device_rgb(ctx),rgb,1,fz_default_color_params);
            fz_drop_text(ctx,text);text=NULL;
            pen.e += first->size * fz_advance_glyph(ctx, first->font, gid, 0) + target->character_spacing;
            if (target->replacement_codes[i]==32) pen.e += target->word_spacing;
        }
        fz_close_device(ctx, device);
        fz_drop_device(ctx, device); device = NULL;
        fz_append_string(ctx,content,"Q\n");
        form = pdf_new_dict(ctx, doc, 5);
        pdf_dict_put(ctx, form, PDF_NAME(Type), PDF_NAME(XObject));
        pdf_dict_put(ctx, form, PDF_NAME(Subtype), PDF_NAME(Form));
        pdf_dict_put_rect(ctx, form, PDF_NAME(BBox), target->page_box);
        pdf_dict_put(ctx, form, PDF_NAME(Resources), resources);
        /* Glyph positions encode the appearance explicitly. Retain the
         * intended spacing for later Proview edits, including word spacing
         * after the emitted font has become a multibyte CID font. */
        pdf_obj *spacing=pdf_new_array(ctx,doc,3);
        pdf_dict_puts_drop(ctx,form,"ProviewTextSpacing",spacing);
        pdf_array_push_real(ctx,spacing,first->size);
        pdf_array_push_real(ctx,spacing,target->character_spacing);
        pdf_array_push_real(ctx,spacing,target->word_spacing);
        form_ref = pdf_add_stream(ctx, doc, content, form, 0);
        target->replacement = form_ref;form_ref=NULL;

    }
    fz_always(ctx) {
        fz_drop_device(ctx,device);fz_drop_text(ctx,text);fz_drop_buffer(ctx,content);
        pdf_drop_obj(ctx,resources);pdf_drop_obj(ctx,form);pdf_drop_obj(ctx,form_ref);
    }
    fz_catch(ctx) {fz_rethrow(ctx);}
}

/* The sanitize filter wraps this callback in q/Q after the source ET.
 * Keep the current clip and paint order, while undoing the local CTM because
 * the replacement Form is expressed in page PDF coordinates. */
static void pv_text_insert(fz_context *ctx, void *opaque, pdf_document *doc,
    pdf_processor *chain, fz_matrix ctm)
{
    pv_text_filter_state *state = opaque;
    pv_text_target *target = state->target;
    if (!target->pending) { state->kept = 0; return; }
    /* Insertion follows ET. Do not inherit effects enabled after the selected
     * glyphs within that same text object. The surrounding source stays intact. */
    if (pv_has_text_effects(state->effects))
        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Transparency changes within selected text object are not supported for text editing");
    if (target->inserted) fz_throw(ctx, FZ_ERROR_ARGUMENT, "Text spans multiple text objects");
    for (int i = 0; i < target->count; ++i)
        if (target->seen[i] != 1) fz_throw(ctx, FZ_ERROR_ARGUMENT, "Selection spans multiple text objects");
    pv_text_replacement(ctx,doc,target);
    pdf_obj *resources = pdf_processor_pop_resources(ctx, chain);
    /* Put it straight back before any throwing operation. */
    pdf_processor_push_resources(ctx, chain, resources);
    pdf_drop_obj(ctx, resources);
    pdf_obj *xobjects = pdf_dict_get(ctx, resources, PDF_NAME(XObject));
    if (!xobjects) xobjects = pdf_dict_put_dict(ctx, resources, PDF_NAME(XObject), 1);
    char name[64]; int suffix = 0;
    do { snprintf(name, sizeof(name), "ProviewCorrection%d", suffix++); }
    while (pdf_dict_gets(ctx, xobjects, name));
    pdf_dict_puts(ctx, xobjects, name, target->replacement);
    fz_matrix inverse = fz_invert_matrix(ctm);
    chain->op_cm(ctx, chain, inverse.a, inverse.b, inverse.c, inverse.d, inverse.e, inverse.f);
    /* The surrounding q/Q isolates these resets from later source text. Keep
     * the invocation state explicit so subsequent edits do not inherit Tc/Tw. */
    chain->op_BT(ctx,chain);
    chain->op_Tc(ctx,chain,0);chain->op_Tw(ctx,chain,0);
    chain->op_ET(ctx,chain);
    chain->op_Do_form(ctx, chain, name, target->replacement);
    target->pending = 0;
    state->kept = 0;
    target->inserted = 1;
    (void)doc;
}

static pdf_processor *pv_make_text_filter(fz_context *ctx,pdf_document *doc,pdf_processor *chain,
    int struct_parents,fz_matrix transform,pdf_filter_options *options,void *opaque)
{
    pv_text_target *target=opaque;
    pv_text_filter_state *state=fz_malloc_struct(ctx,pv_text_filter_state);
    state->target=target;state->next=target->filters;target->filters=state;
    state->effects=target->form_effects;
    if (target->form_spacing) {
        pdf_obj *saved=target->form_spacing;
        if (!pdf_is_array(ctx,saved) || pdf_array_len(ctx,saved)!=3)
            fz_throw(ctx,FZ_ERROR_ARGUMENT,"Invalid saved text spacing");
        for(int i=0;i<3;++i)
            if(!pdf_is_number(ctx,pdf_array_get(ctx,saved,i))) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Invalid saved text spacing");
        state->saved_font_size=pdf_array_get_real(ctx,saved,0);
        state->saved_character=pdf_array_get_real(ctx,saved,1);state->saved_word=pdf_array_get_real(ctx,saved,2);
        if(!isfinite(state->saved_font_size) || state->saved_font_size<=0 || !isfinite(state->saved_character) || !isfinite(state->saved_word))
            fz_throw(ctx,FZ_ERROR_ARGUMENT,"Invalid saved text spacing");
        state->has_saved_spacing=1;
    }
    state->options.opaque=state;
    state->options.text_filter=pv_text_remove;
    state->options.after_text_object=pv_text_insert;
    state->processor=pdf_new_sanitize_filter(ctx,doc,chain,struct_parents,transform,options,&state->options);
    pdf_processor *p=state->processor;
    state->original_gs_begin=p->op_gs_begin;p->op_gs_begin=pv_text_gs;
    state->original_Tc=p->op_Tc;p->op_Tc=pv_spacing_Tc;
    state->original_Tw=p->op_Tw;p->op_Tw=pv_spacing_Tw;
    state->original_Tf=p->op_Tf;p->op_Tf=pv_spacing_Tf;
    state->original_q=p->op_q;p->op_q=pv_spacing_q;
    state->original_Q=p->op_Q;p->op_Q=pv_spacing_Q;
    state->original_dquote=p->op_dquote;p->op_dquote=pv_spacing_dquote;
    state->original_Do_form=p->op_Do_form;p->op_Do_form=pv_spacing_Do_form;
    return p;
}

static void pv_edit_text(const unsigned char *data, size_t length, int number,
    int start, int count, const int *replacement, int replacement_count,
    float dx, float dy, int duplicate, pv_result *result)
{
    fz_context *ctx = fz_new_context(NULL, NULL, FZ_STORE_DEFAULT);
    pdf_document *doc = NULL;
    fz_stream *input = NULL;
    pdf_page *page = NULL;
    fz_stext_page *layout = NULL;
    fz_device *device = NULL;
    fz_buffer *append = NULL;
    pv_text_target target = {0};
    fz_var(doc); fz_var(input); fz_var(page); fz_var(layout);
    fz_var(device); fz_var(append);
    fz_var(target);
    target.duplicate=duplicate;
    target.replacement_codes=replacement;target.replacement_count=replacement_count;target.dx=dx;target.dy=dy;
    if (!ctx) { snprintf(result->error, sizeof(result->error), "Cannot create probe context"); return; }
    fz_set_user_context(ctx,&target);
    fz_set_error_callback(ctx, pv_text_silent, NULL);
    fz_set_warning_callback(ctx, pv_text_silent, NULL);
    fz_try(ctx) {
        input = fz_open_memory(ctx, data, length);
        doc = pdf_open_document_with_stream(ctx, input);
        if (pdf_needs_password(ctx, doc))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Encrypted PDF not supported by text editing");
        page = pdf_load_page(ctx, doc, number);
        if (count > 0) pv_check_text_page(ctx,doc,page);
        fz_rect box;
        pdf_page_transform(ctx, page, &box, &target.page_ctm);
        target.page_box=box;
        /* Contents only: widget/annotation appearances are not editable source text. */
        fz_stext_options st = {0};
        layout = fz_new_stext_page(ctx, fz_bound_page(ctx, &page->super));
        device = fz_new_stext_device(ctx, layout, &st);
        pdf_run_page_contents(ctx, page, device, fz_identity, NULL);
        fz_close_device(ctx, device);
        fz_drop_device(ctx, device); device = NULL;
        if (count == 0) {
            fz_rect bounds = fz_bound_page(ctx, &page->super);
            result->width = bounds.x1 - bounds.x0; result->height = bounds.y1 - bounds.y0;
            size_t n = 0;
            for (fz_stext_block *b = layout->first_block; b; b = b->next) {
                if (b->type != FZ_STEXT_BLOCK_TEXT) continue;
                for (fz_stext_line *l = b->u.t.first_line; l; l = l->next)
                    for (fz_stext_char *ch = l->first_char; ch; ch = ch->next) ++n;
            }
            result->glyphs = calloc(n ? n : 1, sizeof(pv_glyph));
            if (!result->glyphs) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate text editing layout");
            int line = 0;
            for (fz_stext_block *b = layout->first_block; b; b = b->next) {
                if (b->type != FZ_STEXT_BLOCK_TEXT) continue;
                for (fz_stext_line *l = b->u.t.first_line; l; l = l->next, ++line) {
                    for (fz_stext_char *ch = l->first_char; ch; ch = ch->next) {
                        pv_glyph *g = &result->glyphs[result->glyph_count++];
                        g->codepoint = ch->c; g->line = line;
                        g->origin = (pv_point){(ch->origin.x-bounds.x0)/result->width,(ch->origin.y-bounds.y0)/result->height};
                        fz_point points[] = {ch->quad.ul,ch->quad.ur,ch->quad.ll,ch->quad.lr};
                        pv_point *dest[] = {&g->quad.ul,&g->quad.ur,&g->quad.ll,&g->quad.lr};
                        for (int j=0;j<4;++j) *dest[j]=(pv_point){(points[j].x-bounds.x0)/result->width,(points[j].y-bounds.y0)/result->height};
                    }
                }
            }
        } else {
        target.count = count;
        target.chars = fz_calloc(ctx, count, sizeof(*target.chars));
        target.seen = fz_calloc(ctx, count, sizeof(*target.seen));
        int index = 0, selected = 0;
        fz_stext_line *selected_line = NULL;
        for (fz_stext_block *b = layout->first_block; b; b = b->next) {
            if (b->type != FZ_STEXT_BLOCK_TEXT) continue;
            for (fz_stext_line *l = b->u.t.first_line; l; l = l->next) {
                for (fz_stext_char *ch = l->first_char; ch; ch = ch->next, ++index) {
                    if (index < start || index - start >= count) continue;
                    if (l->wmode || fabsf(l->dir.x - 1) > 0.001f || fabsf(l->dir.y) > 0.001f ||
                        (selected_line && selected_line != l))
                        fz_throw(ctx, FZ_ERROR_ARGUMENT, "Text editing requires one horizontal line");
                    selected_line = l;
                    if (ch->bidi & 1 || (ch->c > 0x024f && ch->c != 0x20ac))
                        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Only simple Latin source text is supported for text editing");
                    if (selected && (ch->font != target.chars[0]->font || ch->size != target.chars[0]->size || ch->color != target.chars[0]->color))
                        fz_throw(ctx, FZ_ERROR_ARGUMENT, "Text editing requires one style");
                    target.chars[selected++] = ch;
                }
            }
        }
        if (selected != count)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Text editing range is out of bounds");
        pv_text_obstacles(ctx,&target,layout,start,count);
        /* The filter rewrites a stream object in place: detach shared Contents. */
        pdf_obj *original_contents = pdf_page_contents(ctx, page);
        if (pdf_is_stream(ctx, original_contents)) {
            append = pdf_load_stream(ctx, original_contents);
            pdf_dict_put_drop(ctx, page->obj, PDF_NAME(Contents), pdf_add_stream(ctx, doc, append, NULL, 0));
        }
        pdf_filter_factory filters[] = {{pv_make_text_filter, &target}, {NULL, NULL}};
        pdf_filter_options filter = {0};
        filter.recurse = 1; filter.instance_forms = 1; filter.filters = filters;
        pdf_filter_page_contents(ctx, doc, page, &filter);
        for (int i = 0; i < count; ++i)
            if (target.seen[i] != 1)
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Extracted character has no unique source glyph");

        if (!target.inserted) fz_throw(ctx, FZ_ERROR_ARGUMENT, "No unique text insertion point");
        pv_text_save(ctx, doc, result);
        }
    }
    fz_always(ctx) {
        while(target.filters) {pv_text_filter_state *next=target.filters->next;fz_free(ctx,target.filters);target.filters=next;}
        fz_free(ctx, target.chars); fz_free(ctx, target.seen);
        fz_free(ctx,target.obstacles);fz_free(ctx,target.obstacle_origins);
        fz_drop_device(ctx, device); fz_drop_stext_page(ctx, layout);
        pdf_drop_obj(ctx, target.replacement);
        fz_drop_buffer(ctx, append);
        pdf_drop_page(ctx, page); pdf_drop_document(ctx, doc); fz_drop_stream(ctx, input);
    }
    fz_catch(ctx) {
        snprintf(result->error, sizeof(result->error), "%s", fz_caught_message(ctx));
        fz_report_error(ctx);
    }
    fz_drop_context(ctx);
}

void pv_correct_text(const unsigned char *data,size_t length,int number,
    int start,int count,const int *replacement,int replacement_count,float dx,float dy,pv_result *result)
{ pv_edit_text(data,length,number,start,count,replacement,replacement_count,dx,dy,0,result); }

void pv_duplicate_text(const unsigned char *data,size_t length,int number,
    int start,int count,const int *replacement,int replacement_count,float dx,float dy,pv_result *result)
{ pv_edit_text(data,length,number,start,count,replacement,replacement_count,dx,dy,1,result); }
