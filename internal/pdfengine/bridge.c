#include "bridge.h"
#include "sanitize.h"
#include "mupdf/fitz.h"
#include "mupdf/pdf.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

/* Every MuPDF exception is caught in C; longjmp must never cross Go frames. */
static void pv_error(pv_result *result, const char *message)
{
    snprintf(result->error, sizeof(result->error), "%s", message);
}

/* Capture errors through the return value, without logging document data. */
static void pv_silent_message(void *opaque, const char *message)
{
    (void)opaque;
    (void)message;
}

static fz_context *pv_context(void)
{
    fz_context *ctx = fz_new_context(NULL, NULL, FZ_STORE_DEFAULT);
    if (ctx) {
        fz_set_error_callback(ctx, pv_silent_message, NULL);
        fz_set_warning_callback(ctx, pv_silent_message, NULL);
    }
    return ctx;
}

static pdf_document *pv_open(fz_context *ctx, const unsigned char *data, size_t length)
{
    pdf_document *doc = NULL;
    fz_stream *stream = NULL;
    fz_var(stream);
    fz_var(doc);
    fz_try(ctx) {
        stream = fz_open_memory(ctx, data, length);
        doc = pdf_open_document_with_stream(ctx, stream);
        if (pdf_needs_password(ctx, doc))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Encrypted PDFs are not supported for redaction");
    }
    fz_always(ctx) {
        fz_drop_stream(ctx, stream);
    }
    fz_catch(ctx) {
        pdf_drop_document(ctx, doc);
        fz_rethrow(ctx);
    }
    return doc;
}

static pv_point pv_normalize(fz_point p, fz_rect bounds)
{
    return (pv_point){(p.x - bounds.x0) / (bounds.x1 - bounds.x0),
        (p.y - bounds.y0) / (bounds.y1 - bounds.y0)};
}

void pv_layout(const unsigned char *data, size_t length, int page_number, pv_result *result)
{
    fz_context *ctx = pv_context();
    pdf_document *doc = NULL;
    pdf_page *page = NULL;
    fz_stext_page *text = NULL;
    fz_var(doc);
    fz_var(page);
    fz_var(text);
    if (!ctx) { pv_error(result, "Cannot create MuPDF context"); return; }
    fz_try(ctx) {
        doc = pv_open(ctx, data, length);
        pv_clean_resource_tags(ctx, doc);
        page = pdf_load_page(ctx, doc, page_number);
        pv_clean_page_tags(ctx, doc, page);
        fz_rect bounds = fz_bound_page(ctx, &page->super);
        if (fz_is_empty_rect(bounds) || fz_is_infinite_rect(bounds))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Invalid page bounds");
        result->width = bounds.x1 - bounds.x0;
        result->height = bounds.y1 - bounds.y0;
        fz_stext_options options = {0};
        text = fz_new_stext_page_from_page(ctx, &page->super, &options);
        size_t count = 0;
        for (fz_stext_block *block = text->first_block; block; block = block->next) {
            if (block->type != FZ_STEXT_BLOCK_TEXT) continue;
            for (fz_stext_line *line = block->u.t.first_line; line; line = line->next)
                for (fz_stext_char *ch = line->first_char; ch; ch = ch->next) ++count;
        }
        if (count) {
            result->glyphs = calloc(count, sizeof(pv_glyph));
            if (!result->glyphs) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate text geometry");
        }
        int line_number = 0;
        for (fz_stext_block *block = text->first_block; block; block = block->next) {
            if (block->type != FZ_STEXT_BLOCK_TEXT) continue;
            for (fz_stext_line *line = block->u.t.first_line; line; line = line->next, ++line_number) {
                for (fz_stext_char *ch = line->first_char; ch; ch = ch->next) {
                    pv_glyph *out = &result->glyphs[result->glyph_count++];
                    out->codepoint = ch->c;
                    out->line = line_number;
                    out->quad = (pv_quad){pv_normalize(ch->quad.ul, bounds),
                        pv_normalize(ch->quad.ur, bounds), pv_normalize(ch->quad.ll, bounds),
                        pv_normalize(ch->quad.lr, bounds)};
                }
            }
        }
    }
    fz_always(ctx) {
        fz_drop_stext_page(ctx, text);
        fz_drop_page(ctx, (fz_page *)page);
        pdf_drop_document(ctx, doc);
    }
    fz_catch(ctx) {
        pv_error(result, fz_caught_message(ctx));
        fz_report_error(ctx);
    }
    fz_drop_context(ctx);
}

