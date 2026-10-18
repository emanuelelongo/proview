package document

import (
	"context"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/signatures"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"math"
	"testing"
)

func TestPreviewStyleClosedArrowAndMissingAppearance(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "appearance", true: "missing appearance"}[missing], func(t *testing.T) {
			doc := editablePDF(t, 0)
			ctx, err := readPDFObjects(exportEditable(t, doc))
			if err != nil {
				t.Fatal(err)
			}
			page, _, _, err := ctx.PageDict(context.Background(), 1, false)
			if err != nil {
				t.Fatal(err)
			}
			list, _ := ctx.DereferenceArray(page["Annots"])
			a, _ := ctx.DereferenceDict(list[6])
			a["LE"] = types.Array{types.Name("None"), types.Name("ClosedArrow")}
			if missing {
				delete(a, "AP")
			}
			raw, err := writePDFContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			reopened := reopenEditable(t, raw)
			if len(reopened.Overlays()) != 9 {
				t.Fatal("closed arrow not imported")
			}
			o := reopened.Overlays()[6]
			data, ok := o.Annotation()
			if !ok || data.Shape != annotations.Arrow || !data.ArrowClosed {
				t.Fatal("closed arrow lost its ending")
			}
			data.Color.R = 100
			if err = reopened.ReplaceAnnotation(o.ID, o.Rect, data, prepareAnnotation(t, data)); err != nil {
				t.Fatal(err)
			}
			final := reopenEditable(t, exportEditable(t, reopened))
			data, _ = final.Overlays()[6].Annotation()
			if !data.ArrowClosed || data.Color.R != 100 {
				t.Fatal("closed arrow not editable after repeated save")
			}
		})
	}
}

func strokeAppearance(t *testing.T, content string) []byte {
	t.Helper()
	ctx, err := newAppearanceContext(100, 80)
	if err != nil {
		t.Fatal(err)
	}
	page, _, _, err := ctx.PageDict(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := addPDFStream(ctx, []byte(content), nil)
	if err != nil {
		t.Fatal(err)
	}
	page["Contents"] = *ref
	raw, err := writePDFContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestStandardStampStrokeRecognitionIsConservative(t *testing.T) {
	g := pdfGeometry{w: 300, h: 200}
	rect := NormalizedRect{X: .1, Y: .1, Width: .5, Height: .5}
	for _, tc := range []struct {
		name, content string
		want          bool
	}{
		{"stroke", "q 0 0 100 80 re W n 2 w 1 0 0 RG 10 10 m 20 30 50 20 90 70 c S Q", true},
		{"filled artwork", "q 1 0 0 rg 10 10 m 90 10 l 90 70 l h f Q", false},
		{"text", "BT /F0 12 Tf (Signature) Tj ET", false},
		{"multiple paths", "10 10 m 90 70 l S 20 20 m 70 20 l S", false},
		{"cropped", "q 0 0 30 30 re W n 10 10 m 90 70 l S Q", false},
		{"opacity", "q /GS0 gs 10 10 m 90 70 l S Q", false},
		{"nonuniform transform", "q 2 0 0 1 0 0 cm 10 10 m 40 60 l S Q", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := simplePDFStroke(strokeAppearance(t, tc.content), g, rect)
			if ok != tc.want {
				t.Fatalf("recognized=%t", ok)
			}
			if ok && (d.Kind != annotations.Drawing || len(d.Points) < 3 || math.Abs(d.Width-3) > .001 || d.Color.R != 255 || d.Color.G != 0) {
				t.Fatal("stroke geometry/style changed")
			}
		})
	}
}

func TestStampStrokeBecomesEditableInkAndSignatureStaysLocked(t *testing.T) {
	doc := editablePDF(t, 0)
	ctx, err := readPDFObjects(exportEditable(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	page, _, attrs, err := ctx.PageDict(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	geom := geometry(attrs)
	list, _ := ctx.DereferenceArray(page["Annots"])
	raw := strokeAppearance(t, "q 4 w 1 0 0 RG 10 10 m 20 30 50 20 90 70 c S Q")
	form, _, _, err := formFromPDF(ctx, raw, doc.Overlays()[0].Transform)
	if err != nil {
		t.Fatal(err)
	}
	stamp := types.Dict{"Type": types.Name("Annot"), "Subtype": types.Name("Stamp"), "Rect": pdfArray(100, 200, 200, 280), "F": types.Integer(196), "AP": types.Dict{"N": *form}}
	// Put the rect inside this fixture's offset CropBox.
	w, h := geom.size()
	a, b := geom.user(pdfPoint{w * .1, h * .4}), geom.user(pdfPoint{w*.1 + 100, h*.4 + 80})
	stamp["Rect"] = pdfArray(min(a.x, b.x), min(a.y, b.y), max(a.x, b.x), max(a.y, b.y))
	ref, err := ctx.IndRefForNewObject(stamp)
	if err != nil {
		t.Fatal(err)
	}
	page["Annots"] = append(list, *ref)
	saved, err := writePDFContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reopened := reopenEditable(t, saved)
	if len(reopened.Overlays()) != 10 {
		t.Fatal("stamp missing")
	}
	o := reopened.Overlays()[9]
	d, ok := o.Annotation()
	if !ok || d.Kind != annotations.Drawing {
		t.Fatal("stroke remains locked stamp")
	}
	d.Width = 8
	d.Color.R = 40
	if err = reopened.ReplaceAnnotation(o.ID, o.Rect, d, prepareAnnotation(t, d)); err != nil {
		t.Fatal(err)
	}
	again := reopenEditable(t, exportEditable(t, reopened))
	d, ok = again.Overlays()[9].Annotation()
	if !ok || d.Width != 8 || d.Color.R != 40 {
		t.Fatal("stroke style changes lost")
	}
}

func TestStrokedSignatureDoesNotBecomeDrawing(t *testing.T) {
	doc := editablePDF(t, 0)
	asset := signatures.Asset{Kind: signatures.SVG, Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 80"><path d="M10 10 C20 30 50 20 90 70" fill="none" stroke="black" stroke-width="3"/></svg>`)}
	prepared, err := asset.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = doc.AddSignatureAsset(0, NormalizedRect{X: .1, Y: .4, Width: .4, Height: .2}, prepared); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		doc = reopenEditable(t, exportEditable(t, doc))
		last := doc.Overlays()[9]
		if _, ok := last.Annotation(); ok || last.ObjectKind != "pdf-stamp" {
			t.Fatal("signature imported as editable stroke")
		}
	}
}
