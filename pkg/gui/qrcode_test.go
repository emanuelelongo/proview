package gui

import (
	"context"
	"image/color"
	"testing"

	"emanuelelongo/proview/internal/textselect"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestQRCodeSelectionStatusCopyAndURL(t *testing.T) {
	g, l := annotationTestGui(t)
	g.window = textClipboardWindow{g.window, fyneTest.NewClipboard()}
	payload := "https://example.com/path?q=value,"
	layout := textselect.Layout{Width: 1000, Height: 500, Words: []textselect.Word{
		{Text: payload, Quad: textselect.Rect(.1, .1, .3, .3)},
		{Text: "Other QR", Quad: textselect.Rect(.7, .7, .9, .9)},
	}}
	g.qr = &qrState{doc: g.document, cache: map[string]*ocrEntry{g.document.TextSelectionKey(0): {layout: layout}}}
	app := &selectionURLApp{App: g.app}
	g.app = app
	g.tipText = canvas.NewText("", color.Black)
	l.MouseIn(mouseAt(150, 225))
	if l.Cursor() != desktop.PointerCursor {
		t.Fatal("QR cursor was not clickable")
	}
	l.MouseDown(mouseAt(150, 225))
	l.MouseUp(mouseAt(850, 575))
	if !g.validTextSelection() || !g.textSelection.qrCode || g.textSelection.start != g.textSelection.end || g.tipText.Text != payload {
		t.Fatal("QR did not remain selected as a single payload")
	}
	if len(app.opened) != 0 {
		t.Fatal("page click opened URL")
	}
	g.copyArea()
	if g.window.Clipboard().Content() != payload {
		t.Fatal("QR payload was not copied verbatim")
	}
	surface := newStatusTextSurface(g)
	surface.Tapped(&fyne.PointEvent{})
	if len(app.opened) != 1 || app.opened[0].String() != payload {
		t.Fatal("status URL lost payload punctuation")
	}
	l.Tapped(&fyne.PointEvent{Position: fyne.NewPos(850, 575)})
	if !g.validTextSelection() || g.tipText.Text != "Other QR" {
		t.Fatal("tap did not select QR")
	}
	surface.Tapped(&fyne.PointEvent{})
	if len(app.opened) != 1 {
		t.Fatal("plain payload opened a URL")
	}
	if err := g.document.TransformImage("right"); err != nil {
		t.Fatal(err)
	}
	if g.validTextSelection() || l.qrLayout() != nil || g.selectedTextURL() != nil {
		t.Fatal("source changes retained stale QR")
	}
}

func TestQRCodeCacheAndObsoleteCompletion(t *testing.T) {
	t.Setenv("PROVIEW_OCR", "0")
	g, _ := annotationTestGui(t)
	key := g.document.TextSelectionKey(0)
	layout := textselect.Layout{Words: []textselect.Word{{Text: "payload"}}}
	g.qr = &qrState{doc: g.document, cache: map[string]*ocrEntry{key: {layout: layout}}}
	g.syncOCR()
	if g.ocr != nil {
		t.Fatal("QR recognition initialized the disabled OCR engine")
	}
	if g.qr.running != nil || g.working || len(g.qr.requested) != 1 {
		t.Fatal("cached QR scan blocked the editor or scheduled new work")
	}
	g.qr.requested = nil
	for i := 0; i < 20; i++ {
		g.qr.cache[string(rune('a'+i))] = &ocrEntry{used: uint64(i + 1)}
	}
	g.qr.clock = 30
	job := &ocrJob{doc: g.document, page: 0, key: key, cancel: func() {}}
	g.qr.running = job
	g.completeQRCodes(job, layout, nil, false)
	if len(g.qr.cache) != 20 || g.qr.cache["a"] != nil {
		t.Fatal("QR cache exceeded its limit or evicted the wrong entry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	job = &ocrJob{doc: g.document, page: 0, key: key, cancel: cancel}
	g.qr.running = job
	g.closed = true
	g.stopOCR()
	if ctx.Err() != context.Canceled {
		t.Fatal("closing editor did not cancel QR recognition")
	}
	g.completeQRCodes(job, layout, nil, false)
	if len(g.qr.cache) != 0 {
		t.Fatal("late QR result repopulated closed editor")
	}
}
