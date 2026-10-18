package gui

import (
	"image"
	"testing"

	"emanuelelongo/proview/pkg/annotations"
)

func TestAddingHighlightKeepsCompletedPreviewsVisible(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.addDocument(testDocument(t, 1000, 500), nil, nil)
	layer := g.imageSignatureLayer
	data := annotations.Default(annotations.Highlight)
	data.PageWidth, data.PageHeight = 1000, 500
	data.Quads = []annotations.Quad{{{X: 100, Y: 100}, {X: 300, Y: 100}, {X: 300, Y: 125}, {X: 100, Y: 125}}}
	g.addPageAnnotation(layer, data, 0, 0, nil)
	for len(layer.widgets) == 0 || !layer.widgets[0].image.Visible() {
		driver.runNext(t)
	}
	first := layer.widgets[0]
	preview := first.preview
	if layer.highlightPreviews[first.id] != preview {
		t.Fatal("completed highlight blend was not retained")
	}
	data.Quads = []annotations.Quad{{{X: 100, Y: 200}, {X: 300, Y: 200}, {X: 300, Y: 225}, {X: 100, Y: 225}}}
	g.addPageAnnotation(layer, data, 0, 0, nil)
	for len(g.document.Overlays()) < 2 || g.annotationPending > 0 {
		driver.runNext(t)
	}
	if len(layer.widgets) != 2 || !layer.widgets[0].image.Visible() || layer.widgets[0].preview != preview {
		t.Fatal("adding highlight hid or replaced existing preview before rendering")
	}
	if layer.widgets[1].image.Visible() {
		t.Fatal("new highlight displayed an unblended opaque mask")
	}
	// Further rebuilds also retain the last visible frame while work is pending.
	layer.reload()
	if !layer.widgets[0].image.Visible() || layer.widgets[0].preview != preview {
		t.Fatal("repeated rebuild lost completed preview")
	}
	if err := g.document.RemoveOverlay(first.id); err != nil {
		t.Fatal(err)
	}
	layer.reload()
	if layer.highlightPreviews[first.id] != nil {
		t.Fatal("removed highlight retained its image")
	}
	// Overlay IDs may be reused by another document; fragments must not follow them.
	other := testDocument(t, 1000, 500)
	defer other.Close()
	layer.SetDocument(other, 0, image.Pt(1000, 500))
	if len(layer.highlightPreviews) != 0 {
		t.Fatal("highlight preview leaked across documents")
	}
	g.closeWindow()
}
