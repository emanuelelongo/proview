//go:build largeimageexperiment && cgo

// Package imageregion is an isolated JPEG viewport experiment. Coordinates refer
// to encoded pixels, before EXIF orientation; it does not alter document loading.
package imageregion

/*
#cgo pkg-config: libjpeg
#cgo CFLAGS: -std=c11
#include "native.h"
*/
import "C"

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"unsafe"
)

const MaxOutputPixels = 8_000_000

// Request selects an original-pixel rectangle and a native JPEG scale. The
// returned rectangle uses scaled coordinates, rounded out to cover the request.
type Request struct {
	Rect        image.Rectangle
	Denominator int
}

type JPEG struct {
	data []byte
	size image.Point
}

// Open takes ownership of immutable compressed data. No full raster is decoded.
func Open(data []byte) (*JPEG, error) {
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 300_000_000 {
		return nil, fmt.Errorf("source exceeds experiment's 300 megapixel limit")
	}
	return &JPEG{data: data, size: image.Pt(config.Width, config.Height)}, nil
}

func (j *JPEG) Size() image.Point { return j.size }

func (j *JPEG) Decode(ctx context.Context, request Request) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d := request.Denominator
	if d != 1 && d != 2 && d != 4 && d != 8 {
		return nil, fmt.Errorf("scale denominator must be 1, 2, 4 or 8")
	}
	r := request.Rect.Intersect(image.Rectangle{Max: j.size})
	if r.Empty() {
		return nil, fmt.Errorf("empty image region")
	}
	r = image.Rect(r.Min.X/d, r.Min.Y/d, (r.Max.X+d-1)/d, (r.Max.Y+d-1)/d)
	if int64(r.Dx())*int64(r.Dy()) > MaxOutputPixels {
		return nil, fmt.Errorf("region exceeds output pixel budget")
	}
	flag := C.region_cancel_new()
	if flag == nil {
		return nil, fmt.Errorf("cancellation allocation failed")
	}
	// Exactly one bounded watcher per active decode, joined before freeing C state.
	done, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			C.region_cancel_set(flag)
		case <-done:
		}
	}()
	output := image.NewRGBA(r)
	var message [256]C.char
	status := C.region_decode((*C.uchar)(unsafe.Pointer(&j.data[0])), C.size_t(len(j.data)), C.int(d),
		C.int(r.Min.X), C.int(r.Min.Y), C.int(r.Dx()), C.int(r.Dy()),
		(*C.uchar)(unsafe.Pointer(&output.Pix[0])), flag, &message[0])
	close(done)
	<-joined
	C.region_cancel_free(flag)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if status != 0 {
		return nil, fmt.Errorf("JPEG region: %s", C.GoString(&message[0]))
	}
	return output, nil
}
