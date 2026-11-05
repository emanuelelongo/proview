//go:build largeimageexperiment && cgo

package imageregion

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func sample(t *testing.T) *JPEG {
	t.Helper()
	pixels := image.NewRGBA(image.Rect(0, 0, 123, 95))
	for y := 0; y < 95; y++ {
		for x := 0; x < 123; x++ {
			pixels.Set(x, y, color.RGBA{uint8(x * 2), uint8(y * 2), uint8(x + y), 255})
		}
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, pixels, nil); err != nil {
		t.Fatal(err)
	}
	source, err := Open(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestRegionsMatchFullNativeDecode(t *testing.T) {
	source := sample(t)
	for _, scale := range []int{1, 2, 4, 8} {
		full, err := source.Decode(context.Background(), Request{image.Rectangle{Max: source.Size()}, scale})
		if err != nil {
			t.Fatal(err)
		}
		for _, rect := range []image.Rectangle{image.Rect(13, 17, 87, 79), image.Rect(103, 81, 123, 95), image.Rect(-5, -7, 15, 23)} {
			region, err := source.Decode(context.Background(), Request{rect, scale})
			if err != nil {
				t.Fatal(err)
			}
			for y := region.Rect.Min.Y; y < region.Rect.Max.Y; y++ {
				for x := region.Rect.Min.X; x < region.Rect.Max.X; x++ {
					if region.RGBAAt(x, y) != full.RGBAAt(x, y) {
						t.Fatalf("scale %d pixel %d,%d differs", scale, x, y)
					}
				}
			}
		}
	}
}

func TestDecodeRejectsInvalidAndCanceledRequests(t *testing.T) {
	source := sample(t)
	for _, r := range []Request{{image.Rect(200, 200, 220, 220), 1}, {image.Rect(0, 0, 20, 20), 3}, {image.Rectangle{}, 1}} {
		if _, err := source.Decode(context.Background(), r); err == nil {
			t.Fatal("accepted invalid request")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Decode(ctx, Request{image.Rectangle{Max: source.Size()}, 1}); err != context.Canceled {
		t.Fatal(err)
	}
	corrupt := &JPEG{data: []byte("corrupt"), size: image.Pt(10, 10)}
	if _, err := corrupt.Decode(context.Background(), Request{image.Rect(0, 0, 10, 10), 1}); err == nil {
		t.Fatal("accepted corrupt stream")
	}
}

func TestDecoderRejectsProgressiveAndTruncatedStreams(t *testing.T) {
	source := sample(t)
	progressive := bytes.Clone(source.data)
	marker := bytes.Index(progressive, []byte{0xff, 0xc0})
	if marker < 0 {
		t.Fatal("missing baseline frame header")
	}
	// The header alone is sufficient to reject the progressive allocation path.
	progressive[marker+1] = 0xc2
	for _, data := range [][]byte{progressive, source.data[:len(source.data)/2]} {
		changed := &JPEG{data: data, size: source.size}
		if _, err := changed.Decode(context.Background(), Request{image.Rectangle{Max: source.size}, 1}); err == nil {
			t.Fatal("accepted unsupported or truncated stream")
		}
	}
}