static fz_point pv_denormalize(pv_point point, fz_rect bounds)
{
    return (fz_point){bounds.x0 + point.x * (bounds.x1 - bounds.x0),
        bounds.y0 + point.y * (bounds.y1 - bounds.y0)};
}

/* The sanitized entry point is the only application export path. */
static void pv_apply(const unsigned char *data, size_t length,
    const pv_region *regions, size_t count, int sanitize, pv_result *result)
{
    fz_context *ctx = pv_context();
    pdf_document *doc = NULL;
    pdf_document *clean = NULL;
    pdf_graft_map *map = NULL;
    pdf_page *page = NULL;
    fz_buffer *buffer = NULL;
    fz_output *output = NULL;
    fz_var(doc);
    fz_var(clean);
    fz_var(map);
    fz_var(page);
    fz_var(buffer);
    fz_var(output);
    if (!ctx) { pv_error(result, "Cannot create MuPDF context"); return; }
    fz_try(ctx) {
        doc = pv_open(ctx, data, length);
        int page_count = pdf_count_pages(ctx, doc);
        for (size_t i = 0; i < count; ++i)
            if (regions[i].page < 0 || regions[i].page >= page_count)
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction page is out of range");
        /* Preserve visible widget/annotation appearances before applying marks.
         * Their original dictionaries will not be copied into the clean PDF. */
        if (sanitize) {
            pv_clean_document(ctx, doc, sanitize == 2);
            pv_clean_images(ctx, doc, regions, count);
        } else {
            pdf_bake_document(ctx, doc, 1, 1);
        }
        for (int number = 0; number < page_count; ++number) {
            int has_regions = 0;
            for (size_t i = 0; i < count; ++i)
                if (regions[i].page == number) has_regions = 1;
            if (!has_regions) continue;
            page = pdf_load_page(ctx, doc, number);
            fz_rect bounds = fz_bound_page(ctx, &page->super);
            for (size_t i = 0; i < count; ++i) {
                if (regions[i].page != number) continue;
                pv_quad q = regions[i].quad;
                fz_quad quad = {pv_denormalize(q.ul, bounds), pv_denormalize(q.ur, bounds),
                    pv_denormalize(q.ll, bounds), pv_denormalize(q.lr, bounds)};
                pdf_annot *annot = pdf_create_annot_raw(ctx, page, PDF_ANNOT_REDACT);
                /* Annotation APIs already accept Fitz page coordinates. */
                pdf_set_annot_rect(ctx, annot, fz_rect_from_quad(quad));
                pdf_set_annot_quad_points(ctx, annot, 1, &quad);
            }
            pdf_redact_options options = {1, PDF_REDACT_IMAGE_PIXELS,
                PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED, PDF_REDACT_TEXT_REMOVE};
            pdf_redact_page(ctx, doc, page, &options);
            fz_drop_page(ctx, &page->super);
            page = NULL;
        }
        /* Rebuild from page content/resources only. This removes catalog-level
         * metadata, attachments, structure trees, actions and form dictionaries.
         * It does NOT prove sanitation of data inside retained resources. */
        clean = pdf_create_document(ctx);
        map = pdf_new_graft_map(ctx, clean);
        for (int number = 0; number < page_count; ++number) {
            pdf_graft_mapped_page(ctx, map, -1, doc, number);
            pdf_obj *source = pdf_lookup_page_obj(ctx, doc, number);
            pdf_obj *group = pdf_dict_get(ctx, source, PDF_NAME(Group));
            if (group)
                pdf_dict_put_drop(ctx, pdf_lookup_page_obj(ctx, clean, number), PDF_NAME(Group),
                    pdf_graft_mapped_object(ctx, map, group));
        }
        pdf_write_options options = pdf_default_write_options;
        options.do_incremental = 0;
        options.do_garbage = 4;
        options.do_clean = 1;
        options.do_sanitize = 1;
        options.do_compress = 1;
        options.do_preserve_metadata = 0;
        buffer = fz_new_buffer(ctx, 1024);
        output = fz_new_output_with_buffer(ctx, buffer);
        pdf_write_document(ctx, clean, output, &options);
        fz_close_output(ctx, output);
        unsigned char *bytes = NULL;
        result->length = fz_buffer_storage(ctx, buffer, &bytes);
        result->bytes = malloc(result->length);
        if (!result->bytes) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate redaction output");
        memcpy(result->bytes, bytes, result->length);
    }
    fz_always(ctx) {
        fz_drop_page(ctx, (fz_page *)page);
        fz_drop_output(ctx, output);
        fz_drop_buffer(ctx, buffer);
        pdf_drop_graft_map(ctx, map);
        pdf_drop_document(ctx, clean);
        pdf_drop_document(ctx, doc);
    }
    fz_catch(ctx) {
        pv_error(result, fz_caught_message(ctx));
        fz_report_error(ctx);
    }
    fz_drop_context(ctx);
}

