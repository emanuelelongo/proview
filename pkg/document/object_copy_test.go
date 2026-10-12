package document

import (
	"bytes"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/signatures"
	"image/color"
	"reflect"
	"testing"
)

func TestObjectCopyRoundTripRetainsKindsAndIndependentAssets(t *testing.T) {
	for _, kind := range []annotations.Kind{annotations.Text, annotations.Drawing, annotations.Highlight, annotations.Shape} {
		t.Run(string(kind), func(t *testing.T) {
			source, _ := testImageSource(t)
			d, _ := New(source)
			data := annotations.Default(kind)
			data.Text = "Sample"
			data.Points = []annotations.Point{{X: 1, Y: 1}, {X: 10, Y: 8}}
			data.PageWidth = 100
			data.PageHeight = 100
			prepared := prepareAnnotation(t, data)
			o, e := d.AddAnnotation(0, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, data, prepared)
			if e != nil {
				t.Fatal(e)
			}
			d.TransformOverlay(o.ID, "horizontal")
			copied, e := d.CopyObject(o.ID)
			if e != nil {
				t.Fatal(e)
			}
			encoded, e := copied.Encode()
			if e != nil {
				t.Fatal(e)
			}
			copied, e = DecodeCopiedObject(encoded)
			if e != nil {
				t.Fatal(e)
			}
			p, e := copied.Asset.Prepare()
			if e != nil {
				t.Fatal(e)
			}
			r := d.Overlays()[0].Rect
			r.X += .1
			clone, e := d.PasteObject(0, r, copied, p)
			if e != nil {
				t.Fatal(e)
			}
			got, _ := clone.Annotation()
			if !reflect.DeepEqual(got, data) || clone.ID == o.ID || clone.Transform != d.Overlays()[0].Transform {
				t.Fatal("copied annotation lost data")
			}
			got.Points[0].X = 90
			original, _ := d.Overlays()[0].Annotation()
			if original.Points[0].X != 1 {
				t.Fatal("copy shares geometry")
			}
			if !d.Undo() || len(d.Overlays()) != 1 || !d.Redo() || len(d.Overlays()) != 2 {
				t.Fatal("paste history broken")
			}
		})
	}
	source, _ := testImageSource(t)
	d, _ := New(source)
	a, e := signatures.FromDrawing(signatures.Drawing{Version: 1, Width: 3, Smooth: 20, Color: color.NRGBA{R: 200, A: 255}, Strokes: [][]signatures.Point{{{X: 100, Y: 100}, {X: 200, Y: 150}}}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := a.Prepare()
	if e != nil {
		t.Fatal(e)
	}
	o, e := d.AddSignatureAsset(0, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, p)
	if e != nil {
		t.Fatal(e)
	}
	copied, _ := d.CopyObject(o.ID)
	encoded, _ := copied.Encode()
	copied, e = DecodeCopiedObject(encoded)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(copied.Asset.Data, a.Data) || !reflect.DeepEqual(copied.Asset.Drawing, a.Drawing) {
		t.Fatal("signature original strokes or smoothing lost")
	}
}
func TestObjectCopyDoesNotExposeRedactedText(t *testing.T) {
	source, e := Load("copy.pdf", bytes.NewReader(textPDF(t, "Source")))
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	d, _ := New(source)
	data := annotations.Default(annotations.Text)
	data.Text = "Private"
	o, e := d.AddAnnotation(0, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, data, prepareAnnotation(t, data))
	if e != nil {
		t.Fatal(e)
	}
	q := TextQuad{UL: TextPoint{X: .1, Y: .1}, UR: TextPoint{X: .3, Y: .1}, LL: TextPoint{X: .1, Y: .3}, LR: TextPoint{X: .3, Y: .3}}
	d.AddRedaction(0, []TextQuad{q})
	if !d.ObjectIntersectsRedaction(o.ID) {
		t.Fatal("intersection missed")
	}
	if _, e = d.CopyObject(o.ID); e == nil {
		t.Fatal("copied raw redacted text")
	}
}
