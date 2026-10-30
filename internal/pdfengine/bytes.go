//go:build cgo && !nocgo && !extlib

package pdfengine

/*
#include "bridge.h"
*/
import "C"
import "unsafe"

// cBytes passes the immutable Go buffer to C without copying it. Every bridge
// function drops its document, streams and context before returning, so the
// pointer is never retained across the call. This avoids a full-file copy for
// large PDFs on layout, form, print and text-edit paths.
func cBytes(data []byte) *C.uchar {
	if len(data) == 0 {
		return nil
	}
	return (*C.uchar)(unsafe.Pointer(&data[0]))
}