void pv_trial_redact(const unsigned char *data, size_t length,
    const pv_region *regions, size_t count, pv_result *result)
{
    pv_apply(data, length, regions, count, 0, result);
}

void pv_redact(const unsigned char *data, size_t length,
    const pv_region *regions, size_t count, pv_result *result)
{
    pv_apply(data, length, regions, count, 1, result);
}

void pv_free_result(pv_result *result)
{
    free(result->bytes);
    free(result->glyphs);
}

/* Composed PDFs may contain only the optional layers introduced by our overlay
 * writer. The Go entry point validates every original source before using this. */
void pv_redact_composed(const unsigned char *data, size_t length,
    const pv_region *regions, size_t count, pv_result *result)
{
    pv_apply(data, length, regions, count, 2, result);
}

void pv_check_source(const unsigned char *data, size_t length, pv_result *result)
{
    fz_context *ctx = pv_context();
    pdf_document *doc = NULL;
    fz_var(doc);
    if (!ctx) { pv_error(result, "Cannot create MuPDF context"); return; }
    fz_try(ctx) {
        doc = pv_open(ctx, data, length);
        pv_clean_document(ctx, doc, 0);
    }
    fz_always(ctx) { pdf_drop_document(ctx, doc); }
    fz_catch(ctx) { pv_error(result, fz_caught_message(ctx)); fz_report_error(ctx); }
    fz_drop_context(ctx);
}

/* Form calls deliberately do not enable JavaScript from the input document. */
void pv_free_forms(pv_form_result *result)
{
    for (size_t i = 0; i < result->count; ++i) {
        pv_field *f = &result->fields[i];
        free(f->name); free(f->value); free(f->on_value); free(f->caption);
        for (int j = 0; j < f->value_count; ++j) free(f->values[j]);
        for (int j = 0; j < f->default_count; ++j) free(f->defaults[j]);
        for (int j = 0; j < f->reset_count; ++j) free(f->reset_fields[j]);
        free(f->values); free(f->defaults); free(f->reset_fields);
        for (int j = 0; j < f->option_count; ++j) {
            if (f->options) free(f->options[j]);
            if (f->export_options) free(f->export_options[j]);
        }
        free(f->options); free(f->export_options);
    }
    free(result->fields);
}

static void pv_read_field_values(fz_context *ctx, pdf_obj *obj, char ***strings, int *count)
{
    if (!obj) return;
    int n = pdf_is_array(ctx, obj) ? pdf_array_len(ctx, obj) : 1;
    *strings = calloc(n, sizeof(char *));
    if (n && !*strings) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate PDF values");
    for (int i = 0; i < n; ++i) {
        pdf_obj *value = pdf_is_array(ctx, obj) ? pdf_array_get(ctx, obj, i) : obj;
        (*strings)[i] = strdup(pdf_is_name(ctx, value) ? pdf_to_name(ctx, value) : pdf_to_text_string(ctx, value));
        if (!(*strings)[i]) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate PDF value");
        ++*count;
    }
}

/* Only standalone standard ResetForm actions are exposed as buttons. Other
 * actions and action chains remain inactive; this does not execute scripts. */
static pdf_obj *pv_reset_action(fz_context *ctx, pdf_obj *obj)
{
    pdf_obj *action = pdf_dict_get_inheritable(ctx, obj, PDF_NAME(A));
    if (!pdf_name_eq(ctx, pdf_dict_get(ctx, action, PDF_NAME(S)), PDF_NAME(ResetForm)) ||
        pdf_dict_get(ctx, action, PDF_NAME(Next))) return NULL;
    return action;
}

