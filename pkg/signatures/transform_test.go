package signatures

import (
	"bytes"
	img "emanuelelongo/proview/pkg/images"
	"image/color"
	"testing"
)

func TestVectorTransformAndRecolorPreserveArtwork(t *testing.T) {
	a, e := Import("shape.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><rect x="0" y="0" width="30" height="50" fill="#ff0000" fill-opacity="0.5"/></svg>`))
	if e != nil {
		t.Fatal(e)
	}
	changed, e := a.WithColor(color.NRGBA{B: 255, A: 255})
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(changed.Data, []byte("0.5")) {
		t.Fatal("opacity lost")
	}
	rotated, e := changed.Transformed(img.Transform{Turns: 1})
	if e != nil {
		t.Fatal(e)
	}
	p, e := rotated.Render(200)
	if e != nil {
		t.Fatal(e)
	}
	if p.Bounds().Dy() != 2*p.Bounds().Dx() {
		t.Fatalf("rotation size %v", p.Bounds())
	}
	c := color.NRGBAModel.Convert(p.At(p.Bounds().Dx()/2, 10)).(color.NRGBA)
	if c.B < 200 || c.A < 100 || c.A > 150 {
		t.Fatalf("wrong rotated paint: %v", c)
	}
	data, e := rotated.PDF()
	if e != nil || !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatalf("PDF: %v", e)
	}
	if bytes.Contains(data, []byte("/Subtype /Image")) {
		t.Fatal("vector was rasterized")
	}
	drawn, e := FromDrawing(exampleDrawing())
	if e != nil {
		t.Fatal(e)
	}
	colored, e := drawn.WithColor(color.NRGBA{R: 20, G: 80, A: 255})
	if e != nil {
		t.Fatal(e)
	}
	if colored.Drawing == nil || colored.Drawing.Smooth != drawn.Drawing.Smooth || colored.Drawing.Width != drawn.Drawing.Width {
		t.Fatal("drawing settings lost")
	}
	if e = colored.Validate(); e != nil {
		t.Fatal(e)
	}
}
