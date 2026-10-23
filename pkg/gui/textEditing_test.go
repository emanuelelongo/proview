package gui

import (
	"bytes"
	"codeberg.org/go-pdf/fpdf"
	"fmt"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func textMoveTestGui(t *testing.T) (*Gui, *textCorrectionLayer) {
	t.Helper()
	app := fyneTest.NewApp()
	t.Cleanup(app.Quit)
	g := newGui(app)
	source, err := document.Load("text.pdf", bytes.NewReader(searchTextFixture(t, "PUBLIC SECRET KEEP")))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(doc.Close)
	g.document = doc
	g.pdfPageIDs = doc.PageIDs()
	g.annotationJobs = make(chan func() func(), 1)
	l := newTextCorrectionLayer(g, 0)
	g.correctionLayers = map[int]*textCorrectionLayer{0: l}
	l.doc = doc
	l.revision = doc.Revision()
	l.layout, err = doc.RenderingSnapshot().CorrectionTextGeometry(0)
	if err != nil {
		t.Fatal(err)
	}
	g.window.SetContent(l)
	l.Resize(fyne.NewSize(600, 800))
	g.toggleTextEditing()
	q := l.layout.Glyphs[8].Quad
	size := l.Size()
	l.selectWord(fyne.NewPos(float32((q.UL.X+q.LR.X)/2)*size.Width, float32((q.UL.Y+q.LR.Y)/2)*size.Height))
	return g, l
}

func textDialogControls(t *testing.T, g *Gui) (map[string]*widget.Button, []*widget.Entry) {
	t.Helper()
	buttons := map[string]*widget.Button{}
	var entries []*widget.Entry
	seen := map[fyne.CanvasObject]bool{}
	var visit func(fyne.CanvasObject)
	visit = func(o fyne.CanvasObject) {
		if o == nil || seen[o] {
			return
		}
		seen[o] = true
		if b, ok := o.(*iconButton); ok {
			buttons[b.tooltip] = &b.Button
			return
		}
		if b, ok := o.(*widget.Button); ok {
			buttons[b.Text] = b
			return
		}
		if e, ok := o.(*widget.Entry); ok {
			entries = append(entries, e)
			return
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				visit(child)
			}
		} else if w, ok := o.(fyne.Widget); ok {
			for _, child := range fyneTest.WidgetRenderer(w).Objects() {
				visit(child)
			}
		}
	}
	for _, o := range g.window.Canvas().Overlays().List() {
		visit(o)
	}
	return buttons, entries
}

func textLayoutString(l document.TextLayout) string {
	var b strings.Builder
	for _, g := range l.Glyphs {
		b.WriteRune(g.Rune)
	}
	return b.String()
}

func TestTextEditingEmptyValueDeletesInOneUndo(t *testing.T) {
	g, _ := textMoveTestGui(t)
	buttons, entries := textDialogControls(t, g)
	if len(entries) != 1 || buttons["Delete"] != nil || buttons["Move"] == nil {
		t.Fatalf("unexpected dialog: entries=%d buttons=%v", len(entries), buttons)
	}
	entries[0].SetText("")
	fyneTest.Tap(buttons["Apply"])
	finishAnnotationJobs(t, g)
	layout, err := g.document.RenderingSnapshot().CorrectionTextGeometry(0)
	if err != nil {
		t.Fatal(err)
	}
	if value := textLayoutString(layout); strings.Contains(value, "SECRET") || !strings.Contains(value, "KEEP") {
		t.Fatal(value)
	}
	if g.correctionDialog != nil || !g.document.Dirty() {
		t.Fatal("delete did not finish")
	}
	if !g.document.Undo() || g.document.Dirty() || g.document.CanUndo() {
		t.Fatal("delete was not one undo step")
	}
}