void pv_read_forms(const unsigned char *data, size_t length, pv_form_result *result)
{
    fz_context *ctx = pv_context();
    pdf_document *doc = NULL;
    pdf_page *page = NULL;
    fz_var(doc); fz_var(page);
    if (!ctx) { snprintf(result->error, sizeof(result->error), "Cannot create MuPDF context"); return; }
    fz_try(ctx) {
        doc = pv_open(ctx, data, length);
        pdf_obj *form = pdf_dict_get(ctx, pdf_dict_get(ctx, pdf_trailer(ctx, doc), PDF_NAME(Root)), PDF_NAME(AcroForm));
        if (pdf_dict_get(ctx, form, PDF_NAME(XFA)))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "XFA forms are not supported");
        int pages = pdf_array_len(ctx, pdf_dict_get(ctx, form, PDF_NAME(Fields))) ? pdf_count_pages(ctx, doc) : 0;
        for (int n = 0; n < pages; ++n) {
            page = pdf_load_page(ctx, doc, n);
            fz_rect bounds = fz_bound_page(ctx, &page->super);
            for (pdf_annot *w = pdf_first_widget(ctx, page); w; w = pdf_next_widget(ctx, w)) {
                int kind = pdf_widget_type(ctx, w);
                int flags = pdf_annot_field_flags(ctx, w);
                if (pdf_widget_is_readonly(ctx, w) ||
                    (pdf_annot_flags(ctx, w) & (PDF_ANNOT_IS_HIDDEN | PDF_ANNOT_IS_INVISIBLE | PDF_ANNOT_IS_NO_VIEW))) continue;
                if (kind != PDF_WIDGET_TYPE_TEXT && kind != PDF_WIDGET_TYPE_CHECKBOX &&
                    kind != PDF_WIDGET_TYPE_RADIOBUTTON && kind != PDF_WIDGET_TYPE_COMBOBOX && kind != PDF_WIDGET_TYPE_LISTBOX &&
                    !(kind == PDF_WIDGET_TYPE_BUTTON && pv_reset_action(ctx, pdf_annot_obj(ctx, w)))) continue;
                if (kind == PDF_WIDGET_TYPE_TEXT && (flags & PDF_TX_FIELD_IS_FILE_SELECT)) continue;
                fz_rect rect = fz_intersect_rect(pdf_bound_widget(ctx, w), bounds);
                if (fz_is_empty_rect(rect)) continue;
                pv_field *fields = realloc(result->fields, (result->count + 1) * sizeof(pv_field));
                if (!fields) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate PDF fields");
                result->fields = fields;
                pv_field *f = &fields[result->count++];
                memset(f, 0, sizeof(*f));
                pdf_obj *obj = pdf_annot_obj(ctx, w);
                f->page = n; f->id = pdf_to_num(ctx, obj); f->kind = kind; f->flags = flags;
                f->x = (rect.x0-bounds.x0)/(bounds.x1-bounds.x0);
                f->y = (rect.y0-bounds.y0)/(bounds.y1-bounds.y0);
                f->width = (rect.x1-rect.x0)/(bounds.x1-bounds.x0);
                f->height = (rect.y1-rect.y0)/(bounds.y1-bounds.y0);
                char *name = pdf_load_field_name(ctx, obj);
                f->name = strdup(name ? name : "");
                fz_free(ctx, name);
                const char *value = pdf_field_value(ctx, obj);
                f->value = strdup(value ? value : "");
                pv_read_field_values(ctx, pdf_dict_get_inheritable(ctx, obj, PDF_NAME(V)), &f->values, &f->value_count);
                pv_read_field_values(ctx, pdf_dict_get_inheritable(ctx, obj, PDF_NAME(DV)), &f->defaults, &f->default_count);
                if (kind == PDF_WIDGET_TYPE_BUTTON) {
                    pdf_obj *action = pv_reset_action(ctx, obj);
                    pdf_obj *targets = pdf_dict_get(ctx, action, PDF_NAME(Fields));
                    f->reset_exclude = pdf_dict_get_int(ctx, action, PDF_NAME(Flags)) & 1;
                    int n = pdf_array_len(ctx, targets);
                    f->reset_fields = calloc(n, sizeof(char *));
                    if (n && !f->reset_fields) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate reset targets");
                    for (int j = 0; j < n; ++j) {
                        pdf_obj *target = pdf_array_get(ctx, targets, j);
                        if (pdf_is_string(ctx, target)) f->reset_fields[j] = strdup(pdf_to_text_string(ctx, target));
                        else {
                            char *name = pdf_load_field_name(ctx, target);
                            f->reset_fields[j] = strdup(name ? name : "");
                            fz_free(ctx, name);
                        }
                        if (!f->reset_fields[j]) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate reset target");
                        ++f->reset_count;
                    }
                    f->caption = strdup(pdf_dict_get_text_string(ctx, pdf_dict_get(ctx, obj, PDF_NAME(MK)), PDF_NAME(CA)));
                }
                f->on_value = strdup(pdf_to_name(ctx, pdf_button_field_on_state(ctx, obj)));
                if (kind == PDF_WIDGET_TYPE_TEXT) f->max_len = pdf_text_widget_max_len(ctx, w);
                if (kind == PDF_WIDGET_TYPE_COMBOBOX || kind == PDF_WIDGET_TYPE_LISTBOX) {
                    f->option_count = pdf_choice_field_option_count(ctx, obj);
                    f->options = calloc(f->option_count, sizeof(char *));
                    f->export_options = calloc(f->option_count, sizeof(char *));
                    if (f->option_count && (!f->options || !f->export_options)) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate PDF choices");
                    for (int j = 0; j < f->option_count; ++j) {
                        f->options[j] = strdup(pdf_choice_field_option(ctx, obj, 0, j));
                        f->export_options[j] = strdup(pdf_choice_field_option(ctx, obj, 1, j));
                    }
                }
            }
            fz_drop_page(ctx, &page->super); page = NULL;
        }
    }
    fz_always(ctx) { fz_drop_page(ctx, (fz_page *)page); pdf_drop_document(ctx, doc); }
    fz_catch(ctx) { snprintf(result->error, sizeof(result->error), "%s", fz_caught_message(ctx)); }
    fz_drop_context(ctx);
}

