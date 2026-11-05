#include <stddef.h>

typedef struct region_cancel region_cancel;
region_cancel *region_cancel_new(void);
void region_cancel_set(region_cancel *flag);
void region_cancel_free(region_cancel *flag);
int region_decode(const unsigned char *data, size_t length, int denominator,
                  int x, int y, int width, int height, unsigned char *pixels,
                  region_cancel *cancel, char *message);
