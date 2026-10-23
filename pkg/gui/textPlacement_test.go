package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"fmt"
	"fyne.io/fyne/v2/theme"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestInlineTextStyleChangesStayInDraftAndFollowZoom(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 250)})
	p := g.textPlacement
	p.entry.SetText("Styled text")
	var fonts, sizes *compactSelect
	for _, object := range fyneTest.LaidOutObjects(g.annotationControls) {
		if _, ok := object.(*annotationEntry); ok {
			t.Fatal("new text entry is still in the property bar")
		}
		if selectBox, ok := object.(*compactSelect); ok {
			if selectBox.Options[0] == annotations.Fonts[0] {
				fonts = selectBox
			} else if selectBox.Options[0] == "6" {
				sizes = selectBox
			}
		}
	}
	if fonts == nil || sizes == nil {
		t.Fatal("inline editing lost its font/size controls")
	}
	// Moving focus to properties must preserve the draft until confirmation.
	g.window.Canvas().Focus(nil)
	fonts.SetSelected("Go Mono")
	if fonts.button.tooltip != "Font: Monospace" || fonts.button.Icon.Name() != tablerIcon("typography").Name() {
		t.Fatal("font selector is missing its descriptive name or typography icon")
	}
	sizes.SetSelected("24")
	if p.data.Font != "Go Mono" || p.data.Size != 24 || p.data.Text != "Styled text" || g.document.Dirty() {
		t.Fatal("style changes committed text or lost the draft")
	}
	previousSize, previousPoint := p.size, p.point
	layer.Resize(fyne.NewSize(2000, 1000))
	layer.Refresh()
	if p.point != previousPoint || math.Abs(float64(p.size-previousSize*2)) > .01 {
		t.Fatal("zoom changed physical insertion position or did not scale the editor")
	}
	p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	finishAnnotationJobs(t, g)
	data, _ := g.document.Overlays()[0].Annotation()
	if data.Font != "Go Mono" || data.Size != 24 || data.Text != "Styled text" {
		t.Fatal("confirmation lost draft styles")
	}
}

func TestInlineTextSaveWaitsAndFailedPlacementKeepsInput(t *testing.T) {
	for _, text := range []string{"Save this text", strings.Repeat("x", 2001)} {
		t.Run(text[:4], func(t *testing.T) {
			g, layer := annotationTestGui(t)
			g.setAnnotationTool(toolText)
			layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 250)})
			p := g.textPlacement
			p.entry.SetText(text)
			saved := false
			if !g.waitAnnotationChanges(func() { saved = true }) || saved {
				t.Fatal("save did not wait for text creation")
			}
			finishAnnotationJobs(t, g)
			if len(text) > 2000 {
				if saved || g.textPlacement != p || p.entry.Text != text || g.document.Dirty() || len(g.document.Overlays()) != 0 {
					t.Fatal("failed placement lost input or continued saving")
				}
			} else if !saved || g.textPlacement != nil || len(g.document.Overlays()) != 1 {
				t.Fatal("save did not receive the confirmed text")
			}
		})
	}
}

func TestInlineTextPDFUsesPhysicalCoordinates(t *testing.T) {
	g, layer := redactTestGui(t)
	g.window.SetContent(layer)
	g.setAnnotationTool(toolText)
	origin, display := layer.displayRect()
	layer.input.Tapped(&fyne.PointEvent{Position: origin.Add(fyne.NewPos(display.Width*.2, display.Height*.3))})
	p := g.textPlacement
	if p == nil {
		t.Fatal("PDF click did not open text editor")
	}
	p.entry.SetText("PDF note")
	p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	finishAnnotationJobs(t, g)
	o := g.document.Overlays()[0]
	_, _, metrics, err := p.data.TextArtwork()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(o.Rect.X+metrics.Left/p.data.PageWidth-.2) > 1e-6 || math.Abs(o.Rect.Y+p.data.Padding/p.data.PageHeight-(.3-9/p.data.PageHeight)) > 1e-6 {
		t.Fatal("PDF insertion used preview pixels as PDF points", o.Rect)
	}
}

func placeTestText(t *testing.T, g *Gui, layer *signatureLayer, position fyne.Position, text string) {
	t.Helper()
	layer.input.Tapped(&fyne.PointEvent{Position: position})
	if g.textPlacement == nil || g.textEntry == nil {
		t.Fatal("page click did not open inline text entry")
	}
	g.textEntry.SetText(text)
	g.textEntry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	finishAnnotationJobs(t, g)
}