static pdf_obj *pv_field_head(fz_context *ctx, pdf_obj *obj)
{
    int depth = 0;
    while (!pdf_dict_get(ctx, obj, PDF_NAME(T)) && pdf_dict_get(ctx, obj, PDF_NAME(Parent))) {
        if (++depth > 100) fz_throw(ctx, FZ_ERROR_ARGUMENT, "PDF field hierarchy is too deep");
        obj = pdf_dict_get(ctx, obj, PDF_NAME(Parent));
    }
    return obj;
}

static int pv_changed_list(fz_context *ctx, pdf_annot *widget, pdf_obj *changed)
{
    if (pdf_widget_type(ctx, widget) != PDF_WIDGET_TYPE_LISTBOX) return 0;
    pdf_obj *head = pv_field_head(ctx, pdf_annot_obj(ctx, widget));
    for (int i = 0; i < pdf_array_len(ctx, changed); ++i)
        if (!pdf_objcmp_resolve(ctx, head, pdf_array_get(ctx, changed, i))) return 1;
    return 0;
}

/* MuPDF 1.24 renders list labels but explicitly leaves selection highlighting
 * unimplemented. Keep the selected item at the top of the visible list and
 * tint that row in the generated appearance, preserving its fonts and text.
 * Multiply keeps dark glyphs readable instead of painting over them. */
