#include "sanitize.h"
#include <string.h>

static void pv_reject_inline(fz_context *ctx, pdf_processor *processor, fz_image *image, const char *colorspace)
{
    (void)processor; (void)image; (void)colorspace;
    fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of inline images is not supported");
}

/* Re-emit parsed drawing operators without marked-content properties. This
 * removes inline ActualText/Alt as well as references to property dictionaries,
 * without searching arbitrary binary streams or altering visible text. */
static fz_buffer *pv_without_tags(fz_context *ctx, pdf_document *doc,
    pdf_obj *contents, pdf_obj *resources)
{
    fz_buffer *buffer = NULL;
    pdf_processor *processor = NULL;
    pdf_obj *out_resources = NULL;
    fz_var(buffer);
    fz_var(processor);
    fz_var(out_resources);
    fz_try(ctx) {
        buffer = fz_new_buffer(ctx, 1024);
        processor = pdf_new_buffer_processor(ctx, buffer, 0, 1);
        processor->op_BI = pv_reject_inline;
        processor->op_BDC = NULL;
        processor->op_BMC = NULL;
        processor->op_EMC = NULL;
        processor->op_DP = NULL;
        processor->op_MP = NULL;
        pdf_process_contents(ctx, processor, doc, resources, contents, NULL, &out_resources);
        pdf_close_processor(ctx, processor);
    }
    fz_always(ctx) {
        pdf_drop_obj(ctx, out_resources);
        pdf_drop_processor(ctx, processor);
    }
    fz_catch(ctx) {
        fz_drop_buffer(ctx, buffer);
        fz_rethrow(ctx);
    }
    return buffer;
}

void pv_clean_page_tags(fz_context *ctx, pdf_document *doc, pdf_page *page)
{
    fz_buffer *buffer = NULL;
    fz_var(buffer);
    fz_try(ctx) {
        buffer = pv_without_tags(ctx, doc, pdf_page_contents(ctx, page),
            pdf_page_resources(ctx, page));
        pdf_dict_put_drop(ctx, page->obj, PDF_NAME(Contents),
            pdf_add_stream(ctx, doc, buffer, NULL, 0));
    }
    fz_always(ctx) { fz_drop_buffer(ctx, buffer); }
    fz_catch(ctx) { fz_rethrow(ctx); }
}

/* Indirect objects are handled by the outer xref loop. Avoid following cycles
 * and remove hidden-data keys even in directly nested resource dictionaries. */
static void pv_clean_dict(fz_context *ctx, pdf_obj *object, int depth)
{
    if (depth > 64)
        fz_throw(ctx, FZ_ERROR_ARGUMENT, "PDF resource nesting is not supported for redaction");
    if (pdf_is_array(ctx, object)) {
        for (int i = 0; i < pdf_array_len(ctx, object); ++i) {
            pdf_obj *child = pdf_array_get(ctx, object, i);
            if (!pdf_is_indirect(ctx, child)) pv_clean_dict(ctx, child, depth + 1);
        }
    } else if (pdf_is_dict(ctx, object)) {
        static const char *keys[] = {"Metadata", "ActualText", "Alt", "PieceInfo",
            "AF", "AssociatedFiles", "Thumb", "AA", "A", "EF", "Properties", "OC"};
        for (size_t i = 0; i < sizeof(keys) / sizeof(keys[0]); ++i)
            pdf_dict_dels(ctx, object, keys[i]);
        for (int i = 0; i < pdf_dict_len(ctx, object); ++i) {
            pdf_obj *child = pdf_dict_get_val(ctx, object, i);
            if (!pdf_is_indirect(ctx, child)) pv_clean_dict(ctx, child, depth + 1);
        }
    }
}

