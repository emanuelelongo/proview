#include "bridge.h"

void pv_correct_text(const unsigned char *data, size_t length, int page,
    int start, int count, const int *replacement, int replacement_count,
    float dx, float dy, pv_result *result);

void pv_duplicate_text(const unsigned char *data, size_t length, int page,
    int start, int count, const int *replacement, int replacement_count,
    float dx, float dy, pv_result *result);
