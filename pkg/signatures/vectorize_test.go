package signatures

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"
)

func traceFixture() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 70))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	// Closed ink loop, disconnected dot, one-pixel diagonal and isolated noise.
	draw.Draw(img, image.Rect(20, 15, 55, 50), image.NewUniform(color.Black), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(24, 19, 51, 46), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(65, 12, 68, 15), image.NewUniform(color.Black), image.Point{}, draw.Src)
	for i := 0; i < 20; i++ {
		img.Set(65+i, 30+i, color.Black)
	}
	img.Set(2, 2, color.Black)
	return img
}

func TestTracePreservesHolesDotsThinLinesAndTransparentPaper(t *testing.T) {
	for _, smooth := range []float64{0, 35, 100} {
		scan := Scan{Image: traceFixture()}
		asset, err := scan.Vectorize(context.Background(), TraceOptions{Intensity: 50, Smooth: smooth})
		if err != nil {
			t.Fatal(err)
		}
		if asset.Kind != SVG || bytes.Contains(asset.Data, []byte("<image")) {
			t.Fatal("result is not true vector geometry")
		}
		// The tight SVG viewport is [19,11 .. 86,51]. Render at exactly one pixel
		// per SVG unit to check original ink locations without changing geometry.
		rendered, err := asset.Render(67)
		if err != nil {
			t.Fatal(err)
		}
		alpha := func(x, y int) uint32 { _, _, _, a := rendered.At(x-19, y-11).RGBA(); return a }
		if alpha(35, 30) != 0 || alpha(58, 22) != 0 {
			t.Fatal("loop or background lost transparency")
		}
		if alpha(21, 30) == 0 || alpha(66, 13) == 0 || alpha(75, 40) == 0 {
			t.Fatalf("lost ring, dot or thin diagonal at smoothing %.0f", smooth)
		}
		pdf, err := asset.PDF()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(pdf, []byte("%PDF")) || bytes.Contains(pdf, []byte("/Subtype /Image")) {
			t.Fatal("signature PDF contains a raster fallback")
		}
	}
}

func TestTraceCropExcludesOtherMarksAndKeepsSourceImmutable(t *testing.T) {
	img := traceFixture()
	before := bytes.Clone(img.Pix)
	a, err := (Scan{Image: img}).Vectorize(context.Background(), TraceOptions{Crop: image.Rect(15, 10, 58, 55), Intensity: 50, Smooth: 35})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(a.Data, []byte("M")) != 2 {
		t.Fatalf("crop included another mark or lost the hole: %s", a.Data)
	}
	if !bytes.Equal(before, img.Pix) {
		t.Fatal("trace changed original scan")
	}
}

func TestTraceRejectsBlankCanceledAndInvalidInputs(t *testing.T) {
	blank := image.NewRGBA(image.Rect(0, 0, 50, 20))
	draw.Draw(blank, blank.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if _, err := (Scan{Image: blank}).Vectorize(context.Background(), TraceOptions{Intensity: 50}); err == nil {
		t.Fatal("blank scan accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Scan{Image: traceFixture()}).Vectorize(ctx, TraceOptions{Intensity: 50}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if _, err := (Scan{Image: traceFixture()}).Vectorize(context.Background(), TraceOptions{Intensity: 101}); err == nil {
		t.Fatal("invalid intensity accepted")
	}
	if _, err := (Scan{Image: traceFixture()}).Vectorize(context.Background(), TraceOptions{Crop: image.Rect(500, 500, 600, 600)}); err == nil {
		t.Fatal("outside crop accepted")
	}
}

func TestTraceCompensatesShadingAndTransparentPNG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 120, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			value := uint8(150 + x*100/120)
			img.SetNRGBA(x, y, color.NRGBA{R: value, G: value, B: value, A: 255})
		}
	}
	// A thin blue signature over gradually shaded paper.
	for x := 20; x < 100; x++ {
		for y := 28; y < 31; y++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 50, G: 60, B: 100, A: 255})
		}
	}
	a, err := (Scan{Image: img}).Vectorize(context.Background(), TraceOptions{Intensity: 50, Smooth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(a.Data, []byte("M")) != 1 {
		t.Fatalf("paper shading was traced as ink: %s", a.Data)
	}
	transparent := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	draw.Draw(transparent, image.Rect(5, 8, 35, 11), image.NewUniform(color.Black), image.Point{}, draw.Src)
	if _, err := (Scan{Image: transparent}).Vectorize(context.Background(), TraceOptions{Intensity: 50}); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeSignatureScanJPEGAndPNG(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		var data bytes.Buffer
		if format == "jpeg" {
			if err := jpeg.Encode(&data, traceFixture(), nil); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := png.Encode(&data, traceFixture()); err != nil {
				t.Fatal(err)
			}
		}
		scan, err := DecodeScan(data.Bytes())
		if err != nil || scan.Image.Bounds() != traceFixture().Bounds() {
			t.Fatalf("decode %s: %v", format, err)
		}
	}
	if _, err := DecodeScan([]byte("not an image")); err == nil {
		t.Fatal("invalid input accepted")
	}
}

func TestSensitivityExtendsFilteringAndFaintInkWithoutChangingMidpoint(t *testing.T) {
	if scanContrastThreshold(50) != 27.5 {
		t.Fatal("default sensitivity changed")
	}
	makeInk := func(value uint8) Scan {
		img := image.NewNRGBA(image.Rect(0, 0, 80, 30))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(10, 12, 70, 15), image.NewUniform(color.NRGBA{R: value, G: value, B: value, A: 255}), image.Point{}, draw.Src)
		return Scan{Image: img}
	}
	for _, test := range []struct {
		ink         uint8
		sensitivity float64
		found       bool
	}{{150, 0, false}, {150, 50, true}, {252, 50, false}, {252, 100, true}} {
		_, err := makeInk(test.ink).Vectorize(context.Background(), TraceOptions{Intensity: test.sensitivity, Smooth: 100})
		if (err == nil) != test.found {
			t.Fatalf("ink %d at sensitivity %.0f: error %v", test.ink, test.sensitivity, err)
		}
	}
}
