package gui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"

	"emanuelelongo/proview/internal/textselect"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

type textClipboardWindow struct {
	fyne.Window
	clipboard fyne.Clipboard
}

func (w textClipboardWindow) Clipboard() fyne.Clipboard { return w.clipboard }
func installTestText(g *Gui) textselect.Layout {
	g.window = textClipboardWindow{g.window, fyneTest.NewClipboard()}
	layout := textselect.Layout{Width: 1000, Height: 500, Words: []textselect.Word{
		{Text: "Caffè", Quad: textselect.Rect(.1, .1, .25, .2)},
		{Text: "pronto", Quad: textselect.Rect(.3, .1, .45, .2)},
		{Text: "Second", Line: 1, Quad: textselect.Rect(.1, .3, .25, .4)},
	}}
	g.ocr = &ocrState{doc: g.document, cache: map[string]*ocrEntry{g.document.TextSelectionKey(0): {layout: layout}}}
	return layout
}
func TestTextSelectionGesturesAndClipboard(t *testing.T) {
	g, l := annotationTestGui(t)
	installTestText(g)
	// The contained page occupies y=150..650 in this 1000x800 layer.
	l.MouseMoved(mouseAt(150, 225))
	if l.Cursor() != desktop.TextCursor {
		t.Fatal("text cursor missing")
	}
	l.MouseDown(mouseAt(150, 225))
	l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(180, 325)}})
	l.MouseUp(mouseAt(180, 325))
	if g.textSelection == nil || g.textSelection.active || g.areaSelection != nil || g.document.Dirty() {
		t.Fatal("invalid text gesture state")
	}
	g.copyArea()
	if got := g.window.Clipboard().Content(); got != "Caffè pronto\nSecond" {
		t.Fatalf("copied %q", got)
	}
	before := l.textHighlight.Size()
	l.Resize(fyne.NewSize(2000, 1600))
	l.CreateRenderer().Layout(l.Size())
	if l.textHighlight.Size().Width != before.Width*2 {
		t.Fatal("highlight did not track zoom")
	}
	if !g.cancelCurrentOperation() || g.textSelection != nil || l.textHighlight.Visible() {
		t.Fatal("Escape did not clear selection")
	}
	l.Resize(fyne.NewSize(1000, 800))
	l.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 225)})
	g.copyArea()
	if g.window.Clipboard().Content() != "Caffè" {
		t.Fatal("double click did not select word")
	}
	l.MouseDown(mouseAt(700, 500))
	l.MouseUp(mouseAt(700, 500))
	if g.textSelection != nil {
		t.Fatal("background click did not clear text")
	}
}
func TestTextSelectionAltAndLateOCR(t *testing.T) {
	g, l := annotationTestGui(t)
	layout := installTestText(g)
	e := mouseAt(150, 225)
	e.Modifier = fyne.KeyModifierAlt
	l.MouseDown(e)
	l.MouseUp(mouseAt(450, 300))
	if g.areaSelection == nil || g.textSelection != nil {
		t.Fatal("Alt did not force rectangle")
	}
	g.clearAreaSelection()
	g.ocr.cache = map[string]*ocrEntry{}
	l.MouseDown(mouseAt(150, 225))
	g.ocr.cache[g.document.TextSelectionKey(0)] = &ocrEntry{layout: layout}
	l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(400, 300)}})
	l.MouseUp(mouseAt(400, 300))
	if g.areaSelection == nil || g.textSelection != nil {
		t.Fatal("late OCR changed a running gesture")
	}
	l.MouseDown(mouseAt(150, 225))
	l.MouseUp(mouseAt(150, 225))
	g.setAnnotationTool(toolDraw)
	if g.textSelection != nil {
		t.Fatal("tool change retained text selection")
	}
}
func TestTextSelectionDoesNotOverrideEntryAndRejectsStaleContent(t *testing.T) {
	g, l := annotationTestGui(t)
	installTestText(g)
	l.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 225)})
	entry := widget.NewEntry()
	entry.SetText("editing")
	g.window.SetContent(entry)
	g.window.Canvas().Focus(entry)
	g.window.Clipboard().SetContent("unchanged")
	g.copyArea()
	if g.window.Clipboard().Content() == "Caffè" {
		t.Fatal("page copy intercepted text entry")
	}
	g.window.Canvas().Focus(nil)
	if err := g.document.TransformImage("right"); err != nil {
		t.Fatal(err)
	}
	g.window.Clipboard().SetContent("sentinel")
	g.copyArea()
	if g.window.Clipboard().Content() != "sentinel" {
		t.Fatal("copied obsolete geometry")
	}
}
func TestOCRCompletionRejectsReplacedClosedAndChangedDocuments(t *testing.T) {
	for _, state := range []string{"changed", "replaced", "closed", "canceled"} {
		t.Run(state, func(t *testing.T) {
			g, _ := annotationTestGui(t)
			layout := installTestText(g)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			job := &ocrJob{doc: g.document, page: 0, key: g.document.TextSelectionKey(0), cancel: cancel}
			g.ocr.running = job
			switch state {
			case "changed":
				g.document.TransformImage("right")
				g.ocr.cache[g.document.TextSelectionKey(0)] = &ocrEntry{}
			case "replaced":
				g.stopOCR()
				g.document = testDocument(t, 100, 100)
				g.closed = true
			case "closed":
				g.closed = true
				g.stopOCR()
				if ctx.Err() == nil {
					t.Fatal("close did not cancel job")
				}
			case "canceled":
				cancel()
			}
			g.completeOCR(job, layout, nil, state == "canceled", time.Millisecond)
			if state != "canceled" && g.ocr.cache[job.key] != nil {
				t.Fatal("accepted stale OCR")
			}
		})
	}
}
func TestOCRLRUEvictionAndCacheReuse(t *testing.T) {
	g, _ := annotationTestGui(t)
	layout := installTestText(g)
	key := g.document.TextSelectionKey(0)
	entry := g.ocr.cache[key]
	g.syncOCR()
	if g.ocr.running != nil || g.ocr.cache[key] != entry || g.working {
		t.Fatal("cached view scheduled OCR or blocked editor")
	}
	g.ocr.requested = nil
	for i := 0; i < 20; i++ {
		g.ocr.cache[string(rune('a'+i))] = &ocrEntry{used: uint64(i + 1)}
	}
	g.ocr.clock = 30
	job := &ocrJob{doc: g.document, page: 0, key: key, cancel: func() {}}
	g.ocr.running = job
	g.completeOCR(job, layout, nil, false, time.Millisecond)
	if len(g.ocr.cache) != 20 || g.ocr.cache["a"] != nil || g.ocr.cache[key] == nil {
		t.Fatal("LRU cache limit failed")
	}
}

