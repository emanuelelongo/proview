package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func redactTestPDF() []byte {
	content := "BT /F1 16 Tf 30 140 Td (PUBLIC SECRET KEEP) Tj 0 -35 Td (SECOND LINE) Tj ET\n"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content)}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Root 1 0 R /Size %d >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return out.Bytes()
}
func redactTestGui(t *testing.T) (*Gui, *signatureLayer) {
	t.Helper()
	g, layer := annotationTestGui(t)
	source, err := document.Load("redact.pdf", bytes.NewReader(redactTestPDF()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { doc.Close() })
	g.document = doc
	layer.SetDocument(doc, 0, image.Pt(600, 400))
	layer.Resize(fyne.NewSize(600, 400))
	g.bindAnnotationLayer(layer)
	return g, layer
}
func wordPosition(t *testing.T, layer *signatureLayer, word string) fyne.Position {
	t.Helper()
	layout, err := layer.document.TextGeometry(0)
	if err != nil {
		t.Fatal(err)
	}
	text := make([]rune, len(layout.Glyphs))
	for i, glyph := range layout.Glyphs {
		text[i] = glyph.Rune
	}
	index := strings.Index(string(text), word)
	if index < 0 {
		t.Fatal("missing word")
	}
	q := layout.Glyphs[index].Quad
	origin, size := layer.displayRect()
	return fyne.NewPos(origin.X+float32((q.UL.X+q.LR.X)/2)*size.Width, origin.Y+float32((q.UL.Y+q.LR.Y)/2)*size.Height)
}
func TestRedactLiveSelectionOneGestureAndCancel(t *testing.T) {
	g, layer := redactTestGui(t)
	g.setAnnotationTool(toolRedact)
	finishAnnotationJobs(t, g)
	p := layer.redactInput
	start := wordPosition(t, layer, "SECRET")
	end := wordPosition(t, layer, "SECOND")
	p.MouseDown(mouseAt(start.X, start.Y))
	p.MouseMoved(mouseAt(end.X, end.Y))
	if len(p.draftImages) != 2 || g.document.HasRedactions() {
		t.Fatal("missing live line bands or premature commit")
	}
	p.MouseUp(mouseAt(end.X, end.Y))
	p.DragEnd()
	if len(g.document.Redactions()) != 1 || len(layer.redactionWidgets) != 2 {
		t.Fatal("gesture was lost or committed twice")
	}
	if !g.document.Undo() || g.document.HasRedactions() {
		t.Fatal("gesture was not one Undo")
	}
	p.MouseDown(mouseAt(start.X, start.Y))
	p.MouseMoved(mouseAt(end.X, end.Y))
	if !g.cancelCurrentOperation() || p.active || len(p.draftImages) != 0 {
		t.Fatal("Escape left draft")
	}
	p.MouseUp(mouseAt(end.X, end.Y))
	if g.document.HasRedactions() {
		t.Fatal("cancelled selection committed")
	}
}
func TestRedactFirstGestureWaitsForGeometryAndSaveBarrier(t *testing.T) {
	g, layer := redactTestGui(t)
	g.setAnnotationTool(toolRedact)
	p := layer.redactInput
	point := wordPosition(t, layer, "SECRET")
	p.MouseDown(mouseAt(point.X, point.Y))
	p.MouseUp(mouseAt(point.X, point.Y))
	if !p.pending {
		t.Fatal("first gesture was lost during preload")
	}
	completed := false
	if !g.waitAnnotationChanges(func() { completed = g.document.HasRedactions() }) {
		t.Fatal("save did not wait for geometry")
	}
	finishAnnotationJobs(t, g)
	if !completed || len(g.document.Redactions()) != 1 {
		t.Fatal("pending gesture lost before save")
	}
	g.setAnnotationTool(toolSelect)
	mark := g.document.Redactions()[0]
	g.selectRedaction(0, mark.ID)
	g.deleteSelectedRedaction()
	if g.document.HasRedactions() || !g.document.Undo() {
		t.Fatal("deletion was not reversible")
	}
}
func TestRedactSaveWarningsKeepDestinationAndUndo(t *testing.T) {
	g, layer := redactTestGui(t)
	g.setAnnotationTool(toolRedact)
	finishAnnotationJobs(t, g)
	point := wordPosition(t, layer, "SECRET")
	layer.redactInput.MouseDown(mouseAt(point.X, point.Y))
	layer.redactInput.MouseUp(mouseAt(point.X, point.Y))
	path := filepath.Join(t.TempDir(), "saved.pdf")
	uri := storage.NewFileURI(path)
	output, err := g.exportForName("saved.pdf")
	if err != nil {
		t.Fatal(err)
	}
	g.finishFileSave(output, uri, g.document.Revision(), func() {})
	if g.document.Dirty() || !g.document.CanUndo() {
		t.Fatal("save lost history or dirty state")
	}
	if !g.document.Undo() || !g.restoresRedactions(uri) {
		t.Fatal("Undo overwrite not detected")
	}
	called := false
	g.confirmRedactionExport(uri, func() { called = true })
	tapDialogButton(t, g, "Cancel")
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, output) || called {
		t.Fatal("cancel changed destination")
	}
	exports, err := g.prepareExports()
	if err != nil {
		t.Fatal(err)
	}
	g.showPDFSaveAsDialog(exports, func() { called = true })
	tapDialogButton(t, g, "Cancel")
	data, _ = os.ReadFile(path)
	if !bytes.Equal(data, output) || called {
		t.Fatal("Save As cancellation truncated destination")
	}
}

