package document

import (
	"bytes"
	"context"
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
	"image/color"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestSVGExportRemainsVectorAndPreservesTransparency(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(fmt.Sprint(rotation), func(t *testing.T) {
			source, err := Load("base.pdf", bytes.NewReader(boxedPDF(t, "[0 0 300 400]", "[30 40 250 340]", rotation)))
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			doc, _ := New(source)
			asset, err := signatures.Import("test.svg", []byte(`<svg viewBox="0 0 100 50"><rect width="100" height="50" fill="#ff0000" opacity="0.5"/></svg>`))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := asset.Prepare()
			if err != nil {
				t.Fatal(err)
			}
			size, _ := source.PageSize(0)
			rect := NormalizedRect{X: .2, Y: .3, Width: .4, Height: .4 * float64(size.X) / float64(size.Y) / 2}
			if _, err := doc.AddSignatureAsset(0, rect, prepared); err != nil {
				t.Fatal(err)
			}
			output, err := doc.Export(ExportOptions{Format: FormatPDF})
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := api.ReadContext(context.Background(), bytes.NewReader(output), nil)
			if err != nil {
				t.Fatal(err)
			}
			forms, images := 0, 0
			for _, entry := range ctx.Table {
				if entry == nil {
					continue
				}
				var dict types.Dict
				switch object := entry.Object.(type) {
				case types.Dict:
					dict = object
				case types.StreamDict:
					dict = object.Dict
				}
				if subtype := dict.NameEntry("Subtype"); subtype != nil {
					if *subtype == "Form" {
						forms++
					}
					if *subtype == "Image" {
						images++
					}
				}
			}
			if forms == 0 || images != 0 {
				t.Fatalf("exported PDF has %d forms and %d images; expected vector forms only", forms, images)
			}
			exported, err := Load("signed.pdf", bytes.NewReader(output))
			if err != nil {
				t.Fatal(err)
			}
			defer exported.Close()
			picture, err := exported.Preview(0)
			if err != nil {
				t.Fatal(err)
			}
			bounds := picture.Bounds()
			x, y := int((rect.X+rect.Width/2)*float64(bounds.Dx())), int((rect.Y+rect.Height/2)*float64(bounds.Dy()))
			pixel := color.NRGBAModel.Convert(picture.At(x, y)).(color.NRGBA)
			if pixel.R < 240 || pixel.G < 110 || pixel.G > 145 || pixel.B < 110 || pixel.B > 145 {
				t.Fatalf("signature center at %d,%d = %#v; want translucent red", x, y, pixel)
			}
		})
	}
}

func TestDrawnSignatureCopiesAndSettingsUndo(t *testing.T) {
	source, err := Load("base.pdf", bytes.NewReader(minimalPDF(t, 1)))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	doc, _ := New(source)
	asset, err := signatures.FromDrawing(signatures.Drawing{Version: 1, Width: 4, Smooth: 25, Strokes: [][]signatures.Point{{{X: 100, Y: 100}, {X: 150, Y: 140}, {X: 200, Y: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := asset.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	rect := NormalizedRect{X: .1, Y: .1, Width: .4, Height: .2}
	first, err := doc.AddSignatureAsset(0, rect, prepared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := doc.AddSignatureAsset(0, rect, prepared)
	if err != nil {
		t.Fatal(err)
	}
	doc.MarkSaved()
	updated, _ := asset.WithSettings(80, 6)
	newPrepared, err := updated.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.ReplaceSignatureAsset(first.ID, newPrepared); err != nil {
		t.Fatal(err)
	}
	copies := doc.Signatures()
	if copies[0].Asset().Drawing.Smooth != 80 || copies[1].Asset().Drawing.Smooth != 25 || copies[1].ID != second.ID {
		t.Fatal("changing a copy affected another signature")
	}
	if !doc.Dirty() || !doc.Undo() || doc.Dirty() {
		t.Fatal("settings did not form a single reversible edit")
	}
	if !doc.Redo() || doc.Signatures()[0].Asset().Drawing.Smooth != 80 {
		t.Fatal("redo did not restore settings")
	}
	snapshot := doc.Signatures()[0].Asset()
	snapshot.Drawing.Strokes[0][0].X = 999
	if doc.Signatures()[0].Asset().Drawing.Strokes[0][0].X == 999 {
		t.Fatal("signature source was not independent")
	}
}