func TestTextEditingMouseMoveUsesPageUnitsAndOneUndo(t *testing.T) {
	for _, zoom := range []float32{1, 2} {
		t.Run(fmt.Sprint(zoom), func(t *testing.T) {
			g, l := textMoveTestGui(t)
			buttons, entries := textDialogControls(t, g)
			entries[0].SetText("EDIT")
			fyneTest.Tap(buttons["Move"])
			if g.correctionDialog != nil || g.textMove == nil || !l.selection.Visible() {
				t.Fatal("Move did not activate dragging")
			}
			l.Resize(fyne.NewSize(600*zoom, 800*zoom))
			size := l.Size()
			base := g.textMove.base
			original := l.layout.Glyphs[g.textMove.start]
			pageHeight := l.layout.Height
			x, y := float32(base.X+base.Width/2)*size.Width, float32(base.Y+base.Height/2)*size.Height
			delta := size.Height * .1
			l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y+delta)}, Dragged: fyne.Delta{DY: delta}})
			if g.document.Dirty() || g.annotationPending != 0 {
				t.Fatal("drag preview changed history")
			}
			l.DragEnd()
			if g.annotationPending != 1 {
				t.Fatal("drop did not submit exactly one edit")
			}
			finishAnnotationJobs(t, g)
			layout, err := g.document.RenderingSnapshot().CorrectionTextGeometry(0)
			if err != nil {
				t.Fatal(err)
			}
			value := textLayoutString(layout)
			idx := strings.Index(value, "EDIT")
			if idx < 0 || strings.Contains(value, "SECRET") {
				t.Fatal(value)
			}
			glyph := layout.Glyphs[len([]rune(value[:idx]))]
			if math.Abs((glyph.Quad.UL.Y-original.Quad.UL.Y)*layout.Height-.1*pageHeight) > .03 {
				t.Fatal("move depended on display zoom")
			}
			if g.textMove == nil || !l.selection.Visible() || l.revision != g.document.Revision() {
				t.Fatal("drop did not rearm Move with updated geometry")
			}
			base = g.textMove.base
			x, y = float32(base.X+base.Width/2)*size.Width, float32(base.Y+base.Height/2)*size.Height
			l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y+delta)}, Dragged: fyne.Delta{DY: delta}})
			if g.textMove == nil || !g.textMove.dragging {
				t.Fatalf("second drag not active doc=%p layerdoc=%p revision=%d/%d working=%v move=%+v", g.document, l.doc, g.document.Revision(), l.revision, g.working, g.textMove)
			}
			l.DragEnd()
			if g.annotationPending != 1 {
				t.Fatalf("second drop pending=%d", g.annotationPending)
			}
			finishAnnotationJobs(t, g)
			if g.textMove == nil || math.Abs(g.textMove.base.Y-base.Y-.1) > .0001 {
				t.Fatalf("second drop did not move the updated text: move=%+v previous=%+v size=%+v current=%+v pending=%d", g.textMove, base, size, l.Size(), g.annotationPending)
			}
			if !g.document.Undo() || !g.document.Dirty() {
				t.Fatal("second move was not a separate undo")
			}
			if !g.document.Undo() || g.document.Dirty() || g.document.CanUndo() {
				t.Fatal("first move and pending text change were not one undo")
			}
		})
	}
}

func TestTextEditingEscapeCancelsPendingMouseMove(t *testing.T) {
	g, l := textMoveTestGui(t)
	buttons, _ := textDialogControls(t, g)
	fyneTest.Tap(buttons["Move"])
	base := l.selectionRect
	size := l.Size()
	x, y := float32(base.X+base.Width/2)*size.Width, float32(base.Y+base.Height/2)*size.Height
	l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y+80)}, Dragged: fyne.Delta{DY: 80}})
	l.DragEnd()
	if !g.cancelCurrentOperation() || g.textMove != nil || !g.textEditing || l.selectionRect != base {
		t.Fatal("Escape did not cancel only the move")
	}
	finishAnnotationJobs(t, g)
	if g.document.Dirty() || g.document.CanUndo() {
		t.Fatal("canceled move committed after worker completion")
	}
}

