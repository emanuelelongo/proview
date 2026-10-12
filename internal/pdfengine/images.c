#include "sanitize.h"
#include <math.h>
#include <string.h>

typedef struct {
    int number;
    fz_pixmap *pixels;
    fz_image *replacement;
} pv_image_entry;

typedef struct {
    const pv_region *regions;
    size_t count;
    int page, rewrite;
    fz_rect bounds;
    fz_matrix to_pdf;
    pv_image_entry *entries;
    size_t length;
} pv_images;

static fz_point pv_page_point(pv_point p, fz_rect bounds)
{
    return (fz_point){bounds.x0 + p.x * (bounds.x1 - bounds.x0),
        bounds.y0 + p.y * (bounds.y1 - bounds.y0)};
}

static void pv_rewrite_image(fz_context *ctx, void *opaque, fz_image **image,
    fz_matrix ctm, pdf_obj *object)
{
    pv_images *state = opaque;
    int number = pdf_to_num(ctx, object);
    pv_image_entry *entry = NULL;
    for (size_t i = 0; i < state->length; ++i)
        if (state->entries[i].number == number) { entry = &state->entries[i]; break; }
    if (state->rewrite) {
        if (entry) {
            fz_drop_image(ctx, *image);
            *image = fz_keep_image(ctx, entry->replacement);
        }
        return;
    }
    fz_matrix to_image = fz_concat(state->to_pdf, fz_invert_matrix(ctm));
    for (size_t i = 0; i < state->count; ++i) {
        if (state->regions[i].page != state->page) continue;
        pv_quad q = state->regions[i].quad;
        fz_quad quad = {pv_page_point(q.ul, state->bounds), pv_page_point(q.ur, state->bounds),
            pv_page_point(q.ll, state->bounds), pv_page_point(q.lr, state->bounds)};
        fz_rect area = fz_intersect_rect(fz_transform_rect(fz_rect_from_quad(quad), to_image), fz_unit_rect);
        if (fz_is_empty_rect(area)) continue;
        if (!number)
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction overlapping inline images is not supported");
        if ((*image)->mask || (*image)->imagemask || pdf_dict_get(ctx, object, PDF_NAME(SMask)) || pdf_dict_get(ctx, object, PDF_NAME(Mask)))
            fz_throw(ctx, FZ_ERROR_ARGUMENT, "Redaction overlapping masked images is not supported; remove the overlapping image or overlay first");
        if (!entry) {
            fz_pixmap *original = NULL;
            fz_var(original);
            state->entries = fz_realloc_array(ctx, state->entries, state->length + 1, pv_image_entry);
            entry = &state->entries[state->length++];
            memset(entry, 0, sizeof(*entry));
            entry->number = number;
            fz_try(ctx) {
                original = fz_get_unscaled_pixmap_from_image(ctx, *image);
                entry->pixels = fz_convert_pixmap(ctx, original, fz_device_rgb(ctx), NULL,
                    NULL, fz_default_color_params, 0);
            }
            fz_always(ctx) { fz_drop_pixmap(ctx, original); }
            fz_catch(ctx) { fz_rethrow(ctx); }
        }
        fz_pixmap *pixels = entry->pixels;
        int width = fz_pixmap_width(ctx, pixels), height = fz_pixmap_height(ctx, pixels);
        int components = fz_pixmap_components(ctx, pixels);
        int stride = fz_pixmap_stride(ctx, pixels);
        unsigned char *samples = fz_pixmap_samples(ctx, pixels);
        int x0 = fz_clampi((int)floorf(area.x0 * width), 0, width);
        int x1 = fz_clampi((int)ceilf(area.x1 * width), 0, width);
        int y0 = fz_clampi((int)floorf((1 - area.y1) * height), 0, height);
        int y1 = fz_clampi((int)ceilf((1 - area.y0) * height), 0, height);
        for (int y = y0; y < y1; ++y)
            memset(samples + (size_t)y * stride + (size_t)x0 * components, 0,
                (size_t)(x1 - x0) * components);
    }
}

/* One map for the entire document: any affected image is rewritten in every
 * occurrence, including clipped reuses and multiple occurrences on one page.
 * No full PDF page is rasterized. Unselected image samples retain their size. */
void pv_clean_images(fz_context *ctx, pdf_document *doc, const pv_region *regions, size_t count)
{
    pv_images state = {0};
    state.regions = regions;
    state.count = count;
    pdf_page *page = NULL;
    fz_var(page);
    fz_var(state);
    fz_try(ctx) {
        pdf_color_filter_options color = {0};
        color.opaque = &state;
        color.image_rewrite = pv_rewrite_image;
        color.repeated_image_rewrite = 1;
        pdf_filter_factory filters[] = {{pdf_new_color_filter, &color}, {NULL, NULL}};
        pdf_filter_options options = {0};
        options.filters = filters;
        options.no_update = 1;
        for (int number = 0; number < pdf_count_pages(ctx, doc); ++number) {
            page = pdf_load_page(ctx, doc, number);
            state.page = number;
            state.bounds = fz_bound_page(ctx, &page->super);
            fz_matrix transform;
            fz_rect media;
            pdf_page_transform(ctx, page, &media, &transform);
            state.to_pdf = fz_invert_matrix(transform);
            pdf_filter_page_contents(ctx, doc, page, &options);
            fz_drop_page(ctx, &page->super);
            page = NULL;
        }
        if (state.length) {
            for (size_t i = 0; i < state.length; ++i)
                state.entries[i].replacement = fz_new_image_from_pixmap(ctx, state.entries[i].pixels, NULL);
            state.rewrite = 1;
            options.no_update = 0;
            for (int number = 0; number < pdf_count_pages(ctx, doc); ++number) {
                page = pdf_load_page(ctx, doc, number);
                pdf_filter_page_contents(ctx, doc, page, &options);
                fz_drop_page(ctx, &page->super);
                page = NULL;
            }
        }
    }
    fz_always(ctx) {
        fz_drop_page(ctx, (fz_page *)page);
        for (size_t i = 0; i < state.length; ++i) {
            fz_drop_pixmap(ctx, state.entries[i].pixels);
            fz_drop_image(ctx, state.entries[i].replacement);
        }
        fz_free(ctx, state.entries);
    }
    fz_catch(ctx) { fz_rethrow(ctx); }
}