func TestRedactionHoverIsTemporaryAndDoesNotChangeExport(t *testing.T) {
	g, layer := redactTestGui(t)
	g.setAnnotationTool(toolRedact)
	finishAnnotationJobs(t, g)
	point := wordPosition(t, layer, "SECRET")
	layer.redactInput.MouseDown(mouseAt(point.X, point.Y))
	layer.redactInput.MouseUp(mouseAt(point.X, point.Y))
	revision := g.document.Revision()
	before, err := g.exportForName("redacted.pdf")
	if err != nil {
		t.Fatal(err)
	}
	layer.redactInput.MouseMoved(mouseAt(point.X, point.Y))
	if layer.redactionWidgets[0].preview.Translucency == 0 {
		t.Fatal("Redact tool did not reveal existing band")
	}
	after, err := g.exportForName("redacted.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || g.document.Revision() != revision {
		t.Fatal("hover changed document or export")
	}
	layer.redactInput.MouseOut()
	if layer.redactionWidgets[0].preview.Translucency != 0 {
		t.Fatal("band did not become opaque on exit")
	}
	g.setAnnotationTool(toolSelect)
	mark := layer.redactionWidgets[0]
	mark.MouseIn(mouseAt(mark.Size().Width/2, mark.Size().Height/2))
	if mark.preview.Translucency == 0 {
		t.Fatal("Select tool did not reveal band")
	}
	mark.MouseOut()
	if mark.preview.Translucency != 0 {
		t.Fatal("Select hover reveal remained visible")
	}
}

func TestRedactRectangleStartsOutsideTextAndClipsToPage(t *testing.T) {
	g, layer := redactTestGui(t)
	g.setAnnotationTool(toolRedact)
	finishAnnotationJobs(t, g)
	p := layer.redactInput
	// Start in a blank region, then cross the text. The initial mode stays fixed.
	p.MouseDown(mouseAt(550, 20))
	p.MouseMoved(mouseAt(80, 180))
	if !p.rectangular || len(p.draft) != 1 || g.document.HasRedactions() {
		t.Fatal("blank start did not create a live rectangle")
	}
	q := p.draft[0]
	if q.UL.X != float64(float32(80))/600 || q.UR.X != float64(float32(550))/600 {
		t.Fatal("reverse drag geometry changed")
	}
	p.MouseUp(mouseAt(80, 180))
	p.DragEnd()
	if len(g.document.Redactions()) != 1 || !g.document.Undo() || g.document.HasRedactions() {
		t.Fatal("rectangle was not one Undo gesture")
	}
	p.MouseDown(mouseAt(300, 240))
	p.MouseMoved(mouseAt(900, 900))
	if len(p.draft) != 1 || p.draft[0].LR.X != 1 || p.draft[0].LR.Y != 1 {
		t.Fatal("rectangle escaped page bounds")
	}
	if !g.cancelCurrentOperation() {
		t.Fatal("Escape did not cancel rectangle")
	}
	p.MouseUp(mouseAt(900, 900))
	if g.document.HasRedactions() {
		t.Fatal("cancelled rectangle committed")
	}
	p.MouseDown(mouseAt(300, 240))
	p.MouseUp(mouseAt(301, 241))
	if g.document.HasRedactions() {
		t.Fatal("tiny drag created accidental redaction")
	}
}
func TestRedactRectangleOnPageWithoutTextAndDelayedGeometry(t *testing.T) {
	g, layer := redactTestGui(t)
	source, err := document.Load("blank.pdf", bytes.NewReader(scrollTestPDF(1)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { doc.Close() })
	g.document = doc
	layer.SetDocument(doc, 0, image.Pt(600, 400))
	g.setAnnotationTool(toolRedact)
	p := layer.redactInput
	p.MouseDown(mouseAt(100, 100))
	p.MouseUp(mouseAt(300, 250))
	if !p.pending {
		t.Fatal("gesture did not wait for initial geometry")
	}
	finishAnnotationJobs(t, g)
	if len(doc.Redactions()) != 1 || len(doc.Redactions()[0].Quads) != 1 {
		t.Fatal("text-free page did not accept area rectangle")
	}
	if _, err := doc.Export(document.ExportOptions{Format: document.FormatPDF}); err != nil {
		t.Fatal(err)
	}
	if !doc.Undo() || doc.HasRedactions() {
		t.Fatal("text-free rectangle Undo failed")
	}
}