func TestInlineTextStartsOnPageAndCreatesOneUndo(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	if g.textEntry != nil || g.textPlacement != nil {
		t.Fatal("selecting Text still requires typing into the toolbar")
	}
	layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 250)})
	p := g.textPlacement
	if p == nil || g.window.Canvas().Focused() != p.entry || g.document.Dirty() || p.point != (annotations.Point{X: 100, Y: 91}) {
		t.Fatal("page click did not create a focused, transient draft at the insertion point")
	}
	p.entry.SetText("Hello\nworld")
	if p.entry.Text != "Hello world" || g.document.Dirty() {
		t.Fatal("typing modified history or retained multiline input")
	}
	p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	finishAnnotationJobs(t, g)
	if g.textPlacement != nil || g.textEntry != nil || len(g.document.Overlays()) != 1 || g.annotationTool != toolText {
		t.Fatal("submission did not place exactly one text object")
	}
	o := g.document.Overlays()[0]
	data, _ := o.Annotation()
	_, _, metrics, err := data.TextArtwork()
	if err != nil {
		t.Fatal(err)
	}
	if data.Text != "Hello world" || math.Abs(o.Rect.X+metrics.Left/p.data.PageWidth-.1) > 1e-6 || math.Abs(o.Rect.Y+p.data.Padding/p.data.PageHeight-.182) > 1e-6 {
		t.Fatal("committed text or insertion coordinates changed", data.Text, o.Rect)
	}
	if !g.document.Undo() || g.document.Dirty() || g.document.CanUndo() {
		t.Fatal("text insertion did not create exactly one Undo")
	}
	if !g.document.Redo() || len(g.document.Overlays()) != 1 {
		t.Fatal("text insertion Redo failed")
	}
}

func TestInlineTextCancellationAndEmptyDraftDoNotCreateObjects(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 250)})
	p := g.textPlacement
	p.entry.SetText("Discard me")
	p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if g.textPlacement != nil || g.document.Dirty() || g.document.CanUndo() || g.annotationTool != toolText {
		t.Fatal("Escape committed text or left Text mode")
	}
	layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 250)})
	g.textEntry.SetText("  ")
	g.textEntry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	finishAnnotationJobs(t, g)
	if len(g.document.Overlays()) != 0 || g.document.Dirty() {
		t.Fatal("an empty text box created an annotation")
	}
}

func TestSecondClickPlacesPreviousTextAndStartsFreshDraft(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 250)})
	first := g.textPlacement
	first.entry.SetText("First")
	layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(350, 300)})
	second := g.textPlacement
	if second == nil || second == first || second.entry.Text != "" || g.annotationPending < 1 {
		t.Fatal("second page click lost the draft or reused old text")
	}
	finishAnnotationJobs(t, g)
	if g.textPlacement != second || g.window.Canvas().Focused() != second.entry || len(g.document.Overlays()) != 1 {
		t.Fatal("pending creation displaced the new inline editor")
	}
	second.entry.SetText("Second")
	g.setAnnotationTool(toolDraw)
	finishAnnotationJobs(t, g)
	if g.annotationTool != toolDraw || g.textPlacement != nil || len(g.document.Overlays()) != 2 {
		t.Fatal("leaving Text did not commit the draft or a pending job overrode the new tool")
	}
}

func TestInlineTextPreviewMatchesCommittedPosition(t *testing.T) {
	for _, font := range annotations.Fonts {
		for _, edge := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/edge=%t", font, edge), func(t *testing.T) {
				g, layer := annotationTestGui(t)
				g.setAnnotationTool(toolText)
				position := fyne.NewPos(100, 250)
				if edge {
					position = fyne.NewPos(995, 645)
				}
				layer.input.Tapped(&fyne.PointEvent{Position: position})
				p := g.textPlacement
				p.data.Font = font
				p.data.Bold, p.data.Italic, p.data.Underline, p.data.Strikethrough = true, true, true, true
				p.data.Size = 24
				p.entry.SetText("AV café jg")
				finishAnnotationJobs(t, g)
				if !p.ready || p.preview.Hidden || p.preview.Image == nil {
					t.Fatal("missing live artwork preview")
				}
				if !edge && (math.Abs(p.rect.X*p.data.PageWidth+p.metrics.Left-p.point.X) > 1e-6 || math.Abs(p.rect.Y*p.data.PageHeight+p.data.Padding-p.point.Y) > 1e-6) {
					t.Fatal("ink origin does not follow the clicked point")
				}
				original := p.rect
				layer.Resize(fyne.NewSize(2000, 1600))
				layer.Refresh()
				finishAnnotationJobs(t, g)
				if p.rect != original {
					t.Fatal("zoom moved physical text bounds")
				}
				expectedPosition, expectedSize := layer.widgetRect(p.rect)
				if p.preview.Position() != expectedPosition || p.preview.Size() != expectedSize {
					t.Fatal("preview is not at its page coordinates")
				}
				if p.field.Theme.Color(theme.ColorNameInputBackground, theme.VariantDark) != (color.Transparent) || p.field.Theme.Size(theme.SizeNameInnerPadding) != 0 {
					t.Fatal("entry background or padding offsets the text")
				}
				if p.frame.Hidden || p.frame.StrokeWidth != 1 || p.frame.Size().Width < p.field.Size().Width {
					t.Fatal("missing lightweight editor frame")
				}
				p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
				finishAnnotationJobs(t, g)
				overlays := g.document.Overlays()
				if len(overlays) != 1 || overlays[0].Rect != original {
					t.Fatal("text jumped when confirmed", overlays)
				}
			})
		}
	}
}

