package document

import (
	"bytes"
	"context"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func prepareAnnotation(t *testing.T, data annotations.Data) signatures.Prepared {
	t.Helper()
	asset, _, err := data.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	p, err := asset.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestAnnotationPropertiesResizeAndUndo(t *testing.T) {
	source, err := Load("base.pdf", bytes.NewReader(minimalPDF(t, 1)))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	doc, _ := New(source)
	d := annotations.Default(annotations.Drawing)
	d.PageWidth = 200
	d.PageHeight = 200
	d.Points = []annotations.Point{{X: 10, Y: 10}, {X: 70, Y: 70}}
	p := prepareAnnotation(t, d)
	r := NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}
	o, err := doc.AddAnnotation(0, r, d, p)
	if err != nil {
		t.Fatal(err)
	}
	d.Points[0].X = 99
	snap := doc.Overlays()[0]
	raw, _ := snap.Annotation()
	if raw.Points[0].X != 10 {
		t.Fatal("raw points shared with caller")
	}
	doc.MarkSaved()
	r.Width *= 2
	r.Height *= 2
	if err := doc.BeginOverlayEdit(o.ID); err != nil {
		t.Fatal(err)
	}
	if err := doc.UpdateOverlay(o.ID, 0, r); err != nil {
		t.Fatal(err)
	}
	if err := doc.CommitOverlayEdit(o.ID); err != nil {
		t.Fatal(err)
	}
	raw, _ = doc.Overlays()[0].Annotation()
	if raw.Width != 4 {
		t.Fatal("resize did not update thickness")
	}
	if !doc.Undo() || doc.Dirty() {
		t.Fatal("gesture was not a single reversible edit")
	}
	if !doc.Redo() {
		t.Fatal("redo failed")
	}
	raw.Color = color.NRGBA{R: 255, A: 255}
	raw.Width = 6
	if err := doc.ReplaceAnnotation(o.ID, r, raw, prepareAnnotation(t, raw)); err != nil {
		t.Fatal(err)
	}
	if !doc.Undo() {
		t.Fatal("property undo failed")
	}
	raw, _ = doc.Overlays()[0].Annotation()
	if raw.Width != 4 {
		t.Fatal("property undo did not preserve resize")
	}
	if err := doc.RemoveOverlay(o.ID); err != nil {
		t.Fatal(err)
	}
	if len(doc.Overlays()) != 0 || !doc.Undo() {
		t.Fatal("delete was not reversible")
	}
}
func TestAnnotationPDFVectorExportAndLayerOrder(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(fmt.Sprint(rotation), func(t *testing.T) {
			source, err := Load("base.pdf", bytes.NewReader(boxedPDF(t, "[0 0 300 400]", "[30 40 250 340]", rotation)))
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			doc, _ := New(source)
			size, _ := doc.PageSize(0)
			pw, ph := float64(size.X)/2, float64(size.Y)/2
			line := annotations.Default(annotations.Highlight)
			line.PageWidth = pw
			line.PageHeight = ph
			line.Points = []annotations.Point{{X: pw * .2, Y: ph * .5}, {X: pw * .7, Y: ph * .5}}
			line.Quads = []annotations.Quad{{
				{X: pw * .2, Y: ph*.5 - 6}, {X: pw * .7, Y: ph*.5 - 6},
				{X: pw * .7, Y: ph*.5 + 6}, {X: pw * .2, Y: ph*.5 + 6},
			}}
			line.Color = color.NRGBA{R: 255, A: 255}
			line.Opacity = .5
			a, b, err := line.Artwork()
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := a.Prepare()
			if err != nil {
				t.Fatal(err)
			}
			rect := NormalizedRect{X: b.X0 / pw, Y: b.Y0 / ph, Width: b.W() / pw, Height: b.H() / ph}
			if _, err := doc.AddAnnotation(0, rect, line, prepared); err != nil {
				t.Fatal(err)
			}
			text := annotations.Default(annotations.Text)
			text.Text = "Sample text"
			p := prepareAnnotation(t, text)
			if _, err := doc.AddAnnotation(0, NormalizedRect{X: .1, Y: .1, Width: .3, Height: .1}, text, p); err != nil {
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
			forms := 0
			for _, e := range ctx.Table {
				if e == nil {
					continue
				}
				var d types.Dict
				switch v := e.Object.(type) {
				case types.Dict:
					d = v
				case types.StreamDict:
					d = v.Dict
				}
				if subtype := d.NameEntry("Subtype"); subtype != nil {
					if *subtype == "Image" {
						t.Fatal("annotation rasterized in PDF")
					}
					if *subtype == "Form" {
						forms++
					}
				}
			}
			if forms < 2 {
				t.Fatal("missing vector forms")
			}
			out, err := Load("out.pdf", bytes.NewReader(output))
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			picture, err := out.Preview(0)
			if err != nil {
				t.Fatal(err)
			}
			c := color.NRGBAModel.Convert(picture.At(picture.Bounds().Dx()/2, picture.Bounds().Dy()/2)).(color.NRGBA)
			if c.R < 240 || c.G < 120 || c.G > 135 || c.B < 120 || c.B > 135 {
				t.Fatalf("highlight placement/transparency = %v", c)
			}
			// A later translucent flat-nib stroke must blend above the highlight,
			// in both preview and vector PDF export.
			line.Kind = annotations.Drawing
			line.Width = 12
			line.Opacity = .5
			line.Color = color.NRGBA{B: 255, A: 255}
			a, b, err = line.Artwork()
			if err != nil {
				t.Fatal(err)
			}
			prepared, err = a.Prepare()
			if err != nil {
				t.Fatal(err)
			}
			rect = NormalizedRect{X: b.X0 / pw, Y: b.Y0 / ph, Width: b.W() / pw, Height: b.H() / ph}
			doc.AddAnnotation(0, rect, line, prepared)
			picture, err = doc.RenderPage(0)
			if err != nil {
				t.Fatal(err)
			}
			c = color.NRGBAModel.Convert(picture.At(picture.Bounds().Dx()/2, picture.Bounds().Dy()/2)).(color.NRGBA)
			if c.R < 120 || c.R > 135 || c.G < 58 || c.G > 70 || c.B < 185 || c.B > 200 {
				t.Fatalf("layer order changed in preview: %v", c)
			}
			output, err = doc.Export(ExportOptions{Format: FormatPDF})
			if err != nil {
				t.Fatal(err)
			}
			stacked, err := Load("stacked.pdf", bytes.NewReader(output))
			if err != nil {
				t.Fatal(err)
			}
			defer stacked.Close()
			picture, err = stacked.Preview(0)
			if err != nil {
				t.Fatal(err)
			}
			c = color.NRGBAModel.Convert(picture.At(picture.Bounds().Dx()/2, picture.Bounds().Dy()/2)).(color.NRGBA)
			if c.R < 120 || c.R > 135 || c.G < 58 || c.G > 70 || c.B < 185 || c.B > 200 {
				t.Fatalf("layer order changed in PDF: %v", c)
			}
		})
	}
}

func TestAnnotationImageExportCropAndRenderingSnapshot(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	data := annotations.Default(annotations.Drawing)
	data.Color = color.NRGBA{R: 255, A: 255}
	data.PageWidth = 100
	data.PageHeight = 100
	data.Points = []annotations.Point{{X: 20, Y: 50}, {X: 80, Y: 50}}
	_, bounds, err := data.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	rect := NormalizedRect{X: bounds.X0 / 100, Y: bounds.Y0 / 100, Width: bounds.W() / 100, Height: bounds.H() / 100}
	overlay, err := doc.AddAnnotation(0, rect, data, prepareAnnotation(t, data))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := doc.RenderingSnapshot()
	if err := doc.RemoveOverlay(overlay.ID); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Overlays()) != 1 || len(doc.Overlays()) != 0 {
		t.Fatal("rendering snapshot shared mutable objects")
	}
	doc.Undo()
	doc.MarkSaved()
	if err := doc.ApplyCrop(image.Rect(10, 20, 90, 80), image.Rect(0, 0, 100, 100)); err != nil {
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPNG})
	if err != nil {
		t.Fatal(err)
	}
	picture, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if picture.Bounds().Size() != image.Pt(80, 60) || !containsRed(picture) {
		t.Fatal("cropped image lost annotation or dimensions")
	}
	if !doc.Undo() || doc.Dirty() {
		t.Fatal("crop did not undo independently")
	}
}
