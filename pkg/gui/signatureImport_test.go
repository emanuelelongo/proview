package gui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"

	"emanuelelongo/proview/pkg/signatures"
)

func TestSignatureImportRetainsSVGAndRoutesRasterToVectorization(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><path d="M1 1L19 9" stroke="black"/></svg>`)
	asset, scan, err := signatureImport("signature.SVG", svg)
	if err != nil || asset.Kind != signatures.SVG || scan.Image != nil || !bytes.Equal(asset.Data, svg) {
		t.Fatalf("SVG import did not preserve the asset: %v", err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(3, 3, 15, 6), image.NewUniform(color.Black), image.Point{}, draw.Src)
	for _, format := range []string{"png", "jpeg"} {
		var encoded bytes.Buffer
		if format == "png" {
			err = png.Encode(&encoded, img)
		} else {
			err = jpeg.Encode(&encoded, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		asset, scan, err = signatureImport("signature."+format, encoded.Bytes())
		if err != nil || scan.Image == nil || asset.Kind != "" {
			t.Fatalf("%s was not routed to vectorization: %v", format, err)
		}
	}
	if _, _, err := signatureImport("signature.jpeg", []byte("broken")); err == nil {
		t.Fatal("invalid raster was accepted")
	}
}
