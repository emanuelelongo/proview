package signatures

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestImportedThicknessPreservesVectorsAndRestoresOriginal(t *testing.T) {
	for _, artwork := range []string{
		`<path d="M10 50L90 50" fill="none" stroke="#204080" stroke-width="2"/>`,
		`<path d="M10 40H90V60H10Z M40 45V55H60V45Z" fill="#204080" fill-rule="evenodd"/>`,
	} {
		a, err := Import("signature.svg", []byte(`<svg viewBox="0 0 100 100">`+artwork+`</svg>`))
		if err != nil {
			t.Fatal(err)
		}
		original, _ := a.Render(1024)
		thick, err := a.WithThickness(6)
		if err != nil {
			t.Fatal(err)
		}
		preview, err := thick.Render(1024)
		if err != nil {
			t.Fatal(err)
		}
		if alphaArea(preview) <= alphaArea(original) || !bytes.Equal(thick.Data, a.Data) {
			t.Fatal("thickness did not expand original artwork")
		}
		pdf, err := thick.PDF()
		if err != nil || bytes.Contains(pdf, []byte("/Subtype /Image")) {
			t.Fatal("thickness rasterized the signature")
		}
		colored, err := thick.WithColor(color.NRGBA{R: 255, A: 255})
		if err != nil || colored.Thickness != 6 {
			t.Fatal("recoloring lost thickness")
		}
		restored, err := thick.WithThickness(0)
		if err != nil {
			t.Fatal(err)
		}
		plain, _ := restored.Render(1024)
		if alphaArea(plain) != alphaArea(original) || !bytes.Equal(restored.Data, a.Data) {
			t.Fatal("zero did not restore exact artwork")
		}
		if _, err := a.WithThickness(-1); err == nil {
			t.Fatal("negative thickness accepted")
		}
	}
}

func alphaArea(p image.Image) uint64 {
	var area uint64
	for y := p.Bounds().Min.Y; y < p.Bounds().Max.Y; y++ {
		for x := p.Bounds().Min.X; x < p.Bounds().Max.X; x++ {
			_, _, _, a := p.At(x, y).RGBA()
			area += uint64(a)
		}
	}
	return area
}
