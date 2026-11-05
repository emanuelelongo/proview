//go:build largeimageexperiment && cgo

#include "native.h"
#include <stdio.h>
#include <jpeglib.h>
#include <setjmp.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

struct region_cancel { atomic_int value; };
region_cancel *region_cancel_new(void) {
    region_cancel *flag = malloc(sizeof(*flag));
    if (flag) atomic_init(&flag->value, 0);
    return flag;
}
void region_cancel_set(region_cancel *flag) { atomic_store(&flag->value, 1); }
void region_cancel_free(region_cancel *flag) { free(flag); }

typedef struct {
    struct jpeg_decompress_struct jpeg;
    struct jpeg_error_mgr error;
    struct jpeg_progress_mgr progress;
    jmp_buf jump;
    region_cancel *cancel;
    unsigned char *row;
    char *message;
    int status;
} decoder;

static void fail(j_common_ptr common) {
    decoder *d = common->client_data;
    (*common->err->format_message)(common, d->message);
    longjmp(d->jump, 1);
}

static void check(j_common_ptr common) {
    decoder *d = common->client_data;
    if (atomic_load(&d->cancel->value)) {
        d->status = 2;
        longjmp(d->jump, 1);
    }
}

static void warning(j_common_ptr common, int level) {
    if (level < 0) fail(common);
}

int region_decode(const unsigned char *data, size_t length, int denominator,
                  int x, int y, int width, int height, unsigned char *pixels,
                  region_cancel *cancel, char *message) {
    // Heap-owned state remains valid after libjpeg's error longjmp.
    decoder *d = calloc(1, sizeof(*d));
    if (!d) { strcpy(message, "decoder allocation failed"); return 1; }
    d->cancel = cancel;
    d->message = message;
    d->status = 1;
    d->jpeg.err = jpeg_std_error(&d->error);
    d->error.error_exit = fail;
    // Treat truncated/corrupt streams as failures; suppress stderr callbacks.
    d->error.emit_message = warning;
    d->jpeg.client_data = d;
    if (setjmp(d->jump)) goto cleanup;
    jpeg_create_decompress(&d->jpeg);
    // create_decompress preserves client_data.
    d->progress.progress_monitor = check;
    d->jpeg.progress = &d->progress;
    check((j_common_ptr)&d->jpeg);
    jpeg_mem_src(&d->jpeg, data, length);
    jpeg_read_header(&d->jpeg, TRUE);
    if (d->jpeg.progressive_mode || d->jpeg.arith_code || d->jpeg.num_components > 3) {
        strcpy(message, "experiment supports sequential grayscale/RGB JPEG only");
        goto cleanup;
    }
    d->jpeg.scale_num = 1;
    d->jpeg.scale_denom = denominator;
    d->jpeg.out_color_space = JCS_EXT_RGBA;
    // Disable fancy upsampling to make regional and full decode pixels agree.
    d->jpeg.do_fancy_upsampling = FALSE;
    jpeg_start_decompress(&d->jpeg);
    if (x < 0 || y < 0 || width < 1 || height < 1 ||
        (size_t)x + width > d->jpeg.output_width ||
        (size_t)y + height > d->jpeg.output_height) {
        strcpy(message, "region exceeds decoded dimensions"); goto cleanup;
    }
    JDIMENSION left = x, span = width;
    jpeg_crop_scanline(&d->jpeg, &left, &span);
    d->row = malloc((size_t)span * 4);
    if (!d->row) { strcpy(message, "scanline allocation failed"); goto cleanup; }
    while (d->jpeg.output_scanline < (JDIMENSION)y) {
        check((j_common_ptr)&d->jpeg);
        JDIMENSION count = y - d->jpeg.output_scanline;
        jpeg_skip_scanlines(&d->jpeg, count > 128 ? 128 : count);
    }
    for (int row = 0; row < height; row++) {
        check((j_common_ptr)&d->jpeg);
        JSAMPROW scanline = d->row;
        jpeg_read_scanlines(&d->jpeg, &scanline, 1);
        memcpy(pixels + (size_t)row * width * 4,
               d->row + (size_t)(x - left) * 4, (size_t)width * 4);
    }
    // Do not decode the remaining rows just to finish the stream.
    d->status = 0;
cleanup:
    jpeg_destroy_decompress(&d->jpeg);
    free(d->row);
    int status = d->status;
    free(d);
    return status;
}
