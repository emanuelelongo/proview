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

static void pv_clean_resource_forms(fz_context *ctx, pdf_document *doc, pdf_obj *resources, int depth);

/* Same rules as pv_clean_resource_tags, applied to one reachable Form. */
static void pv_clean_form(fz_context *ctx, pdf_document *doc, pdf_obj *xobj, int depth)
{
    if (!pdf_is_stream(ctx, xobj) || pdf_dict_get(ctx, xobj, PDF_NAME(Subtype)) != PDF_NAME(Form))
        return;
    if (pdf_mark_obj(ctx, xobj))
        return;
    fz_buffer *buffer = NULL;
    fz_var(buffer);
    fz_try(ctx) {
        pdf_obj *form_resources = pdf_dict_get(ctx, xobj, PDF_NAME(Resources));
        if (!form_resources)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction of inherited Form resources is not supported");
        pv_clean_resource_forms(ctx, doc, form_resources, depth + 1);
        buffer = pv_without_tags(ctx, doc, xobj, form_resources);
        pdf_update_stream(ctx, doc, xobj, buffer, 0);
    }
    fz_always(ctx) {
        fz_drop_buffer(ctx, buffer);
        pdf_unmark_obj(ctx, xobj);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
}

static void pv_clean_resource_forms(fz_context *ctx, pdf_document *doc, pdf_obj *resources, int depth)
{
    if (!resources) return;
    if (depth > 64)
        fz_throw(ctx, FZ_ERROR_ARGUMENT, "PDF resource nesting is not supported for redaction");
    if (pdf_mark_obj(ctx, resources)) return;
    fz_try(ctx) {
        pdf_obj *xobjects = pdf_dict_get(ctx, resources, PDF_NAME(XObject));
        int count = pdf_dict_len(ctx, xobjects);
        for (int i = 0; i < count; ++i)
            pv_clean_form(ctx, doc, pdf_dict_get_val(ctx, xobjects, i), depth);
    }
    fz_always(ctx) {
        pdf_unmark_obj(ctx, resources);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
}

/* Text layout needs only the Forms its page can draw: page resources and
 * annotation appearances. pv_clean_document still walks every xref object. */
void pv_clean_page_resource_tags(fz_context *ctx, pdf_document *doc, int page_number)
{
    pdf_obj *page = pdf_lookup_page_obj(ctx, doc, page_number);
    pv_clean_resource_forms(ctx, doc, pdf_dict_get_inheritable(ctx, page, PDF_NAME(Resources)), 0);
    pdf_obj *annots = pdf_dict_get(ctx, page, PDF_NAME(Annots));
    int count = pdf_array_len(ctx, annots);
    for (int i = 0; i < count; ++i) {
        pdf_obj *ap = pdf_dict_get(ctx, pdf_array_get(ctx, annots, i), PDF_NAME(AP));
        int kinds = pdf_dict_len(ctx, ap);
        for (int k = 0; k < kinds; ++k) {
            pdf_obj *appearance = pdf_dict_get_val(ctx, ap, k);
            if (pdf_is_stream(ctx, appearance)) {
                pv_clean_form(ctx, doc, appearance, 0);
                continue;
            }
            int states = pdf_dict_len(ctx, appearance);
            for (int s = 0; s < states; ++s)
                pv_clean_form(ctx, doc, pdf_dict_get_val(ctx, appearance, s), 0);
        }
    }
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
