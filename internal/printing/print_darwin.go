//go:build darwin && cgo

package printing

/*
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework PDFKit
#include <stdint.h>
#include <stdlib.h>
int proviewPrintPDF(const char *path, const char *title, uintptr_t parent, char *error, int capacity);
*/
import "C"

import (
	"context"
	"fmt"
	"unsafe"
)

func printNative(ctx context.Context, job Job, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, title := C.CString(path), C.CString(job.Title)
	defer C.free(unsafe.Pointer(file))
	defer C.free(unsafe.Pointer(title))
	var message [1024]C.char
	result := C.proviewPrintPDF(file, title, C.uintptr_t(job.ParentWindow), &message[0], C.int(len(message)))
	if result < 0 {
		return fmt.Errorf("printing: %s", C.GoString(&message[0]))
	}
	if result == 0 {
		return ErrCanceled
	}
	return nil
}
