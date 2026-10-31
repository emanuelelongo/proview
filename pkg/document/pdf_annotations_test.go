package document

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gen2brain/go-fitz"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/signatures"
)

func editablePDF(t *testing.T, rotation int) *Document {
	t.Helper()
	source, e := Load("editable.pdf", bytes.NewReader(boxedPDF(t, "[0 0 300 400]", "[30 40 250 340]", rotation)))
	if e != nil {
		t.Fatal(e)
	}
	d, e := New(source)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(d.Close)
	size, _ := d.PageSize(0)
	w, h := float64(size.X)/2, float64(size.Y)/2
	data := annotations.Default(annotations.Text)
	data.Text = "Caffè, perché €"
	data.Bold = true
	data.Italic = true
	data.Underline = true
	if _, e = d.AddAnnotation(0, NormalizedRect{.1, .05, .6, .08}, data, prepareAnnotation(t, data)); e != nil {
		t.Fatal(e)
	}
	data = annotations.Default(annotations.Drawing)
	data.PageWidth = w
	data.PageHeight = h
	data.Points = []annotations.Point{{X: w * .1, Y: h * .25}, {X: w * .2, Y: h * .3}, {X: w * .4, Y: h * .2}}
	data.Width = 4
	data.Opacity = .5
	addAnnotationGeometry(t, d, data)
	data = annotations.Default(annotations.Highlight)
	data.PageWidth = w
	data.PageHeight = h
	data.Quads = []annotations.Quad{{{X: w * .1, Y: h * .35}, {X: w * .6, Y: h * .35}, {X: w * .6, Y: h * .4}, {X: w * .1, Y: h * .4}}}
	addAnnotationGeometry(t, d, data)
	for i, kind := range []annotations.ShapeKind{annotations.Rectangle, annotations.Oval, annotations.Line, annotations.Arrow, annotations.Star, annotations.Callout} {
		data = annotations.Default(annotations.Shape)
		data.Shape = kind
		data.PageWidth = w
		data.PageHeight = h
		x, y := w*(.1+float64(i%3)*.28), h*(.5+float64(i/3)*.23)
		data.Points = []annotations.Point{{X: x, Y: y}, {X: x + w*.16, Y: y + h*.12}}
		data.Filled = kind == annotations.Star
		addAnnotationGeometry(t, d, data)
	}
	return d
}
func addAnnotationGeometry(t *testing.T, d *Document, data annotations.Data) {
	t.Helper()
	asset, b, e := data.Artwork()
	if e != nil {
		t.Fatal(e)
	}
	p, e := asset.Prepare()
	if e != nil {
		t.Fatal(e)
	}
	size, _ := d.PageSize(0)
	w, h := float64(size.X)/2, float64(size.Y)/2
	if _, e = d.AddAnnotation(0, NormalizedRect{b.X0 / w, b.Y0 / h, b.W() / w, b.H() / h}, data, p); e != nil {
		t.Fatal(e)
	}
}
func reopenEditable(t *testing.T, data []byte) *Document {
	t.Helper()
	s, e := Load("reopened.pdf", bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	d, e := New(s)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(d.Close)
	return d
}
func exportEditable(t *testing.T, d *Document) []byte {
	t.Helper()
	data, e := d.Export(ExportOptions{Format: FormatPDF})
	if e != nil {
		t.Fatal(e)
	}
	return data
}

func TestStandardAnnotationsRoundTripModifyDeleteAndUndo(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(string(rune('a'+rotation/90)), func(t *testing.T) {
			original := editablePDF(t, rotation)
			data := exportEditable(t, original)
			ctx, e := readPDFObjects(data)
			if e != nil {
				t.Fatal(e)
			}
			page, _, _, e := ctx.PageDict(context.Background(), 1, false)
			if e != nil {
				t.Fatal(e)
			}
			list, e := ctx.DereferenceArray(page["Annots"])
			if e != nil {
				t.Fatal(e)
			}
			if len(list) != 9 {
				t.Fatalf("annotations=%d, want 9", len(list))
			}
			typesSeen := map[string]int{}
			for _, ref := range list {
				a, e := ctx.DereferenceDict(ref)
				if e != nil {
					t.Fatal(e)
				}
				typesSeen[annotationName(a, "Subtype")]++
				for key := range a {
					if strings.HasPrefix(key, "Proview") {
						t.Fatal("private editor data in PDF")
					}
				}
			}
			if typesSeen["FreeText"] != 1 || typesSeen["Ink"] != 1 || typesSeen["Highlight"] != 1 || typesSeen["Polygon"] != 2 {
				t.Fatalf("types=%v", typesSeen)
			}
			d := reopenEditable(t, data)
			if len(d.Overlays()) != 9 {
				t.Fatalf("reopened objects=%d, want 9", len(d.Overlays()))
			}
			if d.Dirty() || d.Undo() {
				t.Fatal("opening created an edit")
			}
			a, b := d.Overlays()[0].Annotation()
			if !b || a.Text != "Caffè, perché €" || !a.Bold || !a.Italic || !a.Underline {
				t.Fatalf("text metadata lost: %+v", a)
			}
			before, e := original.RenderPage(0)
			if e != nil {
				t.Fatal(e)
			}
			after, e := d.RenderPage(0)
			if e != nil {
				t.Fatal(e)
			}
			var total int64
			for y := 0; y < before.Bounds().Dy(); y++ {
				for x := 0; x < before.Bounds().Dx(); x++ {
					p := color.NRGBAModel.Convert(before.At(x, y)).(color.NRGBA)
					q := color.NRGBAModel.Convert(after.At(x, y)).(color.NRGBA)
					total += int64(absInt(int(p.R)-int(q.R)) + absInt(int(p.G)-int(q.G)) + absInt(int(p.B)-int(q.B)))
				}
			}
			if float64(total)/float64(before.Bounds().Dx()*before.Bounds().Dy()*3) > 3 {
				t.Fatalf("reopen changed appearance: average difference %.3f", float64(total)/float64(before.Bounds().Dx()*before.Bounds().Dy()*3))
			}
			o := d.Overlays()[0]
			moved := o.Rect
			moved.Y += .05
			if e := d.UpdateOverlay(o.ID, 0, moved); e != nil {
				t.Fatal(e)
			}
			a.Text = "Edited after reopen"
			a.Color = color.NRGBA{R: 200, A: 255}
			p := prepareAnnotation(t, a)
			if e := d.ReplaceAnnotation(o.ID, moved, a, p); e != nil {
				t.Fatal(e)
			}
			d2 := reopenEditable(t, exportEditable(t, d))
			if len(d2.Overlays()) != 9 {
				t.Fatal("save duplicated or lost annotations")
			}
			a, _ = d2.Overlays()[0].Annotation()
			if a.Text != "Edited after reopen" || a.Color.R != 200 {
				t.Fatalf("edit lost: %+v", a)
			}
			for _, o := range d2.Overlays() {
				if e := d2.RemoveOverlay(o.ID); e != nil {
					t.Fatal(e)
				}
			}
			if len(reopenEditable(t, exportEditable(t, d2)).Overlays()) != 0 {
				t.Fatal("deleted annotation survived in source")
			}
			if !d2.Undo() || len(d2.Overlays()) != 1 {
				t.Fatal("delete undo failed")
			}
		})
	}
}

