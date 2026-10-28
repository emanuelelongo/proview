package images

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func BenchmarkTransformLargeImage(b *testing.B) {
	for _, tc := range []struct {
		name string
		src  image.Image
	}{
		{"RGBA", image.NewRGBA(image.Rect(0, 0, 2048, 1536))},
		{"NRGBA", image.NewNRGBA(image.Rect(0, 0, 2048, 1536))},
		{"JPEG", image.NewYCbCr(image.Rect(0, 0, 2048, 1536), image.YCbCrSubsampleRatio420)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result := (Transform{Turns: 1}).Apply(tc.src)
				if result.Bounds().Dx() != 1536 {
					b.Fatal("wrong bounds")
				}
			}
		})
	}
}

// Hiding typed access exercises the original generic conversion as an oracle.
type genericImage struct{ image.Image }

func TestTransformTypedPixelsMatchGeneric(t *testing.T) {
	bounds := image.Rect(4, 7, 13, 12)
	rgba := image.NewRGBA(bounds)
	nrgba := image.NewNRGBA(bounds)
	rgba64 := image.NewRGBA64(bounds)
	nrgba64 := image.NewNRGBA64(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBA{R: uint8(x * 17), G: uint8(y * 31), B: uint8(x * y), A: uint8((x + y) * 23)}
			rgba.Set(x, y, c)
			nrgba.SetNRGBA(x, y, c)
			rgba64.Set(x, y, c)
			nrgba64.Set(x, y, c)
		}
	}
	jpeg := image.NewYCbCr(bounds, image.YCbCrSubsampleRatio420)
	for i := range jpeg.Y {
		jpeg.Y[i] = uint8(i * 17)
	}
	for i := range jpeg.Cb {
		jpeg.Cb[i] = uint8(i * 31)
		jpeg.Cr[i] = uint8(i * 53)
	}
	for _, src := range []image.Image{rgba, nrgba, rgba64, nrgba64, jpeg} {
		for turns := 0; turns < 4; turns++ {
			for _, flipX := range []bool{false, true} {
				for _, flipY := range []bool{false, true} {
					transform := Transform{Turns: turns, FlipX: flipX, FlipY: flipY}
					if transform == (Transform{}) {
						continue
					}
					got := transform.Apply(src).(*image.NRGBA)
					want := transform.Apply(genericImage{src}).(*image.NRGBA)
					if got.Rect != want.Rect || !bytes.Equal(got.Pix, want.Pix) {
						t.Fatalf("pixel mismatch for %T, %+v", src, transform)
					}
				}
			}
		}
	}
}