void pv_clean_resource_tags(fz_context *ctx, pdf_document *doc)
{
    pdf_obj *object = NULL;
    fz_buffer *buffer = NULL;
    fz_var(object); fz_var(buffer);
    fz_try(ctx) {
        for (int number = 1; number < pdf_count_objects(ctx, doc); ++number) {
            pdf_xref_entry *entry = pdf_get_xref_entry(ctx, doc, number);
            if (entry->type != 'n' && entry->type != 'o') continue;
            object = pdf_load_object(ctx, doc, number);
            if (pdf_dict_get(ctx, object, PDF_NAME(Subtype)) == PDF_NAME(Form)) {
                pdf_obj *resources = pdf_dict_get(ctx, object, PDF_NAME(Resources));
                if (!resources) fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of inherited Form resources is not supported");
                pdf_obj *ref = pdf_new_indirect(ctx, doc, number, 0);
                fz_try(ctx) {
                    buffer = pv_without_tags(ctx, doc, ref, resources);
                    pdf_update_stream(ctx, doc, ref, buffer, 0);
                }
                fz_always(ctx) { pdf_drop_obj(ctx, ref); }
                fz_catch(ctx) { fz_rethrow(ctx); }
                fz_drop_buffer(ctx, buffer); buffer = NULL;
            }
            pdf_drop_obj(ctx, object); object = NULL;
        }
    }
    fz_always(ctx) { pdf_drop_obj(ctx, object); fz_drop_buffer(ctx, buffer); }
    fz_catch(ctx) { fz_rethrow(ctx); }
}

void pv_clean_document(fz_context *ctx, pdf_document *doc, int composed)
{
    pdf_obj *object = NULL;
    fz_buffer *buffer = NULL;
    pdf_page *page = NULL;
    fz_var(object);
    fz_var(buffer);
    fz_var(page);
    fz_try(ctx) {
        pdf_obj *root = pdf_dict_get(ctx, pdf_trailer(ctx, doc), PDF_NAME(Root));
        if (!composed && pdf_dict_get(ctx, root, PDF_NAME(OCProperties)))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of PDFs with optional or hidden layers is not supported");
        if (pdf_dict_getp(ctx, root, "AcroForm/XFA"))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of XFA forms is not supported");
        /* Bake visible appearances before their field dictionaries are dropped. */
        int original_count = pdf_count_objects(ctx, doc);
        pdf_bake_document(ctx, doc, 1, 1);
        int count = pdf_count_objects(ctx, doc);
        for (int number = 1; number < count; ++number) {
            pdf_xref_entry *entry = pdf_get_xref_entry(ctx, doc, number);
            if (entry->type != 'n' && entry->type != 'o') continue;
            object = pdf_load_object(ctx, doc, number);
            if (!composed && (pdf_dict_get(ctx, object, PDF_NAME(OC)) || pdf_dict_get(ctx, object, PDF_NAME(Type)) == PDF_NAME(OCG) || pdf_dict_get(ctx, object, PDF_NAME(Type)) == PDF_NAME(OCMD)))
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of optional-content resources is not supported");
            if (pdf_dict_get(ctx, object, PDF_NAME(Subtype)) == PDF_NAME(Type3))
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of Type 3 fonts is not supported");
            if (pdf_dict_get_bool(ctx, object, PDF_NAME(ImageMask)))
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of image stencils is not supported");
            if (pdf_dict_get_int(ctx, object, PDF_NAME(PatternType)) == 1)
                fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of tiling patterns is not supported");
            /* MuPDF-generated vector-only annotation appearances can omit an
             * empty Resources dictionary. Original inherited Forms stay rejected. */
            if (number >= original_count && pdf_dict_get(ctx, object, PDF_NAME(Subtype)) == PDF_NAME(Form) &&
                !pdf_dict_get(ctx, object, PDF_NAME(Resources)))
                pdf_dict_put_drop(ctx, object, PDF_NAME(Resources), pdf_new_dict(ctx, doc, 0));
            /* Keep property resources until all streams have been parsed. */
            pdf_drop_obj(ctx, object);
            object = NULL;
        }
        pv_clean_resource_tags(ctx, doc);
        int pages = pdf_count_pages(ctx, doc);
        for (int number = 0; number < pages; ++number) {
            page = pdf_load_page(ctx, doc, number);
            pv_clean_page_tags(ctx, doc, page);
            fz_drop_page(ctx, &page->super);
            page = NULL;
        }
        count = pdf_count_objects(ctx, doc);
        for (int number = 1; number < count; ++number) {
            pdf_xref_entry *entry = pdf_get_xref_entry(ctx, doc, number);
            if (entry->type != 'n' && entry->type != 'o') continue;
            object = pdf_load_object(ctx, doc, number);
            pv_clean_dict(ctx, object, 0);
            pdf_drop_obj(ctx, object);
            object = NULL;
        }
    }
    fz_always(ctx) {
        pdf_drop_obj(ctx, object);
        fz_drop_buffer(ctx, buffer);
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) { fz_rethrow(ctx); }
}
