package gui

import (
	"image"

	"fyne.io/fyne/v2"
	"golang.org/x/image/draw"
)

// Retain one bounded snapshot of the reading viewport, without moving the
// reader or creating a second Markdown renderer just for the sidebar.
func (v *markdownView) scheduleThumbnail() {
	if v.thumbnailPending || v.editor.activeDocument == nil || v.editor.activeDocument.thumbnail != nil {
		return
	}
	v.thumbnailPending = true
	fyne.Do(func() {
		defer func() { v.thumbnailPending = false }()
		if v.captureThumbnail() {
			v.editor.refreshDocumentSidebar()
		}
	})
}

func (v *markdownView) captureThumbnail() bool {
	g := v.editor
	entry := g.activeDocument
	if v.thumbnailCapturing || g.closed || g.markdownView != v || entry == nil || entry.markdown != v.doc || entry.thumbnail != nil || !g.markdownPanel.Visible() || !v.scroll.Visible() {
		return false
	}
	if v.crop != nil && v.crop.active {
		return false
	}
	if g.window.Canvas().Overlays().Top() != nil || v.scroll.Size().Width < 80 || v.scroll.Size().Height < 80 {
		return false
	}
	for _, diagram := range v.diagrams {
		if !diagram.hasRequest || diagram.running {
			return false
		}
	}
	v.thumbnailCapturing = true
	defer func() { v.thumbnailCapturing = false }()
	position := g.app.Driver().AbsolutePositionForObject(v.scroll)
	frame, err := markdownViewportSnapshot(g.window.Canvas().Capture(), g.window.Canvas().Size(), position, v.scroll.Size())
	if err != nil {
		return false
	}
	bounds := frame.Bounds()
	scale := min(1., 320./float64(bounds.Dx()), 240./float64(bounds.Dy()))
	thumbnail := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale))))
	draw.ApproxBiLinear.Scale(thumbnail, thumbnail.Bounds(), frame, bounds, draw.Src, nil)
	entry.thumbnail = thumbnail
	return true
}