func searchTextFixture(t *testing.T, text string) []byte {
	t.Helper()
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 14)
	pdf.Text(30, 40, text)
	var b bytes.Buffer
	if err := pdf.Output(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestTextCorrectionModeIsSeparateFromForms(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	if !g.textEditAction.button.Disabled() {
		t.Fatal("text editing enabled without PDF")
	}
	data, err := os.ReadFile("../../internal/pdfengine/testdata/forms.pdf")
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("forms.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	g.document = doc
	l := newTextCorrectionLayer(g, 0)
	g.correctionLayers = map[int]*textCorrectionLayer{0: l}
	g.updateHistoryActions()
	if g.textEditAction.button.Disabled() || l.Visible() {
		t.Fatal("incorrect initial mode state")
	}
	g.setFormsActive(true)
	g.toggleTextEditing()
	if !g.textEditing || g.formsActive || !l.Visible() {
		t.Fatal("text editing did not deactivate forms")
	}
	if g.currentToolIndicator.icon.Resource.Name() != tablerIcon("file-type-pdf").Name() {
		t.Fatal("tool indicator did not follow PDF text editing")
	}
	g.toggleForms()
	if g.textEditing || !g.formsActive || l.Visible() {
		t.Fatal("forms did not deactivate text editing")
	}
	if g.currentToolIndicator.icon.Resource.Name() != tablerIcon("forms").Name() {
		t.Fatal("tool indicator did not follow PDF form filling")
	}
	g.toggleTextEditing()
	g.cancelCurrentOperation()
	if g.textEditing || l.Visible() {
		t.Fatal("Escape did not exit correction mode")
	}
}

func TestTextCorrectionSelectionDialogAndZoom(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	// Reuse the native text fixture emitted by the search test's PDF helper.
	data := searchTextFixture(t, "PUBLIC SECRET KEEP")
	source, err := document.Load("text.pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	g.document = doc
	l := newTextCorrectionLayer(g, 0)
	g.correctionLayers = map[int]*textCorrectionLayer{0: l}
	l.doc = doc
	l.revision = doc.Revision()
	l.layout, err = doc.RenderingSnapshot().CorrectionTextGeometry(0)
	if err != nil {
		t.Fatal(err)
	}
	g.window.SetContent(l)
	l.Resize(fyne.NewSize(600, 800))
	g.toggleTextEditing()
	start, end := correctionWordRange(l.layout, 8)
	if start != 7 || end != 12 {
		t.Fatalf("wrong word bounds %d,%d", start, end)
	}
	q := l.layout.Glyphs[8].Quad
	l.selectWord(fyne.NewPos(float32((q.UL.X+q.LR.X)/2)*600, float32((q.UL.Y+q.LR.Y)/2)*800))
	if g.correctionDialog == nil || !l.selection.Visible() || doc.Dirty() {
		t.Fatal("selection failed or modified document")
	}
	if path := os.Getenv("PROVIEW_TEXT_EDIT_UI_PREVIEW"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, g.window.Canvas().Capture())
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	position, size := l.selection.Position(), l.selection.Size()
	l.Resize(fyne.NewSize(1200, 1600))
	if l.selection.Position() != fyne.NewPos(position.X*2, position.Y*2) || l.selection.Size() != fyne.NewSize(size.Width*2, size.Height*2) {
		t.Fatal("selection does not follow zoom")
	}
	if !g.cancelCurrentOperation() || g.correctionDialog != nil || !g.textEditing || l.selection.Visible() {
		t.Fatal("Escape did not close only the dialog")
	}
	g.setAnnotationTool(toolSelect)
	if g.textEditing {
		t.Fatal("tool switch left correction mode active")
	}
}

func TestTextMoveAlignmentShiftAndZoom(t *testing.T) {
	for _, zoom := range []float32{1, 2} {
		t.Run(fmt.Sprint(zoom), func(t *testing.T) {
			g, l := textMoveTestGui(t)
			buttons, _ := textDialogControls(t, g)
			fyneTest.Tap(buttons["Move"])
			l.Resize(fyne.NewSize(600*zoom, 800*zoom))
			m, size := g.textMove, l.Size()
			// Different top edges but matching baselines: alignment must use
			// the font baseline, not the glyph bounding box.
			baselineOffset := l.layout.Glyphs[m.start].Origin.Y - m.base.Y
			targetX, targetY := m.base.X+.1, m.base.Y+.15
			ref := textAlignmentReference{
				rect:     document.NormalizedRect{X: targetX, Y: targetY - .02, Width: .1, Height: .06},
				baseline: targetY + baselineOffset,
			}
			m.references = []textAlignmentReference{ref}
			m.dragging = true
			m.rawOffset = fyne.NewPos(float32(.1)+4/size.Width, float32(.15)+4/size.Height)
			g.refreshTextMoveAlignment()
			check := func(wantX, wantY float64) {
				t.Helper()
				if math.Abs(l.selectionRect.X-wantX)*float64(size.Width) > .001 || math.Abs(l.selectionRect.Y-wantY)*float64(size.Height) > .001 {
					t.Fatalf("unexpected position: %+v; want %f,%f", l.selectionRect, wantX, wantY)
				}
				if !l.horizontalGuide.Visible() || !l.verticalGuide.Visible() {
					t.Fatal("alignment guides missing")
				}
				if math.Abs(float64(l.horizontalGuide.Position1.Y)-ref.baseline*float64(size.Height)) > .001 {
					t.Fatal("horizontal guide does not follow the baseline")
				}
			}
			check(targetX, targetY)
			g.handleSelectionShift(desktop.KeyShiftLeft, true)
			check(targetX+4/float64(size.Width), targetY+4/float64(size.Height))
			g.handleSelectionShift(desktop.KeyShiftLeft, false)
			check(targetX, targetY)
			// Hysteresis retains an existing guide beyond the acquisition
			// distance, then releases it without accumulating snapped deltas.
			m.rawOffset = fyne.NewPos(float32(.1)+8/size.Width, float32(.15)+8/size.Height)
			g.refreshTextMoveAlignment()
			check(targetX, targetY)
			m.rawOffset = fyne.NewPos(float32(.1)+12/size.Width, float32(.15)+12/size.Height)
			g.refreshTextMoveAlignment()
			if m.horizontal.active || m.vertical.active || l.horizontalGuide.Visible() || l.verticalGuide.Visible() {
				t.Fatal("guides did not release outside tolerance")
			}
			if math.Abs(l.selectionRect.X-targetX-12/float64(size.Width))*float64(size.Width) > .001 {
				t.Fatal("snap altered raw pointer displacement")
			}
			// Outside the acquisition radius a released guide stays inactive.
			m.rawOffset = fyne.NewPos(float32(.1)+8/size.Width, float32(.15)+8/size.Height)
			g.refreshTextMoveAlignment()
			if m.horizontal.active || m.vertical.active {
				t.Fatal("guide acquired beyond screen-space tolerance")
			}
			g.cancelTextMove()
			if l.horizontalGuide.Visible() || l.verticalGuide.Visible() || l.selection.Visible() {
				t.Fatal("cancel left transient geometry visible")
			}
		})
	}
}

func TestTextMoveNoOpRemainsArmed(t *testing.T) {
	g, l := textMoveTestGui(t)
	buttons, _ := textDialogControls(t, g)
	fyneTest.Tap(buttons["Move"])
	m := g.textMove
	m.dragging = true
	m.rawOffset = fyne.NewPos(.004, 0) // A raw drag may have snapped back to the starting position.
	l.DragEnd()
	if g.textMove != m || !l.selection.Visible() || g.annotationPending != 0 || g.document.Dirty() || m.rawOffset != (fyne.Position{}) {
		t.Fatal("no-op drop changed history or exited Move")
	}
	if !g.cancelCurrentOperation() || g.textMove != nil || !g.textEditing {
		t.Fatal("Escape did not exit Move while retaining the editing tool")
	}
}

func TestPDFTextDuplicateStartsDraggingAndCreatesOneEditableCopy(t *testing.T) {
	for _, zoom := range []float32{1, 2} {
		t.Run(fmt.Sprint(zoom), func(t *testing.T) {
			g, l := textMoveTestGui(t)
			buttons, entries := textDialogControls(t, g)
			duplicate := buttons["Duplicate"]
			if duplicate == nil || duplicate.Icon == nil || !strings.Contains(duplicate.Icon.Name(), "copy-plus") {
				t.Fatal("missing Duplicate icon")
			}
			entries[0].SetText("EDIT")
			fyneTest.Tap(duplicate)
			if g.correctionDialog != nil || g.textMove == nil || !g.textMove.duplicate || g.document.Dirty() || g.annotationPending != 0 {
				t.Fatal("duplicate did not arm without mutation")
			}
			l.Resize(fyne.NewSize(600*zoom, 800*zoom))
			m, size := g.textMove, l.Size()
			x, y := float32(m.base.X+m.base.Width/2)*size.Width, float32(m.base.Y+m.base.Height/2)*size.Height
			delta := size.Height * .1
			l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y+delta)}, Dragged: fyne.Delta{DY: delta}})
			if g.document.Dirty() {
				t.Fatal("duplicate preview changed history")
			}
			l.DragEnd()
			finishAnnotationJobs(t, g)
			geometry, err := g.document.RenderingSnapshot().CorrectionTextGeometry(0)
			if err != nil {
				t.Fatal(err)
			}
			value := textLayoutString(geometry)
			if strings.Count(value, "SECRET") != 1 || strings.Count(value, "EDIT") != 1 {
				t.Fatalf("original/copy incorrect: %q", value)
			}
			if g.textMove == nil || g.textMove.duplicate || g.textMove.value != "EDIT" {
				t.Fatal("copy did not continue as Move")
			}
			// Refine the copy's position; this must move the copy rather than duplicate again.
			m = g.textMove
			x, y = float32(m.base.X+m.base.Width/2)*size.Width, float32(m.base.Y+m.base.Height/2)*size.Height
			l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y+delta)}, Dragged: fyne.Delta{DY: delta}})
			l.DragEnd()
			finishAnnotationJobs(t, g)
			geometry, err = g.document.RenderingSnapshot().CorrectionTextGeometry(0)
			if err != nil {
				t.Fatal(err)
			}
			value = textLayoutString(geometry)
			if strings.Count(value, "SECRET") != 1 || strings.Count(value, "EDIT") != 1 {
				t.Fatal("refinement made another copy")
			}
			if !g.document.Undo() || !g.document.Dirty() || !g.document.Undo() || g.document.Dirty() || g.document.CanUndo() {
				t.Fatal("duplicate/refinement history incorrect")
			}
		})
	}
}

