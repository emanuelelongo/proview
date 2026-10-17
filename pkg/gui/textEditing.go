package gui

import (
	"fmt"
	"golang.org/x/text/unicode/norm"
	"image/color"
	"math"
	"strings"
	"unicode"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type textCorrectionLayer struct {
	widget.BaseWidget
	editor                         *Gui
	page                           int
	doc                            *document.Document
	revision                       uint64
	layout                         document.TextLayout
	loading                        bool
	selection                      *canvas.Rectangle
	selectionRect                  document.NormalizedRect
	horizontalGuide, verticalGuide *canvas.Line
}

func newTextCorrectionLayer(g *Gui, page int) *textCorrectionLayer {
	l := &textCorrectionLayer{editor: g, page: page, selection: canvas.NewRectangle(color.NRGBA{R: 255, G: 170, A: 90})}
	l.selection.Hide()
	l.horizontalGuide = canvas.NewLine(color.NRGBA{R: 40, G: 190, B: 255, A: 255})
	l.verticalGuide = canvas.NewLine(color.NRGBA{R: 40, G: 190, B: 255, A: 255})
	l.horizontalGuide.StrokeWidth = 1.25
	l.verticalGuide.StrokeWidth = 1.25
	l.hideMoveGuides()
	l.ExtendBaseWidget(l)
	if !g.textEditing {
		l.Hide()
	}
	return l
}
func (l *textCorrectionLayer) CreateRenderer() fyne.WidgetRenderer {
	return &textCorrectionRenderer{layer: l}
}

type textCorrectionRenderer struct{ layer *textCorrectionLayer }

func (r *textCorrectionRenderer) Destroy()           {}
func (r *textCorrectionRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (r *textCorrectionRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.layer.selection, r.layer.horizontalGuide, r.layer.verticalGuide}
}
func (r *textCorrectionRenderer) Layout(size fyne.Size) {
	q := r.layer.selectionRect
	r.layer.selection.Move(fyne.NewPos(float32(q.X)*size.Width, float32(q.Y)*size.Height))
	r.layer.selection.Resize(fyne.NewSize(float32(q.Width)*size.Width, float32(q.Height)*size.Height))
	r.layer.layoutMoveGuides(size)
}
func (r *textCorrectionRenderer) Refresh() {
	r.Layout(r.layer.Size())
	r.layer.selection.Refresh()
	r.layer.horizontalGuide.Refresh()
	r.layer.verticalGuide.Refresh()
}

func (l *textCorrectionLayer) Tapped(e *fyne.PointEvent) {
	g := l.editor
	if !g.textEditing || g.working || g.document == nil || l.loading {
		return
	}
	if m := g.textMove; m != nil {
		if m.layer == l {
			return
		}
		g.cancelTextMove()
	}
	if l.doc == g.document && l.revision == g.document.Revision() {
		l.selectWord(e.Position)
		return
	}
	doc, revision := g.document, g.document.Revision()
	snapshot := doc.RenderingSnapshot()
	position := e.Position
	// Capture page-relative coordinates before the worker starts; zoom can change.
	size := l.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	point := fyne.NewPos(position.X/size.Width, position.Y/size.Height)
	l.loading = true
	go func() {
		layout, err := snapshot.CorrectionTextGeometry(l.page)
		fyne.Do(func() {
			l.loading = false
			if g.closed || g.document != doc || doc.Revision() != revision || !g.textEditing || g.correctionLayers[l.page] != l {
				return
			}
			if err != nil {
				g.showTextEditError(l.page, err)
				return
			}
			l.doc = doc
			l.revision = revision
			l.layout = layout
			l.selectWord(fyne.NewPos(point.X*l.Size().Width, point.Y*l.Size().Height))
		})
	}()
}
func (l *textCorrectionLayer) selectWord(position fyne.Position) {
	size := l.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	index := l.layout.GlyphAt(document.TextPoint{X: float64(position.X / size.Width), Y: float64(position.Y / size.Height)}, false)
	if index < 0 {
		return
	}
	start, end := correctionWordRange(l.layout, index)
	quads := l.layout.Selection(start, end)
	if len(quads) != 1 {
		return
	}
	x0, y0, x1, y1 := quads[0].Bounds()
	l.selectionRect = document.NormalizedRect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	l.selection.Move(fyne.NewPos(float32(x0)*size.Width, float32(y0)*size.Height))
	l.selection.Resize(fyne.NewSize(float32(x1-x0)*size.Width, float32(y1-y0)*size.Height))
	l.selection.Show()
	l.Refresh()
	l.editor.activatePDFPage(l.page)
	l.editor.promptTextCorrection(l, start, end)
}
func correctionWordRange(layout document.TextLayout, index int) (int, int) {
	start, end := index, index
	for start > 0 && layout.Glyphs[start-1].Line == layout.Glyphs[index].Line && !unicode.IsSpace(layout.Glyphs[start-1].Rune) {
		start--
	}
	for end+1 < len(layout.Glyphs) && layout.Glyphs[end+1].Line == layout.Glyphs[index].Line && !unicode.IsSpace(layout.Glyphs[end+1].Rune) {
		end++
	}
	return start, end
}
func (g *Gui) toggleTextEditing() {
	if g.document == nil || g.document.Format() != document.FormatPDF || g.document.HasRedactions() || g.working {
		return
	}
	active := !g.textEditing
	g.setAnnotationTool(toolSelect)
	g.closeSearch()
	g.setTextEditing(active)
}
func (g *Gui) setTextEditing(active bool) {
	if !active {
		g.cancelTextMove()
	}
	g.textEditing = active
	if g.textEditItem != nil {
		g.textEditItem.Checked = active
	}
	if g.textEditAction != nil {
		g.textEditAction.SetActive(active)
	}
	for _, l := range g.correctionLayers {
		l.selection.Hide()
		if active {
			l.Show()
		} else {
			l.Hide()
		}
	}
	if !active && g.correctionDialog != nil {
		g.correctionDialog.Hide()
	}
}
func (g *Gui) promptTextCorrection(l *textCorrectionLayer, start, end int) {
	if g.correctionDialog != nil {
		g.correctionDialog.Hide()
	}
	var value []rune
	for _, glyph := range l.layout.Glyphs[start : end+1] {
		value = append(value, glyph.Rune)
	}
	original := string(value)
	text := widget.NewEntry()
	text.SetText(original)
	var d *dialog.CustomDialog
	apply := widget.NewButton("Apply", nil)
	apply.Importance = widget.HighImportance
	move := newIconButton(tablerIcon("arrows-move"), "Move", nil)
	duplicate := newIconButton(tablerIcon("copy-plus"), "Duplicate", nil)
	submit := func(value string) {
		if g.working {
			return
		}
		if value == original {
			d.Hide()
			return
		}
		apply.Disable()
		move.Disable()
		duplicate.Disable()
		g.applyTextEdit(l, start, end, value, 0, 0, func() bool { return g.correctionDialog == d }, func(err error, _ document.TextLayout) {
			apply.Enable()
			move.Enable()
			if strings.TrimSpace(text.Text) != "" {
				duplicate.Enable()
			}
			if err != nil {
				g.showTextEditError(l.page, err)
				return
			}
			d.Hide()
		})
	}
	apply.OnTapped = func() { submit(text.Text) }
	move.OnTapped = func() {
		if g.working {
			return
		}
		replacement := text.Text
		d.Hide()
		g.beginTextMove(l, start, end, replacement)
	}
	duplicate.OnTapped = func() {
		if g.working || strings.TrimSpace(text.Text) == "" {
			return
		}
		value := text.Text
		d.Hide()
		g.beginTextMove(l, start, end, value)
		if g.textMove != nil {
			g.textMove.duplicate = true
			g.textMove.references = textAlignmentReferences(l.layout, -1, -1)
		}
	}
	text.OnChanged = func(value string) {
		if strings.TrimSpace(value) == "" {
			duplicate.Disable()
		} else {
			duplicate.Enable()
		}
	}
	cancel := widget.NewButton("Cancel", func() { d.Hide() })
	row := container.NewBorder(nil, nil, nil, container.NewHBox(move, duplicate), text)
	form := widget.NewForm(widget.NewFormItem("Text", row))
	content := container.NewVBox(form, container.NewGridWithColumns(2, cancel, apply))
	tooltips := newTooltipSurface(content)
	move.tooltipHost, duplicate.tooltipHost = tooltips, tooltips
	d = dialog.NewCustomWithoutButtons("Edit PDF Text", tooltips, g.window)
	g.correctionDialog = d
	d.SetOnClosed(func() {
		move.hideTooltip()
		duplicate.hideTooltip()
		l.selection.Hide()
		if g.correctionDialog == d {
			g.correctionDialog = nil
		}
	})
	d.Resize(fyne.NewSize(400, 170))
	d.Show()
	g.window.Canvas().Focus(text)
	text.TypedShortcut(&fyne.ShortcutSelectAll{})
}

// Both dialog actions and mouse moves use the same native transaction and
// stale-result guards. The caller owns cancellation of the transient UI state.
func (g *Gui) applyTextEdit(l *textCorrectionLayer, start, end int, value string, dx, dy float64, valid func() bool, done func(error, document.TextLayout)) {
	doc, revision := l.doc, l.revision
	if g.working {
		return
	}
	if doc == nil || g.document != doc || doc.Revision() != revision {
		done(fmt.Errorf("The document changed; select the text again"), document.TextLayout{})
		return
	}
	keepMoving := g.textMove != nil && g.textMove.layer == l
	duplicate := keepMoving && g.textMove.duplicate
	snapshot := doc.RenderingSnapshot()
	g.queueAnnotation(func() func() {
		var prepared *document.PreparedTextCorrection
		var err error
		if duplicate {
			prepared, err = snapshot.PrepareTextDuplication(l.page, start, end-start+1, value, dx, dy)
		} else {
			prepared, err = snapshot.PrepareTextCorrection(l.page, start, end-start+1, value, dx, dy)
		}
		var updated document.TextLayout
		if err == nil && keepMoving {
			updated, err = prepared.TextGeometry(l.page)
		}
		return func() {
			defer prepared.Close()
			if g.closed || !valid() {
				return
			}
			if g.document != doc || doc.Revision() != revision {
				done(fmt.Errorf("The document changed; select the text again"), document.TextLayout{})
				return
			}
			if err == nil {
				err = doc.ApplyTextCorrection(prepared)
			}
			if err == nil {
				g.clearTextSelection()
				g.syncDocumentView()
			}
			done(err, updated)
		}
	})
}

type textMoveState struct {
	layer                *textCorrectionLayer
	start, end           int
	value                string
	base                 document.NormalizedRect
	offset               fyne.Position
	rawOffset            fyne.Position
	references           []textAlignmentReference
	horizontal, vertical textSnapAxis
	dragging             bool
	duplicate            bool
}

func (g *Gui) beginTextMove(l *textCorrectionLayer, start, end int, value string) {
	g.cancelTextMove()
	if l.doc == nil || g.document != l.doc || l.doc.Revision() != l.revision || !g.textEditing {
		return
	}
	g.textMove = &textMoveState{layer: l, start: start, end: end, value: norm.NFC.String(value), base: l.selectionRect, references: textAlignmentReferences(l.layout, start, end)}
	l.selection.Show()
	l.Refresh()
	g.window.Canvas().Focus(nil)
}
func (g *Gui) cancelTextMove() bool {
	if g.textMove == nil {
		return false
	}
	m := g.textMove
	g.textMove = nil
	m.layer.selectionRect = m.base
	m.layer.selection.Hide()
	m.layer.hideMoveGuides()
	m.layer.Refresh()
	return true
}
func (l *textCorrectionLayer) Cursor() desktop.Cursor {
	if m := l.editor.textMove; m != nil && m.layer == l {
		return desktop.CrosshairCursor
	}
	return desktop.TextCursor
}
func (l *textCorrectionLayer) Dragged(e *fyne.DragEvent) {
	g := l.editor
	m := g.textMove
	if m == nil || m.layer != l || !g.textEditing || g.working {
		return
	}
	if g.document != l.doc || l.doc.Revision() != l.revision {
		g.cancelTextMove()
		return
	}
	size := l.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	if !m.dragging {
		x, y := (e.Position.X-e.Dragged.DX)/size.Width, (e.Position.Y-e.Dragged.DY)/size.Height
		if float64(x) < m.base.X || float64(x) > m.base.X+m.base.Width || float64(y) < m.base.Y || float64(y) > m.base.Y+m.base.Height {
			return
		}
		m.dragging = true
	}
	m.rawOffset.X += e.Dragged.DX / size.Width
	m.rawOffset.Y += e.Dragged.DY / size.Height
	g.refreshTextMoveAlignment()
}

func (l *textCorrectionLayer) DragEnd() {
	g := l.editor
	m := g.textMove
	if m == nil || m.layer != l || !m.dragging || g.working {
		return
	}
	m.dragging = false
	m.horizontal, m.vertical = textSnapAxis{}, textSnapAxis{}
	l.hideMoveGuides()
	dx, dy := float64(m.offset.X)*l.layout.Width, float64(m.offset.Y)*l.layout.Height
	var original []rune
	for _, glyph := range l.layout.Glyphs[m.start : m.end+1] {
		original = append(original, glyph.Rune)
	}
	if !m.duplicate && math.Abs(dx) < .001 && math.Abs(dy) < .001 && m.value == string(original) {
		m.rawOffset, m.offset = fyne.Position{}, fyne.Position{}
		l.selectionRect = m.base
		l.Refresh()
		return
	}
	g.applyTextEdit(l, m.start, m.end, m.value, dx, dy, func() bool { return g.textMove == m }, func(err error, updated document.TextLayout) {
		if err != nil {
			m.rawOffset = fyne.Position{}
			m.offset = fyne.Position{}
			l.selectionRect = m.base
			l.Refresh()
			g.showTextEditError(l.page, err)
			return
		}
		// syncDocumentView canceled the old transient move before this callback.
		// Identify the replacement by value and expected baseline origin, not by
		// old extraction indexes (which change after native insertion).
		if !g.textEditing || strings.TrimSpace(m.value) == "" {
			return
		}
		origin := l.layout.Glyphs[m.start].Origin
		origin.X += dx / l.layout.Width
		origin.Y += dy / l.layout.Height
		start, end, ok := findMovedText(updated, m.value, origin)
		if !ok {
			return
		}
		l.doc = g.document
		l.revision = g.document.Revision()
		l.layout = updated
		l.selectionRect = textRangeRect(updated, start, end)
		g.beginTextMove(l, start, end, m.value)
	})
}

// Source replacement is reversible in document history. Refresh base-page
// images when that source changes, including Undo/Redo, without rebuilding UI.
func (g *Gui) refreshCorrectionPreviews() {
	if g.document == nil || g.document.Format() != document.FormatPDF {
		return
	}
	if g.correctionPreviewKeys == nil {
		g.correctionPreviewKeys = map[int]string{}
	}
	for page := range g.pdfPageImages {
		key := g.document.TextSelectionKey(page)
		if g.correctionPreviewKeys[page] == key {
			continue
		}
		g.correctionPreviewKeys[page] = key
		if !g.pdfPageLoaded[page] {
			continue
		}
		doc, revision, generation := g.document, g.document.Revision(), g.pdfGeneration
		snapshot := doc.RenderingSnapshot()
		go func() {
			preview, err := snapshot.RenderBasePage(page)
			fyne.Do(func() {
				if g.closed || g.document != doc || doc.Revision() != revision || g.pdfGeneration != generation {
					return
				}
				if err != nil {
					g.logEvent("operation_failed", "level=ERROR operation=pdf_text_preview page=%d error=%q", page+1, err)
					g.showErrorMessage(operationErrorMessage("preview", err))
					return
				}
				g.pdfPageImages[page].Image = preview
				g.pdfPageImages[page].Refresh()
			})
		}()
	}
}
