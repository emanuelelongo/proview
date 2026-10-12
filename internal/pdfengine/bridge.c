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