static void pv_highlight_list(fz_context *ctx, pdf_document *doc, pdf_annot *widget)
{
    pdf_obj *ap = pdf_annot_ap(ctx, widget);
    if (!ap) fz_throw(ctx, FZ_ERROR_FORMAT, "Missing PDF list appearance");
    fz_rect box = pdf_dict_get_rect(ctx, ap, PDF_NAME(BBox));
    const char *font;
    float size, color[4];
    int n;
    pdf_annot_default_appearance(ctx, widget, &font, &size, &n, color);
    if (size <= 0) size = 12; /* MuPDF's multiline auto-size default. */
    float border = pdf_annot_border_width(ctx, widget);
    float x0 = box.x0 + 2 * border, x1 = box.x1 - 2 * border;
    float y1 = box.y1 - 2 * border;
    if (x1 <= x0) return;
    pdf_obj *res = pdf_dict_get(ctx, ap, PDF_NAME(Resources));
    if (!res) res = pdf_dict_put_dict(ctx, ap, PDF_NAME(Resources), 1);
    pdf_obj *states = pdf_dict_get(ctx, res, PDF_NAME(ExtGState));
    if (!states) states = pdf_dict_put_dict(ctx, res, PDF_NAME(ExtGState), 1);
    pdf_obj *state = pdf_dict_puts_dict(ctx, states, "ProviewListSelection", 1);
    pdf_dict_put_name(ctx, state, PDF_NAME(BM), "Multiply");
    fz_buffer *buffer = pdf_load_stream(ctx, ap);
    fz_try(ctx) {
        pdf_obj *obj = pdf_annot_obj(ctx, widget);
        pdf_obj *head = pv_field_head(ctx, obj);
        pdf_obj *indices = pdf_dict_get(ctx, head, PDF_NAME(I));
        int top = pdf_dict_get_int(ctx, obj, PDF_NAME(TI));
        for (int i = 0; i < pdf_array_len(ctx, indices); ++i) {
            int row = pdf_array_get_int(ctx, indices, i) - top;
            if (row < 0) continue;
            float row_top = y1 - row * size * 1.116f;
            float row_bottom = fz_max(box.y0 + border, row_top - size * 1.116f);
            if (row_top <= row_bottom) continue;
            fz_append_printf(ctx, buffer,
                "\nq /ProviewListSelection gs 0.72 0.85 0.98 rg %g %g %g %g re f Q\n",
                x0, row_bottom, x1-x0, row_top-row_bottom);
        }
        pdf_update_stream(ctx, doc, ap, buffer, 0);
    }
    fz_always(ctx) fz_drop_buffer(ctx, buffer);
    fz_catch(ctx) fz_rethrow(ctx);
}

/* MuPDF 1.24 does not mask password text in synthesized appearances. Use a
 * temporary masked value only while generating graphics, then restore /V.
 * The saved logical value remains editable; it is not encrypted by this flag. */
static void pv_update_form_page(fz_context *ctx, pdf_document *doc, pdf_page *page)
{
    pdf_obj *saved = NULL;
    pdf_obj *lists = NULL;
    fz_buffer *mask = NULL;
    fz_var(saved); fz_var(lists); fz_var(mask);
    fz_try(ctx) {
        saved = pdf_new_array(ctx, doc, 4);
        lists = pdf_new_array(ctx, doc, 4);
        for (pdf_annot *w = pdf_first_widget(ctx, page); w; w = pdf_next_widget(ctx, w)) {
            if (pdf_widget_type(ctx, w) == PDF_WIDGET_TYPE_LISTBOX) {
                pdf_obj *obj = pdf_annot_obj(ctx, w);
                if (!pdf_dict_get(ctx, obj, PDF_NAME(Opt)))
                    pdf_dict_put(ctx, obj, PDF_NAME(Opt), pdf_dict_get_inheritable(ctx, obj, PDF_NAME(Opt)));
                if (!pdf_dict_getp(ctx, obj, "AP/N") || pdf_annot_needs_resynthesis(ctx, w)) {
                    pdf_obj *head = pv_field_head(ctx, obj);
                    if (!pdf_dict_get(ctx, head, PDF_NAME(I))) {
                        pdf_obj *indices = pdf_dict_put_array(ctx, head, PDF_NAME(I), 1);
                        pdf_obj *values = pdf_dict_get_inheritable(ctx, head, PDF_NAME(V));
                        int count = pdf_is_array(ctx, values) ? pdf_array_len(ctx, values) : 1;
                        for (int j = 0; j < pdf_choice_field_option_count(ctx, head); ++j) {
                            const char *option = pdf_choice_field_option(ctx, head, 1, j);
                            for (int k = 0; k < count; ++k) {
                                pdf_obj *value = pdf_is_array(ctx, values) ? pdf_array_get(ctx, values, k) : values;
                                if (!strcmp(option, pdf_to_text_string(ctx, value))) {
                                    pdf_array_push_int(ctx, indices, j);
                                    break;
                                }
                            }
                        }
                    }
                    pdf_array_push(ctx, lists, obj);
                    pdf_annot_request_resynthesis(ctx, w);
                }
            }
            if (pdf_widget_type(ctx, w) != PDF_WIDGET_TYPE_TEXT ||
                !(pdf_annot_field_flags(ctx, w) & PDF_TX_FIELD_IS_PASSWORD)) continue;
            pdf_obj *head = pv_field_head(ctx, pdf_annot_obj(ctx, w));
            int seen = 0;
            for (int j = 0; j < pdf_array_len(ctx, saved); j += 2)
                if (!pdf_objcmp_resolve(ctx, head, pdf_array_get(ctx, saved, j))) seen = 1;
            if (!seen) {
                const char *value = pdf_field_value(ctx, head);
                mask = fz_new_buffer(ctx, 32);
                if (value) for (const char *p = value; *p;) {
                    int rune;
                    p += fz_chartorune(&rune, p);
                    fz_append_byte(ctx, mask, '*');
                }
                pdf_array_push(ctx, saved, head);
                pdf_array_push(ctx, saved, pdf_dict_get(ctx, head, PDF_NAME(V)));
                pdf_dict_put_text_string(ctx, head, PDF_NAME(V), fz_string_from_buffer(ctx, mask));
                fz_drop_buffer(ctx, mask); mask = NULL;
            }
            pdf_annot_request_resynthesis(ctx, w);
        }
        pdf_update_page(ctx, page);
        for (pdf_annot *w = pdf_first_widget(ctx, page); w; w = pdf_next_widget(ctx, w)) {
            for (int j = 0; j < pdf_array_len(ctx, lists); ++j) {
                if (!pdf_objcmp_resolve(ctx, pdf_annot_obj(ctx, w), pdf_array_get(ctx, lists, j))) {
                    pv_highlight_list(ctx, doc, w);
                    break;
                }
            }
        }
    }
    fz_always(ctx) {
        for (int j = 0; j < pdf_array_len(ctx, saved); j += 2) {
            pdf_obj *head = pdf_array_get(ctx, saved, j);
            pdf_obj *value = pdf_array_get(ctx, saved, j+1);
            if (pdf_is_null(ctx, value)) pdf_dict_del(ctx, head, PDF_NAME(V));
            else pdf_dict_put(ctx, head, PDF_NAME(V), value);
        }
        fz_drop_buffer(ctx, mask);
        pdf_drop_obj(ctx, saved); pdf_drop_obj(ctx, lists);
    }
    fz_catch(ctx) fz_rethrow(ctx);
}

