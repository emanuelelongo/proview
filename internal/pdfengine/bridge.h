#ifndef PROVIEW_PDF_ENGINE_BRIDGE_H
#define PROVIEW_PDF_ENGINE_BRIDGE_H

#include <stddef.h>

typedef struct {
    float x, y;
} pv_point;

typedef struct {
    pv_point ul, ur, ll, lr;
} pv_quad;

typedef struct {
    int codepoint, line;
    pv_quad quad;
} pv_glyph;

typedef struct {
    int page;
    pv_quad quad;
} pv_region;

typedef struct {
    unsigned char *bytes;
    size_t length;
    pv_glyph *glyphs;
    size_t glyph_count;
    float width, height;
    char error[512];
} pv_result;

void pv_layout(const unsigned char *data, size_t length, int page, pv_result *result);
void pv_trial_redact(const unsigned char *data, size_t length,
    const pv_region *regions, size_t count, pv_result *result);
void pv_check_source(const unsigned char *data, size_t length, pv_result *result);
void pv_redact_composed(const unsigned char *data, size_t length,
    const pv_region *regions, size_t count, pv_result *result);
void pv_redact(const unsigned char *data, size_t length,
    const pv_region *regions, size_t count, pv_result *result);
void pv_free_result(pv_result *result);

typedef struct {
    int page, id, kind, flags, max_len;
    float x, y, width, height;
    char *name, *value, *on_value, *caption;
    char **values, **defaults, **reset_fields;
    int value_count, default_count, reset_count, reset_exclude;
    char **options, **export_options;
    int option_count;
} pv_field;
typedef struct {
    pv_field *fields;
    size_t count;
    char error[512];
} pv_form_result;
void pv_read_forms(const unsigned char *data, size_t length, pv_form_result *result);
void pv_free_forms(pv_form_result *result);
typedef struct { int id, count; char **values; } pv_form_edit;
void pv_fill_form(const unsigned char *data, size_t length,
    const pv_form_edit *edits, size_t count, pv_result *result);
void pv_render_forms(const unsigned char *data, size_t length, int page, float dpi, pv_result *result);
#endif
