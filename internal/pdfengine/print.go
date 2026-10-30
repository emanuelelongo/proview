//go:build cgo && !nocgo && !extlib

package pdfengine

/*
#include "bridge.h"
#include <stdlib.h>
void pv_render_print(const unsigned char *data, size_t length, int page, float dpi, pv_result *result);
*/
import "C"

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"math"
	"unsafe"
)

// RenderPrintPage observes PDF Print usage and annotation print flags. It is
// used only for a printer device context; saved PDFs retain their vector data.
func RenderPrintPage(data []byte, page int, dpi float64) (image.Image, error) {
	if len(data) == 0 || page < 0 || page > math.MaxInt32 || math.IsNaN(dpi) || dpi <= 0 || dpi > 600 {
		return nil, errors.New("pdfengine: invalid print page")
	}
	input := cBytes(data)
	var result C.pv_result
	defer C.pv_free_result(&result)
	C.pv_render_print((*C.uchar)(input), C.size_t(len(data)), C.int(page), C.float(dpi), &result)
	if err := resultError(&result); err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(C.GoBytes(unsafe.Pointer(result.bytes), C.int(result.length))))
}