// Verify drawing the actual selection mask does not paint unrelated page areas.
func TestTextHighlightIsLocalToSelectedWords(t *testing.T) {
	g, l := annotationTestGui(t)
	installTestText(g)
	l.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 225)})
	rendered := l.textHighlight.Generator(1000, 500)
	_, _, _, inside := rendered.At(150, 75).RGBA()
	_, _, _, outside := rendered.At(500, 250).RGBA()
	if inside == 0 || outside != 0 || rendered.Bounds() != image.Rect(0, 0, 1000, 500) {
		t.Fatal("invalid highlight mask")
	}
}

func TestSelectionStatusPausesTipsAndPreservesPreference(t *testing.T) {
	for _, tips := range []bool{true, false} {
		t.Run(fmt.Sprintf("tips=%v", tips), func(t *testing.T) {
			g, l := annotationTestGui(t)
			installTestText(g)
			g.tipText = canvas.NewText("", color.Black)
			g.tipBar = container.NewVBox(g.tipText)
			g.tipsEnabled = tips
			g.app.Preferences().SetBool(tipsPreferenceKey, tips)
			g.startTipSequence()
			oldGeneration := g.tipGeneration
			defer g.stopTipSequence()
			l.MouseDown(mouseAt(150, 225))
			l.MouseUp(mouseAt(180, 325))
			if g.tipText.Text != "Caffè pronto Second" || g.tipTimer != nil {
				t.Fatal("selection did not replace tips with bare text")
			}
			g.fadeTip(oldGeneration, 1, time.Now())
			g.showNextTip(g.tipGeneration, false)
			g.finishCurrentTip(g.tipGeneration, true)
			if g.tipText.Text != "Caffè pronto Second" || g.tipTimer != nil {
				t.Fatal("tip callback overwrote selection")
			}
			if g.tipsEnabled != tips || g.app.Preferences().Bool(tipsPreferenceKey) != tips {
				t.Fatal("selection changed tips preference")
			}
			g.clearTextSelection()
			if g.tipText.Text != "" || (g.tipTimer != nil) != tips {
				t.Fatal("deselection did not restore tip behavior")
			}
			// Explicit preference changes during selection must preserve the selected text.
			l.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 225)})
			g.setTipsEnabled(false)
			g.setTipsEnabled(true)
			if g.tipText.Text != "Caffè" || g.tipTimer != nil {
				t.Fatal("toggling tips interrupted selection")
			}
			g.clearTextSelection()
			if g.tipTimer == nil {
				t.Fatal("tips did not resume after being enabled during selection")
			}
		})
	}
}