void pv_fill_form(const unsigned char *data, size_t length,
    const pv_form_edit *edits, size_t count, pv_result *result)
{
    fz_context *ctx = pv_context();
    pdf_document *doc = NULL;
    pdf_page *page = NULL;
    pdf_obj *changed_lists = NULL;
    pdf_obj *edit_obj = NULL;
    fz_buffer *buffer = NULL;
    fz_output *output = NULL;
    fz_var(doc); fz_var(page); fz_var(changed_lists); fz_var(edit_obj); fz_var(buffer); fz_var(output);
    if (!ctx) { pv_error(result, "Cannot create MuPDF context"); return; }
    fz_try(ctx) {
        doc = pv_open(ctx, data, length);
        changed_lists = pdf_new_array(ctx, doc, 1);
        for (size_t i = 0; i < count; ++i) {
            edit_obj = pdf_new_indirect(ctx, doc, edits[i].id, 0);
            pdf_obj *head = pv_field_head(ctx, edit_obj);
            int kind = pdf_field_type(ctx, head);
            int multi = kind == PDF_WIDGET_TYPE_LISTBOX && (pdf_field_flags(ctx, head) & PDF_CH_FIELD_IS_MULTI_SELECT);
            const char *value = edits[i].count ? edits[i].values[0] : "";
            int accepted = 1;
            if (multi) {
                pdf_obj *array = pdf_dict_put_array(ctx, head, PDF_NAME(V), edits[i].count);
                for (int j = 0; j < edits[i].count; ++j) pdf_array_push_text_string(ctx, array, edits[i].values[j]);
                pdf_dirty_obj(ctx, head);
            } else {
                if (kind == PDF_WIDGET_TYPE_COMBOBOX || kind == PDF_WIDGET_TYPE_LISTBOX)
                    pdf_dict_del(ctx, head, PDF_NAME(I));
                accepted = pdf_set_field_value(ctx, doc, head, value, 1);
            }
            if (kind == PDF_WIDGET_TYPE_LISTBOX) {
                pdf_obj *indices = pdf_dict_put_array(ctx, head, PDF_NAME(I), edits[i].count);
                int first = -1;
                for (int j = 0; j < pdf_choice_field_option_count(ctx, head); ++j) {
                    for (int k = 0; k < edits[i].count; ++k) {
                        if (!strcmp(edits[i].values[k], pdf_choice_field_option(ctx, head, 1, j))) {
                            pdf_array_push_int(ctx, indices, j);
                            if (first < 0) first = j;
                            break;
                        }
                    }
                }
                pdf_dict_put_int(ctx, head, PDF_NAME(TI), first < 0 ? 0 : first);
                pdf_array_push(ctx, changed_lists, head);
            }
            pdf_drop_obj(ctx, edit_obj); edit_obj = NULL;
            if (!accepted) fz_throw(ctx, FZ_ERROR_ARGUMENT, "PDF field value was rejected");
        }
        for (int n = 0; n < pdf_count_pages(ctx, doc); ++n) {
            page = pdf_load_page(ctx, doc, n);
            for (pdf_annot *w = pdf_first_widget(ctx, page); w; w = pdf_next_widget(ctx, w)) {
                if (!pv_changed_list(ctx, w, changed_lists)) continue;
                pdf_obj *obj = pdf_annot_obj(ctx, w);
                pdf_obj *head = pv_field_head(ctx, obj);
                pdf_dict_put(ctx, obj, PDF_NAME(Opt), pdf_dict_get_inheritable(ctx, obj, PDF_NAME(Opt)));
                pdf_dict_put_int(ctx, obj, PDF_NAME(TI), pdf_dict_get_int(ctx, head, PDF_NAME(TI)));
                pdf_annot_request_resynthesis(ctx, w);
            }
            pv_update_form_page(ctx, doc, page);
            fz_drop_page(ctx, &page->super); page = NULL;
        }
        pdf_write_options options = pdf_default_write_options;
        options.do_compress = 1;
        buffer = fz_new_buffer(ctx, 1024);
        output = fz_new_output_with_buffer(ctx, buffer);
        pdf_write_document(ctx, doc, output, &options);
        fz_close_output(ctx, output);
        unsigned char *bytes = NULL;
        result->length = fz_buffer_storage(ctx, buffer, &bytes);
        result->bytes = malloc(result->length);
        if (!result->bytes) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate form output");
        memcpy(result->bytes, bytes, result->length);
    }
    fz_always(ctx) {
        fz_drop_page(ctx, (fz_page *)page); fz_drop_output(ctx, output);
        fz_drop_buffer(ctx, buffer); pdf_drop_obj(ctx, changed_lists); pdf_drop_obj(ctx, edit_obj); pdf_drop_document(ctx, doc);
    }
    fz_catch(ctx) { pv_error(result, fz_caught_message(ctx)); }
    fz_drop_context(ctx);
}

