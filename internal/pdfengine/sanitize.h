#ifndef PROVIEW_PDF_SANITIZE_H
#define PROVIEW_PDF_SANITIZE_H
#include "mupdf/pdf.h"
#include "bridge.h"

void pv_clean_resource_tags(fz_context *ctx, pdf_document *doc);
void pv_clean_page_resource_tags(fz_context *ctx, pdf_document *doc, int page_number);
void pv_clean_page_tags(fz_context *ctx, pdf_document *doc, pdf_page *page);
void pv_clean_document(fz_context *ctx, pdf_document *doc, int composed);
void pv_clean_images(fz_context *ctx, pdf_document *doc, const pv_region *regions, size_t count);
#endif