func TestStandardAnnotationsRepeatedSaveHasStableSize(t *testing.T) {
	data := exportEditable(t, editablePDF(t, 0))
	initial := len(data)
	for i := 0; i < 12; i++ {
		d := reopenEditable(t, data)
		data = exportEditable(t, d)
		if len(d.Overlays()) != 9 {
			t.Fatal("object count changed")
		}
	}
	if len(data) > initial*2 {
		t.Fatalf("appearance resources accumulated: %d -> %d", initial, len(data))
	}
}
func TestImportedAnnotationsSurvivePageOperations(t *testing.T) {
	data := exportEditable(t, editablePDF(t, 0))
	d := reopenEditable(t, data)
	source, e := Load("append.pdf", bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	if e = d.AppendPDFs(source); e != nil {
		t.Fatal(e)
	}
	if e = d.DuplicatePage(0); e != nil {
		t.Fatal(e)
	}
	if e = d.TransformPDFPage(1, "right"); e != nil {
		t.Fatal(e)
	}
	out := reopenEditable(t, exportEditable(t, d))
	if out.PageCount() != 3 || len(out.Overlays()) != 27 {
		t.Fatalf("pages=%d, objects=%d", out.PageCount(), len(out.Overlays()))
	}
	for page := range out.PageCount() {
		objects, _ := out.OverlaysForPage(page)
		if len(objects) != 9 {
			t.Fatalf("page %d: objects=%d", page, len(objects))
		}
		before, err := d.RenderPage(page)
		if err != nil {
			t.Fatal(err)
		}
		after, err := out.RenderPage(page)
		if err != nil {
			t.Fatal(err)
		}
		assertAnnotationPixels(t, before, after)
	}
}
func TestNativeTextEditingAndRedactionWithImportedAnnotations(t *testing.T) {
	d := redactionDocument(t)
	a := annotations.Default(annotations.Text)
	a.Text = "Annotation secret"
	_, e := d.AddAnnotation(0, NormalizedRect{.1, .7, .6, .08}, a, prepareAnnotation(t, a))
	if e != nil {
		t.Fatal(e)
	}
	d = reopenEditable(t, exportEditable(t, d))
	start, n := correctionRange(t, d, 0, "SECRET")
	prepared, e := d.RenderingSnapshot().PrepareTextCorrection(0, start, n, "EDIT", 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Close()

	if e = d.ApplyTextCorrection(prepared); e != nil {
		t.Fatal(e)
	}
	d = reopenEditable(t, exportEditable(t, d))
	if len(d.Overlays()) != 1 || !strings.Contains(correctedPageText(t, d, 0), "EDIT") {
		t.Fatalf("native edit lost annotations or text: objects=%d text=%q", len(d.Overlays()), correctedPageText(t, d, 0))
	}
	if _, e = d.AddRedaction(0, []TextQuad{{UL: TextPoint{X: 0, Y: 0}, UR: TextPoint{X: 1, Y: 0}, LL: TextPoint{X: 0, Y: 1}, LR: TextPoint{X: 1, Y: 1}}}); e != nil {
		t.Fatal(e)
	}
	output := exportEditable(t, d)
	out := reopenEditable(t, output)
	if len(out.Overlays()) != 0 {
		t.Fatal("redaction left editable annotations")
	}
	ctx, e := readPDFObjects(output)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range ctx.Table {
		if entry != nil {
			if dict, ok := entry.Object.(types.Dict); ok {
				if value := annotationString(ctx, dict, "Contents"); strings.Contains(value, "secret") {
					t.Fatal("redaction leaked annotation contents")
				}
			}
		}
	}
}

// Set this only for manual checks with PDFKit, Preview or Acrobat. Inputs and
// outputs are synthetic; ordinary unit tests do not write outside t.TempDir.
func TestStandardAnnotationInteroperabilityFixture(t *testing.T) {
	folder := os.Getenv("PROVIEW_ANNOTATION_PROBE_DIR")
	if folder == "" {
		t.Skip("set PROVIEW_ANNOTATION_PROBE_DIR to create review fixtures")
	}
	if e := os.MkdirAll(folder, 0700); e != nil {
		t.Fatal(e)
	}
	for _, rotation := range []int{0, 90, 180, 270} {
		data := exportEditable(t, editablePDF(t, rotation))
		if e := os.WriteFile(filepath.Join(folder, fmt.Sprintf("annotations-%d.pdf", rotation)), data, 0600); e != nil {
			t.Fatal(e)
		}
		if incoming, e := os.ReadFile(filepath.Join(folder, fmt.Sprintf("pdfkit-%d.pdf", rotation))); e == nil {
			doc := reopenEditable(t, incoming)
			if len(doc.Overlays()) != 9 {
				t.Fatalf("PDFKit annotations not editable: %d", len(doc.Overlays()))
			}
			text, ok := doc.Overlays()[0].Annotation()
			if !ok || text.Text != "Changed with PDFKit" || text.Color.R < 245 || text.Color.G > 10 || text.Color.B > 10 || text.Size != 20 {
				t.Fatalf("PDFKit text changes not retained: %+v", text)
			}
			base, e := doc.source.Preview(0)
			if e != nil {
				t.Fatal(e)
			}
			composed, e := doc.RenderPage(0)
			if e != nil {
				t.Fatal(e)
			}
			assertAnnotationPixels(t, base, composed)
			if len(reopenEditable(t, exportEditable(t, doc)).Overlays()) != 9 {
				t.Fatal("PDFKit -> Proview save changed annotation count")
			}
		}

	}
}

func TestAnnotationEmbeddedFontsRenderTrueTypeAndOpenType(t *testing.T) {
	for _, family := range annotations.Fonts {
		for _, style := range []struct{ bold, italic bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
			t.Run(fmt.Sprintf("%s/%t/%t", family, style.bold, style.italic), func(t *testing.T) {
				ctx, e := newAppearanceContext(200, 60)
				if e != nil {
					t.Fatal(e)
				}
				data := annotations.Default(annotations.Text)
				data.Font = family
				data.Bold = style.bold
				data.Italic = style.italic
				font, e := annotationFont(ctx, data)
				if e != nil {
					t.Fatal(e)
				}
				page, _, _, e := ctx.PageDict(context.Background(), 1, false)
				if e != nil {
					t.Fatal(e)
				}
				page["Resources"] = types.Dict{"Font": types.Dict{"F0": *font}}
				content, e := addPDFStream(ctx, []byte("BT /F0 18 Tf 10 30 Td <004300610066006600e80020002020ac> Tj ET"), nil)
				if e != nil {
					t.Fatal(e)
				}
				page["Contents"] = *content
				pdf, e := writePDFContext(ctx)
				if e != nil {
					t.Fatal(e)
				}
				r, e := fitz.NewFromMemory(pdf)
				if e != nil {
					t.Fatal(e)
				}
				defer r.Close()
				text, e := r.Text(0)
				if e != nil || !strings.Contains(text, "Caffè") || !strings.Contains(text, "€") {
					t.Fatalf("font lost Unicode text: %q %v", text, e)
				}
				rendered, e := r.Image(0)
				if e != nil {
					t.Fatal(e)
				}
				ink := 0
				for y := rendered.Bounds().Min.Y; y < rendered.Bounds().Max.Y; y++ {
					for x := rendered.Bounds().Min.X; x < rendered.Bounds().Max.X; x++ {
						r, g, b, _ := rendered.At(x, y).RGBA()
						if r+g+b < 3*30000 {
							ink++
						}
					}
				}
				if ink < 100 {
					t.Fatal("embedded font did not render")
				}
			})
		}
	}
}

func assertAnnotationPixels(t *testing.T, before, after image.Image) {
	t.Helper()
	if before.Bounds().Size() != after.Bounds().Size() {
		t.Fatalf("page dimensions changed: %v -> %v", before.Bounds(), after.Bounds())
	}
	var total int64
	for y := 0; y < before.Bounds().Dy(); y++ {
		for x := 0; x < before.Bounds().Dx(); x++ {
			p := color.NRGBAModel.Convert(before.At(x, y)).(color.NRGBA)
			q := color.NRGBAModel.Convert(after.At(x, y)).(color.NRGBA)
			total += int64(absInt(int(p.R)-int(q.R)) + absInt(int(p.G)-int(q.G)) + absInt(int(p.B)-int(q.B)))
		}
	}
	difference := float64(total) / float64(before.Bounds().Dx()*before.Bounds().Dy()*3)
	if difference > 3 {
		t.Fatalf("page appearance changed: average channel difference %.3f", difference)
	}
}
func TestImportedTextTransformsSurviveEditing(t *testing.T) {
	for _, commands := range [][]string{{"right"}, {"left"}, {"horizontal"}, {"vertical"}, {"right", "horizontal"}, {"right", "vertical"}, {"horizontal", "vertical"}} {
		t.Run(strings.Join(commands, "/"), func(t *testing.T) {
			d := editablePDF(t, 0)
			id := d.Overlays()[0].ID
			for _, command := range commands {
				if e := d.TransformOverlay(id, command); e != nil {
					t.Fatal(e)
				}
			}
			reopened := reopenEditable(t, exportEditable(t, d))
			a, e := d.RenderPage(0)
			if e != nil {
				t.Fatal(e)
			}
			b, e := reopened.RenderPage(0)
			if e != nil {
				t.Fatal(e)
			}
			assertAnnotationPixels(t, a, b)
			for _, doc := range []*Document{d, reopened} {
				o := doc.Overlays()[0]
				text, _ := o.Annotation()
				text.Text = "Modified"
				prepared := prepareAnnotation(t, text)
				if e := doc.ReplaceAnnotation(o.ID, o.Rect, text, prepared); e != nil {
					t.Fatal(e)
				}
			}
			a, e = d.RenderPage(0)
			if e != nil {
				t.Fatal(e)
			}
			b, e = reopened.RenderPage(0)
			if e != nil {
				t.Fatal(e)
			}
			assertAnnotationPixels(t, a, b)
		})
	}
}
func TestImportedAnnotationClipboardStaysVector(t *testing.T) {
	d := reopenEditable(t, exportEditable(t, editablePDF(t, 0)))
	for _, o := range d.Overlays() {
		copied, e := d.CopyObject(o.ID)
		if e != nil {
			t.Fatal(e)
		}
		if copied.Asset.Kind != signatures.SVG {
			t.Fatalf("imported annotation copied as %s", copied.Asset.Kind)
		}
		payload, e := copied.Encode()
		if e != nil {
			t.Fatal(e)
		}
		copied, e = DecodeCopiedObject(payload)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = d.ObjectImage(o.ID); e != nil {
			t.Fatal(e)
		}
		prepared, e := copied.Asset.Prepare()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = d.PasteObject(0, o.Rect, copied, prepared); e != nil {
			t.Fatal(e)
		}
	}
	if len(reopenEditable(t, exportEditable(t, d)).Overlays()) != 18 {
		t.Fatal("copied annotations lost on reopen")
	}
}
func TestSignatureStampRetainsOrderButIsNotEditableAnnotation(t *testing.T) {
	d := editablePDF(t, 0)
	if _, e := d.AddSignature(0, NormalizedRect{.1, .05, .6, .08}, opaqueSignaturePNG(t)); e != nil {
		t.Fatal(e)
	}
	out := reopenEditable(t, exportEditable(t, d))
	objects := out.Overlays()
	if len(objects) != 10 || objects[9].ObjectKind != "pdf-stamp" {
		t.Fatal("signature changed position in object stack")
	}
	if _, ok := objects[9].Annotation(); ok {
		t.Fatal("signature imported as editable markup")
	}
	a, e := d.RenderPage(0)
	if e != nil {
		t.Fatal(e)
	}
	b, e := out.RenderPage(0)
	if e != nil {
		t.Fatal(e)
	}
	assertAnnotationPixels(t, a, b)
	again := reopenEditable(t, exportEditable(t, out))
	if len(again.Overlays()) != 10 {
		t.Fatal("signature stamp duplicated")
	}
}

func TestUnsupportedAnnotationsAndStandardMarkupSurvive(t *testing.T) {
	ctx, err := readPDFObjects(exportEditable(t, editablePDF(t, 0)))
	if err != nil {
		t.Fatal(err)
	}
	page, _, _, err := ctx.PageDict(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	list, err := ctx.DereferenceArray(page["Annots"])
	if err != nil {
		t.Fatal(err)
	}
	first, err := ctx.DereferenceDict(list[0])
	if err != nil {
		t.Fatal(err)
	}
	first["T"] = pdfText("Jane Doe")
	first["Subj"] = pdfText("Review text")
	first["NM"] = pdfText("review-1")
	// A multiline FreeText appearance must remain intact in the page, since the
	// editor supports one line. Existing vectors and text are still renderable.
	unsupported := first.Clone().(types.Dict)
	unsupported["Contents"] = pdfText("Multiple\nlines")
	unsupported["NM"] = pdfText("unsupported-1")
	ref, err := ctx.IndRefForNewObject(unsupported)
	if err != nil {
		t.Fatal(err)
	}
	page["Annots"] = append(list, *ref)
	raw, err := writePDFContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	doc := reopenEditable(t, raw)
	if len(doc.Overlays()) != 9 {
		t.Fatal("unsupported text detached from source")
	}
	o := doc.Overlays()[0]
	data, _ := o.Annotation()
	data.Text = "Revised"
	if err = doc.ReplaceAnnotation(o.ID, o.Rect, data, prepareAnnotation(t, data)); err != nil {
		t.Fatal(err)
	}
	saved, err := readPDFObjects(exportEditable(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	page, _, _, err = saved.PageDict(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	list, err = saved.DereferenceArray(page["Annots"])
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 10 {
		t.Fatalf("annotations=%d", len(list))
	}
	seen := map[string]bool{}
	for _, r := range list {
		a, err := saved.DereferenceDict(r)
		if err != nil {
			t.Fatal(err)
		}
		name := annotationString(saved, a, "NM")
		seen[name] = true
		if name == "review-1" && (annotationString(saved, a, "Contents") != "Revised" || annotationString(saved, a, "T") != "Jane Doe" || annotationString(saved, a, "Subj") != "Review text") {
			t.Fatal("standard markup lost on editing")
		}
		if name == "unsupported-1" && annotationString(saved, a, "Contents") != "Multiple\nlines" {
			t.Fatal("unsupported annotation modified")
		}
	}
	if !seen["review-1"] || !seen["unsupported-1"] {
		t.Fatal("markup identity lost")
	}
}

func TestFormsRemainEditableAlongsideStandardAnnotations(t *testing.T) {
	doc := formDocument(t)
	fields, err := doc.FormFields(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = doc.SetFormValue(0, fields[0].ID, "With annotations"); err != nil {
		t.Fatal(err)
	}
	text := annotations.Default(annotations.Text)
	text.Text = "Review note"
	if _, err = doc.AddAnnotation(0, NormalizedRect{X: .1, Y: .8, Width: .4, Height: .1}, text, prepareAnnotation(t, text)); err != nil {
		t.Fatal(err)
	}
	out := reopenEditable(t, exportEditable(t, doc))
	fields, err = out.FormFields(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields[0].Value != "With annotations" || len(out.Overlays()) != 1 {
		t.Fatal("form fields or annotation lost")
	}
	base, _, err := out.pageSource(0)
	if err != nil {
		t.Fatal(err)
	}
	if base != out.source.annotationBase {
		t.Fatal("render page did not route through the annotation base source")
	}
	if _, err = out.Preview(0); err != nil {
		t.Fatal(err)
	}
	if base.formRenderer == nil {
		t.Fatal("annotation-base preview did not retain its native form renderer")
	}
	renderer := base.formRenderer
	oneShot, err := renderPDFPage(base.original, 0, pdfPreviewDPI)
	if err != nil {
		t.Fatal(err)
	}
	persistent, err := base.renderFormPage(base.original, 0, pdfPreviewDPI, 0)
	if err != nil {
		t.Fatal(err)
	}
	if base.formRenderer != renderer {
		t.Fatal("same PDF content reopened its persistent renderer")
	}
	if oneShot.Bounds() != persistent.Bounds() {
		t.Fatalf("persistent form raster bounds = %v, want %v", persistent.Bounds(), oneShot.Bounds())
	}
	for y := 0; y < oneShot.Bounds().Dy(); y++ {
		for x := 0; x < oneShot.Bounds().Dx(); x++ {
			if color.NRGBAModel.Convert(oneShot.At(x, y)) != color.NRGBAModel.Convert(persistent.At(x, y)) {
				t.Fatalf("persistent annotation raster differs at (%d,%d)", x, y)
			}
		}
	}
	unfilledKey := base.formRendererKey
	if err = out.SetFormValue(0, fields[0].ID, "Edited again"); err != nil {
		t.Fatal(err)
	}
	if _, err = out.Preview(0); err != nil {
		t.Fatal(err)
	}
	if base.formRenderer == nil || base.formRendererKey == unfilledKey {
		t.Fatal("form-filled preview did not replace the renderer with the edited PDF")
	}
	again := reopenEditable(t, exportEditable(t, out))
	fields, err = again.FormFields(0)
	if err != nil {
		t.Fatal(err)
	}
	if fields[0].Value != "Edited again" || len(again.Overlays()) != 1 {
		t.Fatal("second form edit lost")
	}
	out.Close()
	if base.formRenderer != nil {
		t.Fatal("closing the document retained its annotation-base form renderer")
	}
}