func TestInlineTextPreviewCoalescesTyping(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 250)})
	p := g.textPlacement
	for i := 1; i <= 50; i++ {
		p.entry.SetText(strings.Repeat("x", i))
	}
	if g.annotationPending != 1 {
		t.Fatal("typing queued unbounded previews", g.annotationPending)
	}
	finishAnnotationJobs(t, g)
	if !p.ready || p.requested.Text != p.entry.Text || g.document.Dirty() {
		t.Fatal("preview did not catch up without changing history")
	}
}

func TestExistingTextEditsOnPageWithSingleUndo(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	placeTestText(t, g, layer, fyne.NewPos(100, 250), "Original")
	g.document.MarkSaved()
	g.setAnnotationTool(toolSelect)
	original := g.document.Overlays()[0]
	layer.widgets[0].DoubleTapped(&fyne.PointEvent{})
	p := g.textPlacement
	if p == nil || p.original == nil || p.original.ID != original.ID || g.window.Canvas().Focused() != p.entry {
		t.Fatal("double click did not open existing text on page")
	}
	finishAnnotationJobs(t, g)
	for _, object := range fyneTest.LaidOutObjects(g.annotationControls) {
		if _, ok := object.(*annotationEntry); ok {
			t.Fatal("text entry returned to property bar")
		}
	}
	if layer.widgets[0].Visible() {
		t.Fatal("original text remains underneath inline editor")
	}
	p.entry.SetText("Revised text")
	var sizes *compactSelect
	for _, object := range fyneTest.LaidOutObjects(g.annotationControls) {
		if selectBox, ok := object.(*compactSelect); ok && selectBox.Options[0] == "6" {
			sizes = selectBox
		}
	}
	if sizes == nil {
		t.Fatal("missing style controls")
	}
	g.window.Canvas().Focus(sizes)
	sizes.SetSelected("24")
	finishAnnotationJobs(t, g)
	if g.textPlacement != p || g.document.Dirty() {
		t.Fatal("changing style committed the draft")
	}
	previewRect := p.rect
	p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	finishAnnotationJobs(t, g)
	objects := g.document.Overlays()
	data, _ := objects[0].Annotation()
	if len(objects) != 1 || objects[0].ID != original.ID || data.Text != "Revised text" || data.Size != 24 || objects[0].Rect != previewRect || !layer.widgets[0].Visible() {
		t.Fatal("editing duplicated, moved or lost text")
	}
	if !g.document.Undo() || g.document.Dirty() {
		t.Fatal("editing did not produce one undo step")
	}
	data, _ = g.document.Overlays()[0].Annotation()
	if data.Text != "Original" {
		t.Fatal("undo did not restore original text")
	}
}

