package document

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
)

func TestShapeResizeReplacesArtworkAndPreservesTransformsAndUndo(t *testing.T) {
	for _, kind := range []annotations.ShapeKind{annotations.Rectangle, annotations.Oval, annotations.Callout, annotations.Line, annotations.Arrow, annotations.Star, annotations.Magnifier} {
		for _, turns := range []int{0, 1, 2, 3} {
			t.Run(fmt.Sprintf("%s/%d", kind, turns), func(t *testing.T) {
				source, err := Load("base.pdf", bytes.NewReader(minimalPDF(t, 1)))
				if err != nil {
					t.Fatal(err)
				}
				defer source.Close()
				doc, _ := New(source)
				data := annotations.Default(annotations.Shape)
				data.Shape = kind
				data.Points = []annotations.Point{{X: 20, Y: 20}, {X: 80, Y: 60}}
				if kind == annotations.Magnifier {
					data.Points[1].Y = 80
				}
				o, err := doc.AddAnnotation(0, NormalizedRect{X: .2, Y: .2, Width: .2, Height: .15}, data, prepareAnnotation(t, data))
				if err != nil {
					t.Fatal(err)
				}
				for range turns {
					if err := doc.TransformOverlay(o.ID, "right"); err != nil {
						t.Fatal(err)
					}
				}
				if err := doc.TransformOverlay(o.ID, "horizontal"); err != nil {
					t.Fatal(err)
				}
				original := doc.Overlays()[0]
				doc.MarkSaved()
				next := original.Rect
				next.Width *= 1.4
				next.Height *= .8
				if kind == annotations.Magnifier {
					next.Height = original.Rect.Height * 1.4
				}
				if err := doc.BeginOverlayEdit(o.ID); err != nil {
					t.Fatal(err)
				}
				if err := doc.UpdateOverlay(o.ID, 0, next); err != nil {
					t.Fatal(err)
				}
				if err := doc.CommitOverlayEdit(o.ID); err != nil {
					t.Fatal(err)
				}
				resized := doc.Overlays()[0]
				newData, _ := resized.Annotation()
				if bytes.Equal(resized.Asset().Data, original.Asset().Data) || resized.Transform != original.Transform || newData.Width != data.Width {
					t.Fatal("resize stretched existing artwork or changed stroke/transform")
				}
				_, oldBounds, _ := data.Artwork()
				_, newBounds, _ := newData.Artwork()
				sx, sy := next.Width/original.Rect.Width, next.Height/original.Rect.Height
				if turns%2 != 0 {
					sx, sy = sy, sx
				}
				if math.Abs(newBounds.W()-oldBounds.W()*sx) > .001 || math.Abs(newBounds.H()-oldBounds.H()*sy) > .001 {
					t.Fatal("artwork has the wrong dimensions after transformed resize")
				}
				if !doc.Undo() || doc.Dirty() || !bytes.Equal(doc.Overlays()[0].Asset().Data, original.Asset().Data) || doc.Overlays()[0].Rect != original.Rect {
					t.Fatal("resize did not restore all original geometry in one Undo")
				}
				if !doc.Redo() || !bytes.Equal(doc.Overlays()[0].Asset().Data, resized.Asset().Data) {
					t.Fatal("Redo lost regenerated geometry")
				}
				// Non-transactional callers must regenerate height-only changes too.
				next = resized.Rect
				next.Height *= .9
				if kind != annotations.Magnifier {
					if err := doc.UpdateOverlay(o.ID, 0, next); err != nil {
						t.Fatal(err)
					}
					if bytes.Equal(doc.Overlays()[0].Asset().Data, resized.Asset().Data) {
						t.Fatal("height-only resize retained the old artwork")
					}
				}
			})
		}
	}
}