void pv_render_forms(const unsigned char *data, size_t length, int number, float dpi, pv_result *result)
{
    fz_context *ctx = pv_context();
    pdf_document *doc = NULL;
    pdf_page *page = NULL;
    fz_pixmap *pixmap = NULL;
    fz_buffer *buffer = NULL;
    fz_var(doc); fz_var(page); fz_var(pixmap); fz_var(buffer);
    if (!ctx) { pv_error(result, "Cannot create MuPDF context"); return; }
    fz_try(ctx) {
        doc = pv_open(ctx, data, length);
        page = pdf_load_page(ctx, doc, number);
        pv_update_form_page(ctx, doc, page);
        pixmap = fz_new_pixmap_from_page(ctx, &page->super, fz_scale(dpi/72, dpi/72), fz_device_rgb(ctx), 1);
        buffer = fz_new_buffer_from_pixmap_as_png(ctx, pixmap, fz_default_color_params);
        unsigned char *bytes = NULL;
        result->length = fz_buffer_storage(ctx, buffer, &bytes);
        result->bytes = malloc(result->length);
        if (!result->bytes) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate form preview");
        memcpy(result->bytes, bytes, result->length);
    }
    fz_always(ctx) {
        fz_drop_buffer(ctx, buffer); fz_drop_pixmap(ctx, pixmap);
        fz_drop_page(ctx, (fz_page *)page); pdf_drop_document(ctx, doc);
    }
    fz_catch(ctx) { pv_error(result, fz_caught_message(ctx)); }
    fz_drop_context(ctx);
}
