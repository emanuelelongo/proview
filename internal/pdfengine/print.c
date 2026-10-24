#include "bridge.h"
#include "mupdf/fitz.h"
#include "mupdf/pdf.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>

static void print_silent_message(void *opaque, const char *message)
{
    (void)opaque; (void)message;
}

void pv_render_print(const unsigned char *data, size_t length, int number, float dpi, pv_result *result)
{
    fz_context *ctx = fz_new_context(NULL, NULL, FZ_STORE_DEFAULT);
    fz_stream *stream = NULL;
    pdf_document *doc = NULL;
    pdf_page *page = NULL;
    fz_pixmap *pixmap = NULL;
    fz_device *device = NULL;
    fz_buffer *buffer = NULL;
    fz_var(stream); fz_var(doc); fz_var(page);
    fz_var(pixmap); fz_var(device); fz_var(buffer);
    if (!ctx) { snprintf(result->error, sizeof(result->error), "Cannot create print context"); return; }
    fz_set_error_callback(ctx, print_silent_message, NULL);
    fz_set_warning_callback(ctx, print_silent_message, NULL);
    fz_try(ctx) {
        stream = fz_open_memory(ctx, data, length);
        doc = pdf_open_document_with_stream(ctx, stream);
        if (pdf_needs_password(ctx, doc)) fz_throw(ctx, FZ_ERROR_ARGUMENT, "Encrypted print documents are unsupported");
        page = pdf_load_page(ctx, doc, number);
        fz_matrix matrix = fz_scale(dpi / 72, dpi / 72);
        fz_irect box = fz_round_rect(fz_transform_rect(fz_bound_page(ctx, (fz_page *)page), matrix));
        int64_t width = (int64_t)box.x1 - box.x0, height = (int64_t)box.y1 - box.y0;
        if (width <= 0 || height <= 0 || width > 33000000 || height > 33000000 || width * height > 33000000)
            fz_throw(ctx, FZ_ERROR_LIMIT, "Print page exceeds rendering limits");
        pixmap = fz_new_pixmap_with_bbox(ctx, fz_device_rgb(ctx), box, NULL, 0);
        fz_clear_pixmap_with_value(ctx, pixmap, 255);
        device = fz_new_draw_device(ctx, matrix, pixmap);
        pdf_run_page_with_usage(ctx, page, device, fz_identity, "Print", NULL);
        fz_close_device(ctx, device);
        buffer = fz_new_buffer_from_pixmap_as_png(ctx, pixmap, fz_default_color_params);
        unsigned char *bytes = NULL;
        result->length = fz_buffer_storage(ctx, buffer, &bytes);
        result->bytes = malloc(result->length);
        if (!result->bytes) fz_throw(ctx, FZ_ERROR_SYSTEM, "Cannot allocate print page");
        memcpy(result->bytes, bytes, result->length);
    }
    fz_always(ctx) {
        fz_drop_buffer(ctx, buffer);
        fz_drop_device(ctx, device);
        fz_drop_pixmap(ctx, pixmap);
        fz_drop_page(ctx, (fz_page *)page);
        pdf_drop_document(ctx, doc);
        fz_drop_stream(ctx, stream);
    }
    fz_catch(ctx) { snprintf(result->error, sizeof(result->error), "%s", fz_caught_message(ctx)); }
    fz_drop_context(ctx);
}