func TestPDFTextDuplicateRejectsOriginalPositionAndCanRetry(t *testing.T) {
	g, l := textMoveTestGui(t)
	buttons, _ := textDialogControls(t, g)
	fyneTest.Tap(buttons["Duplicate"])
	m := g.textMove
	m.dragging = true
	l.DragEnd()
	finishAnnotationJobs(t, g)
	if g.document.Dirty() || g.document.CanUndo() || g.textMove != m || !m.duplicate || m.offset != (fyne.Position{}) {
		t.Fatal("rejected duplicate changed state")
	}
	tapDialogButton(t, g, "OK")
	size := l.Size()
	x, y := float32(m.base.X+m.base.Width/2)*size.Width, float32(m.base.Y+m.base.Height/2)*size.Height
	delta := size.Height * .1
	l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y+delta)}, Dragged: fyne.Delta{DY: delta}})
	l.DragEnd()
	finishAnnotationJobs(t, g)
	geometry, err := g.document.RenderingSnapshot().CorrectionTextGeometry(0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(textLayoutString(geometry), "SECRET") != 2 || !g.document.Dirty() {
		t.Fatal("retry failed")
	}
}

func TestPDFTextDuplicateCancellationAndEmptyValue(t *testing.T) {
	g, _ := textMoveTestGui(t)
	buttons, entries := textDialogControls(t, g)
	entries[0].SetText("")
	if !buttons["Duplicate"].Disabled() {
		t.Fatal("empty duplicate enabled")
	}
	entries[0].SetText("SECRET")
	fyneTest.Tap(buttons["Duplicate"])
	if !g.cancelCurrentOperation() || g.textMove != nil || !g.textEditing || g.document.Dirty() {
		t.Fatal("canceling duplicate changed document")
	}
}