func TestExistingTextCancelDeleteAndSaveBarrier(t *testing.T) {
	for _, operation := range []string{"cancel", "delete", "save", "unchanged", "invalid"} {
		t.Run(operation, func(t *testing.T) {
			g, layer := annotationTestGui(t)
			g.setAnnotationTool(toolText)
			placeTestText(t, g, layer, fyne.NewPos(100, 250), "Original")
			g.document.MarkSaved()
			g.setAnnotationTool(toolSelect)
			layer.widgets[0].DoubleTapped(&fyne.PointEvent{})
			p := g.textPlacement
			switch operation {
			case "cancel":
				p.entry.SetText("Discarded")
				p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
			case "delete":
				p.entry.SetText("")
				p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
			case "save", "invalid":
				text := "Saved edit"
				if operation == "invalid" {
					text = strings.Repeat("x", 2001)
				}
				p.entry.SetText(text)
				saved := false
				g.waitAnnotationChanges(func() { saved = true })
				finishAnnotationJobs(t, g)
				if operation == "invalid" {
					if saved || g.textPlacement != p || p.entry.Text != text || g.document.Dirty() {
						t.Fatal("failed edit lost draft or continued save")
					}
					return
				}
				data, _ := g.document.Overlays()[0].Annotation()
				if !saved || data.Text != "Saved edit" {
					t.Fatal("save did not wait for edit")
				}
				return
			case "unchanged":
				p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
			}
			finishAnnotationJobs(t, g)
			if operation == "delete" {
				if len(g.document.Overlays()) != 0 || !g.document.Undo() || g.document.Dirty() {
					t.Fatal("empty edit did not delete in one undo step")
				}
				return
			}
			data, _ := g.document.Overlays()[0].Annotation()
			if data.Text != "Original" || g.document.Dirty() || g.textPlacement != nil || !layer.widgets[0].Visible() {
				t.Fatal("cancel/unchanged modified or hid original")
			}
		})
	}
}

func TestExistingPDFTextNeverUsesLayoutBoxToStretchGlyphs(t *testing.T) {
	g, layer := redactTestGui(t)
	g.setAnnotationTool(toolText)
	placeTestText(t, g, layer, fyne.NewPos(100, 100), "ciao")
	original := g.document.Overlays()[0]
	// A PDF text layout box can be arbitrarily wider/taller than its glyphs.
	wide := original.Rect
	wide.Width = .6
	wide.Height = .3
	data, _ := original.Annotation()
	prepared, err := original.Asset().Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if err = g.document.RemoveOverlay(original.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = g.document.AddAnnotation(0, wide, data, prepared); err != nil {
		t.Fatal(err)
	}
	raw, err := g.document.Export(document.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	source, err := document.Load("layout-box.pdf", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	g.document = reopened
	layer.SetDocument(reopened, 0, image.Pt(600, 400))
	layer.reload()
	g.setAnnotationTool(toolSelect)
	layer.widgets[0].DoubleTapped(&fyne.PointEvent{})
	p := g.textPlacement
	p.entry.SetText("ciao edited")
	finishAnnotationJobs(t, g)
	_, bounds, _, err := p.data.TextArtwork()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(p.rect.Width*p.data.PageWidth-bounds.W()) > .001 || math.Abs(p.rect.Height*p.data.PageHeight-bounds.H()) > .001 {
		t.Fatal("inline preview stretches text into old layout box")
	}
	p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	finishAnnotationJobs(t, g)
	current := g.document.Overlays()[0]
	data, _ = current.Annotation()
	// Style edits outside the inline editor must use the same natural dimensions.
	data.Color = color.NRGBA{R: 150, A: 255}
	g.selectSignature(layer, current.ID)
	g.updateAnnotationProperties(current, data, true)
	finishAnnotationJobs(t, g)
	current = g.document.Overlays()[0]
	if math.Abs(current.Rect.Width*p.data.PageWidth-bounds.W()) > .001 || math.Abs(current.Rect.Height*p.data.PageHeight-bounds.H()) > .001 {
		t.Fatal("property edit stretches PDF text")
	}
}

func TestInlineTextKeepsCaretAndBaselineWhileTyping(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolText)
	layer.input.Tapped(&fyne.PointEvent{Position: fyne.NewPos(100, 250)})
	p := g.textPlacement
	p.entry.SetText("abc")
	finishAnnotationJobs(t, g)
	if p.field.Theme.Size(theme.SizeNameInputBorder) <= 0 {
		t.Fatal("invisible caret")
	}
	position := p.field.Position()
	baseline := p.rect.Y*p.data.PageHeight + p.metrics.Baseline
	for _, text := range []string{"abcg", "abcgH", "abcgH café"} {
		p.entry.SetText(text)
		if !p.ready || p.preview.Hidden || p.field.Position() != position {
			t.Fatal("typing discarded visible preview or moved field before update")
		}
		finishAnnotationJobs(t, g)
		if math.Abs(p.rect.Y*p.data.PageHeight+p.metrics.Baseline-baseline) > .001 {
			t.Fatal("typing moved writing baseline")
		}
	}
	p.entry.SetText("")
	if p.ready || !p.preview.Hidden {
		t.Fatal("clearing entry left stale artwork")
	}

}
