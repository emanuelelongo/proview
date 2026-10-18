package document

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
)

func TestLensSamplingDetailIsBounded(t *testing.T) {
	for _, size := range []image.Point{image.Pt(1200, 1600), image.Pt(20000, 20000), image.Pt(20, 20000)} {
		dpi := lensSamplingDPI(144, 8, size)
		scale := dpi / 144
		if dpi > 600 || float64(size.X)*float64(size.Y)*scale*scale > 16_000_001 || float64(max(size.X, size.Y))*scale > 8192.01 {
			t.Fatalf("unbounded lens sampling: size=%v dpi=%g", size, dpi)
		}
	}
	if dpi := lensSamplingDPI(144, 2, image.Pt(1200, 1600)); math.Abs(dpi-288) > .001 {
		t.Fatalf("lens did not increase source detail: %g", dpi)
	}
}

func TestLensResolvesPDFDetailBeforeMagnification(t *testing.T) {
	ctx, err := newAppearanceContext(100, 80)
	if err != nil {
		t.Fatal(err)
	}
	page, _, _, err := ctx.PageDict(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	var content strings.Builder
	for x := 40.; x < 60; x += .5 {
		fmt.Fprintf(&content, "0 g %.3f 30 .15 20 re f\n", x)
	}
	ref, err := addPDFStream(ctx, []byte(content.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	page["Contents"] = *ref
	raw, err := writePDFContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Load("fine-detail.pdf", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	data := annotations.Default(annotations.Shape)
	data.Shape = annotations.Magnifier
	data.Magnification = 4
	data.PageWidth, data.PageHeight = 100, 80
	data.Points = []annotations.Point{{X: 20, Y: 10}, {X: 80, Y: 70}}
	lens, err := doc.AddAnnotation(0, NormalizedRect{X: .2, Y: .125, Width: .6, Height: .75}, data, prepareAnnotation(t, data))
	if err != nil {
		t.Fatal(err)
	}
	picture, err := doc.RenderLens(lens.ID, 144)
	if err != nil {
		t.Fatal(err)
	}
	if picture.Bounds().Size() != image.Pt(120, 120) {
		t.Fatalf("detail rendering changed lens dimensions: %v", picture.Bounds())
	}
	low, high := uint8(255), uint8(0)
	for x := 20; x < 100; x++ {
		c := color.NRGBAModel.Convert(picture.At(x, 60)).(color.NRGBA)
		low = min(low, c.R)
		high = max(high, c.R)
	}
	if low > 80 || high < 240 {
		t.Fatalf("magnifier lost fine vector contrast: min=%d max=%d", low, high)
	}
	if _, _, _, a := picture.At(0, 0).RGBA(); a != 0 {
		t.Fatal("high resolution lens exposes corner pixels")
	}
}