func TestPDFTextEditAndMoveRejectOverlapWithoutHistory(t *testing.T) {
	for _, mode := range []string{"apply", "move"} {
		t.Run(mode, func(t *testing.T) {
			g, l := textMoveTestGui(t)
			buttons, entries := textDialogControls(t, g)
			if mode == "apply" {
				entries[0].SetText("LONG LONG LONG")
				fyneTest.Tap(buttons["Apply"])
			} else {
				fyneTest.Tap(buttons["Move"])
				m := g.textMove
				value := textLayoutString(l.layout)
				keep := len([]rune(value[:strings.Index(value, "KEEP")]))
				dx := l.layout.Glyphs[keep].Origin.X - l.layout.Glyphs[m.start].Origin.X
				size := l.Size()
				x, y := float32(m.base.X+m.base.Width/2)*size.Width, float32(m.base.Y+m.base.Height/2)*size.Height
				delta := float32(dx) * size.Width
				l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x+delta, y)}, Dragged: fyne.Delta{DX: delta}})
				l.DragEnd()
			}
			finishAnnotationJobs(t, g)
			if g.document.Dirty() || g.document.CanUndo() {
				t.Fatal("overlap rejection changed document")
			}
			if g.window.Canvas().Overlays().Top() == nil {
				t.Fatal("overlap was silently ignored")
			}
			if mode == "move" && (g.textMove == nil || g.textMove.offset != (fyne.Position{})) {
				t.Fatal("failed move did not reset for retry")
			}
		})
	}
}
