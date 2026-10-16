//go:build cgo && !nocgo && !extlib

/* Native correction prototype. MuPDF exceptions never cross Go frames. */
#include "textedit.h"
#include "mupdf/fitz.h"
#include "mupdf/pdf.h"
#include <math.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

typedef struct pv_text_filter_state pv_text_filter_state;
typedef struct {
    fz_stext_char **chars;
    int *seen;
    int count, pending, inserted;
    pv_text_filter_state *filters;
    pdf_obj *replacement;
    fz_matrix page_ctm;
} pv_text_target;

struct pv_text_filter_state {
    pv_text_filter_state *next;
    pv_text_target *target;
    pdf_sanitize_filter_options options;
    pdf_processor *processor;
    int kept;
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
        if (++target->seen[i] != 1)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Ambiguous duplicate source glyphs in text editing");
        fz_matrix matrix = fz_concat(fz_concat(trm, ctm), target->page_ctm);
        if (fabsf(matrix.a - ch->size) > 0.02f || fabsf(matrix.d + ch->size) > 0.02f ||
            fabsf(matrix.b) > 0.02f || fabsf(matrix.c) > 0.02f)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Scaled or skewed text is not supported");
        /* Preflight guarantees no live path at BT. If no previous glyph was
         * retained, flush the pending CTM before MuPDF can elide this whole BT.
         * n is emitted before the still-pending BT, with an empty path. */
        if (!state->kept) { state->processor->op_n(ctx,state->processor); state->kept = 1; }
        target->pending = 1;
        return 1;
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
{ (void)proc; if(mode!=0) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Only filled visible text is supported for correction"); }
static void pv_guard_scale(fz_context *ctx,pdf_processor *proc,float value)
{ (void)proc; if(fabsf(value-100)>.001f) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Horizontal text scaling is not supported for correction"); }
static void pv_guard_spacing(fz_context *ctx,pdf_processor *proc,float value)
{ (void)proc; if(fabsf(value)>.001f) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Custom character or word spacing is not supported for correction"); }
static void pv_guard_dquote(fz_context *ctx,pdf_processor *proc,float word,float character,char *str,size_t length)
{ (void)str;(void)length;pv_guard_spacing(ctx,proc,word);pv_guard_spacing(ctx,proc,character); }
static void pv_guard_font(fz_context *ctx,pdf_processor *proc,const char *name,pdf_font_desc *font,float size)
{
    (void)proc;(void)name;(void)size;
    if (!font || !font->font) fz_throw(ctx,FZ_ERROR_ARGUMENT,"Missing text font");
    fz_font_flags_t *flags=fz_font_flags(font->font);
    if (fz_font_t3_procs(ctx,font->font) || flags->fake_bold || flags->fake_italic || flags->ft_substitute)
        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Type 3, substituted or synthetic fonts are not supported for correction");
}
static void pv_guard_gs(fz_context *ctx,pdf_processor *proc,const char *name,pdf_obj *gs)
{
    (void)proc;(void)name;
    pdf_obj *bm=pdf_dict_get(ctx,gs,PDF_NAME(BM));
    pdf_obj *mask=pdf_dict_get(ctx,gs,PDF_NAME(SMask));
    if ((bm && !pdf_name_eq(ctx,bm,PDF_NAME(Normal))) ||
        (mask && !pdf_name_eq(ctx,mask,PDF_NAME(None))) ||
        (pdf_dict_get(ctx,gs,PDF_NAME(ca)) && pdf_dict_get_real(ctx,gs,PDF_NAME(ca))!=1) ||
        (pdf_dict_get(ctx,gs,PDF_NAME(CA)) && pdf_dict_get_real(ctx,gs,PDF_NAME(CA))!=1))
        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Transparency effects are not supported for text editing");
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
        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Alternative text is not supported for correction");
}
static void pv_check_text_page(fz_context *ctx,pdf_document *doc,pdf_page *page)
{
    pdf_processor *proc=pdf_new_processor(ctx,sizeof(pv_text_guard));
    pv_text_guard *g=(pv_text_guard *)proc;
    g->doc=doc;
    proc->drop_processor=pv_guard_drop;
    proc->push_resources=pv_guard_push;proc->pop_resources=pv_guard_pop;
    proc->op_Tr=pv_guard_render;proc->op_Tz=pv_guard_scale;
    proc->op_Tc=pv_guard_spacing;proc->op_Tw=pv_guard_spacing;
    proc->op_dquote=pv_guard_dquote;
    proc->op_Tf=pv_guard_font;proc->op_gs_begin=pv_guard_gs;
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

/* The sanitize filter wraps this callback in q/Q after the source ET.
 * Keep the current clip and paint order, while undoing the local CTM because
 * the replacement Form is expressed in page PDF coordinates. */
static void pv_text_insert(fz_context *ctx, void *opaque, pdf_document *doc,
    pdf_processor *chain, fz_matrix ctm)
{
    pv_text_filter_state *state = opaque;
    pv_text_target *target = state->target;
    if (!target->pending) { state->kept = 0; return; }
    if (target->inserted) fz_throw(ctx, FZ_ERROR_ARGUMENT, "Text spans multiple text objects");
    for (int i = 0; i < target->count; ++i)
        if (target->seen[i] != 1) fz_throw(ctx, FZ_ERROR_ARGUMENT, "Selection spans multiple text objects");
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
    state->options.opaque=state;
    state->options.text_filter=pv_text_remove;
    state->options.after_text_object=pv_text_insert;
    state->processor=pdf_new_sanitize_filter(ctx,doc,chain,struct_parents,transform,options,&state->options);
    return state->processor;
}

void pv_correct_text(const unsigned char *data, size_t length, int number,
    int start, int count, const int *replacement, int replacement_count,
    float dx, float dy, pv_result *result)
{
    fz_context *ctx = fz_new_context(NULL, NULL, FZ_STORE_DEFAULT);
    pdf_document *doc = NULL;
    fz_stream *input = NULL;
    pdf_page *page = NULL;
    fz_stext_page *layout = NULL;
    fz_text *text = NULL;
    fz_device *device = NULL;
    fz_buffer *content = NULL, *append = NULL;
    pdf_obj *resources = NULL, *form = NULL, *form_ref = NULL;
    pv_text_target target = {0};
    fz_var(doc); fz_var(input); fz_var(page); fz_var(layout); fz_var(text);
    fz_var(device); fz_var(content); fz_var(append);
    fz_var(resources); fz_var(form); fz_var(form_ref);
    fz_var(target);
    if (!ctx) { snprintf(result->error, sizeof(result->error), "Cannot create probe context"); return; }
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
            if (!result->glyphs) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate correction layout");
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
                        fz_throw(ctx,FZ_ERROR_ARGUMENT,"Only simple Latin source text is supported for correction");
                    if (selected && (ch->font != target.chars[0]->font || ch->size != target.chars[0]->size || ch->color != target.chars[0]->color))
                        fz_throw(ctx, FZ_ERROR_ARGUMENT, "Text editing requires one style");
                    target.chars[selected++] = ch;
                }
            }
        }
        if (selected != count)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Text editing range is out of bounds");
        fz_stext_char *first = target.chars[0];
        if (fz_font_t3_procs(ctx, first->font))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Type 3 font not supported by text editing");
        if (!pdf_font_writing_supported(ctx, first->font))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Original font cannot be written by text editing");
        text = fz_new_text(ctx);
        fz_matrix pen = fz_scale(first->size, -first->size);
        pen.e = first->origin.x + dx; pen.f = first->origin.y + dy;
        for (int i = 0; i < replacement_count; ++i) {
            if ((replacement[i] > 0x024f && replacement[i] != 0x20ac) || replacement[i] < 32)
                fz_throw(ctx,FZ_ERROR_ARGUMENT,"Only simple Latin replacement text is supported");
            int gid = fz_encode_character(ctx, first->font, replacement[i]);
            if (!gid)
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Original font lacks replacement glyph U+%04X", replacement[i]);
            /* Subsets can retain a Unicode cmap entry for a removed glyph.
             * A nonzero GID alone is therefore not evidence of usable artwork. */
            fz_rect glyph_box = fz_bound_glyph(ctx, first->font, gid, fz_identity);
            /* MuPDF caches empty glyph bounds as a 1e-7 by 1e-7 rectangle. */
            if (replacement[i] != 32 && (fz_is_empty_rect(glyph_box) ||
                (glyph_box.x1 - glyph_box.x0 < 0.000001f && glyph_box.y1 - glyph_box.y0 < 0.000001f)))
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Original font lacks replacement outline U+%04X", replacement[i]);
            fz_show_glyph(ctx, text, first->font, pen, gid, replacement[i], 0, 0, FZ_BIDI_NEUTRAL, FZ_LANG_UNSET);
            pen.e += first->size * fz_advance_glyph(ctx, first->font, gid, 0);
        }
        /* Isolate emitted font resource names from the original page resources. */
        resources = pdf_new_dict(ctx, doc, 2);
        content = fz_new_buffer(ctx, 256);
        device = pdf_new_pdf_device(ctx, doc, fz_invert_matrix(target.page_ctm), resources, content);
        float rgb[] = {((first->color >> 16) & 255) / 255.f, ((first->color >> 8) & 255) / 255.f, (first->color & 255) / 255.f};
        fz_fill_text(ctx, device, text, fz_identity, fz_device_rgb(ctx), rgb, 1, fz_default_color_params);
        fz_close_device(ctx, device);
        fz_drop_device(ctx, device); device = NULL;
        form = pdf_new_dict(ctx, doc, 5);
        pdf_dict_put(ctx, form, PDF_NAME(Type), PDF_NAME(XObject));
        pdf_dict_put(ctx, form, PDF_NAME(Subtype), PDF_NAME(Form));
        pdf_dict_put_rect(ctx, form, PDF_NAME(BBox), box);
        pdf_dict_put(ctx, form, PDF_NAME(Resources), resources);
        form_ref = pdf_add_stream(ctx, doc, content, form, 0);
        target.replacement = form_ref;
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
        fz_drop_device(ctx, device); fz_drop_text(ctx, text); fz_drop_stext_page(ctx, layout);
        pdf_drop_obj(ctx, form_ref); pdf_drop_obj(ctx, form); pdf_drop_obj(ctx, resources);
        fz_drop_buffer(ctx, append); fz_drop_buffer(ctx, content);
        pdf_drop_page(ctx, page); pdf_drop_document(ctx, doc); fz_drop_stream(ctx, input);
    }
    fz_catch(ctx) {
        snprintf(result->error, sizeof(result->error), "%s", fz_caught_message(ctx));
        fz_report_error(ctx);
    }
    fz_drop_context(ctx);
}
