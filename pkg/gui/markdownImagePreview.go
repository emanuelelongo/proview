package gui

import (
	"emanuelelongo/proview/internal/gifplay"
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// The preview shares decoded pixels with the document and owns only view state.
// It is a temporary canvas overlay, not a native window or a session entry.
type markdownImagePreview struct {
	animation     *gifplay.Player
	view          *markdownView
	overlay       *markdownPreviewOverlay
	previousFocus fyne.Focusable
	closed        bool
	picture       *canvas.Image
	scroll        *container.Scroll
	viewport      *markdownPreviewViewport
	scale         float32
	fitted        bool
}

func (v *markdownView) showImagePreview(picture image.Image) {
	if picture == nil || v.editor.closed || v.editor.markdownView != v {
		return
	}
	if v.imagePreview != nil {
		v.imagePreview.close()
	}
	p := v.editor.markdownPreviewCache
	if p == nil {
		p = v.newImagePreview()
		v.editor.markdownPreviewCache = p
	}
	p.view = v
	p.closed, p.scale, p.fitted = false, 1, true
	p.previousFocus = v.editor.window.Canvas().Focused()
	p.picture.Image = picture
	p.picture.Refresh()
	p.animation = nil
	for _, segment := range v.animatedImages {
		if segment.image == picture {
			p.animation = segment.player
			break
		}
	}
	v.imagePreview = p
	target := v.editor.window.Canvas()
	p.overlay.Resize(target.Size())
	target.Overlays().Add(p.overlay)
	target.Focus(p.overlay)
	p.fit()
}

// Reuse the canvas image identity across openings so native texture caches
// cannot accumulate a separate full-resolution texture for every click.
func (v *markdownView) newImagePreview() *markdownImagePreview {
	p := &markdownImagePreview{view: v, picture: canvas.NewImageFromImage(nil), scale: 1, fitted: true, closed: true}
	p.picture.FillMode = canvas.ImageFillContain
	p.picture.ScaleMode = canvas.ImageScaleFastest
	p.scroll = container.NewScroll(container.New(&markdownPreviewImageLayout{preview: p}, p.picture))
	p.viewport = &markdownPreviewViewport{preview: p}
	p.viewport.ExtendBaseWidget(p.viewport)
	interaction := &markdownPreviewInteraction{preview: p}
	interaction.ExtendBaseWidget(interaction)
	p.viewport.content = container.NewStack(p.scroll, interaction)
	p.overlay = &markdownPreviewOverlay{preview: p}
	p.overlay.ExtendBaseWidget(p.overlay)
	return p
}

func (p *markdownImagePreview) close() {
	if p.closed {
		return
	}
	p.closed = true
	if p.view.imagePreview == p {
		p.view.imagePreview = nil
	}
	target := p.view.editor.window.Canvas()
	wasFocused := target.Focused() == p.overlay
	// Drop the source reference and invalidate the same canvas image, including
	// when the overlay has already left the visible tree.
	p.animation = nil
	p.picture.Image = nil
	p.picture.Refresh()
	target.Overlays().Remove(p.overlay)
	if wasFocused && (target.Focused() == nil || target.Focused() == p.overlay) {
		target.Focus(p.previousFocus)
	}
	p.previousFocus = nil
	p.view = nil
}

// The full-canvas surface consumes the dismissing click so it cannot activate
// a document link or tool beneath it. Only the image viewport has interactions.
type markdownPreviewOverlay struct {
	widget.BaseWidget
	preview *markdownImagePreview
}

func (o *markdownPreviewOverlay) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(color.NRGBA{A: 190})
	return &markdownPreviewOverlayRenderer{overlay: o, background: background}
}
func (o *markdownPreviewOverlay) Tapped(event *fyne.PointEvent) {
	position := event.Position.Subtract(o.preview.viewport.Position())
	size := o.preview.viewport.Size()
	if position.X < 0 || position.Y < 0 || position.X >= size.Width || position.Y >= size.Height {
		o.preview.close()
	}
}
func (o *markdownPreviewOverlay) TappedSecondary(event *fyne.PointEvent) { o.Tapped(event) }
func (*markdownPreviewOverlay) FocusGained()                             {}
func (o *markdownPreviewOverlay) FocusLost()                             { o.preview.close() }
func (o *markdownPreviewOverlay) TypedKey(event *fyne.KeyEvent) {
	if event.Name == fyne.KeyEscape {
		o.preview.close()
	}
}
func (o *markdownPreviewOverlay) TypedRune(key rune) {
	switch key {
	case '+', '=':
		o.preview.zoom(1, nil)
	case '-':
		o.preview.zoom(-1, nil)
	case '0':
		o.preview.fit()
	case '1':
		o.preview.setScale(1, nil)
	}
}
func (o *markdownPreviewOverlay) TypedShortcut(shortcut fyne.Shortcut) {
	if key, ok := shortcut.(*desktop.CustomShortcut); ok {
		switch key.KeyName {
		case fyne.KeyEqual, fyne.KeyPlus:
			o.TypedRune('+')
		case fyne.KeyMinus:
			o.TypedRune('-')
		case fyne.Key0:
			o.TypedRune('0')
		case fyne.Key1:
			o.TypedRune('1')
		case fyne.KeyW:
			o.preview.close()
		}
	}
}

type markdownPreviewOverlayRenderer struct {
	overlay    *markdownPreviewOverlay
	background *canvas.Rectangle
}

func (r *markdownPreviewOverlayRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	viewport := r.overlay.preview.viewport
	fitted := fyne.NewSize(max(1, size.Width-48), max(1, size.Height-48))
	viewport.Resize(fitted)
	viewport.Move(fyne.NewPos((size.Width-fitted.Width)/2, (size.Height-fitted.Height)/2))
}
func (*markdownPreviewOverlayRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (r *markdownPreviewOverlayRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.background, r.overlay.preview.viewport}
}
func (*markdownPreviewOverlayRenderer) Destroy() {}
func (*markdownPreviewOverlayRenderer) Refresh() {}

func (p *markdownImagePreview) imageSize() fyne.Size {
	if p.picture.Image == nil {
		return fyne.Size{}
	}
	bounds := p.picture.Image.Bounds().Size()
	return fyne.NewSize(float32(bounds.X)*p.scale, float32(bounds.Y)*p.scale)
}
func (p *markdownImagePreview) fitScale() float32 {
	if p.picture.Image == nil {
		return 1
	}
	bounds := p.picture.Image.Bounds().Size()
	size := p.viewport.Size()
	return min(max(1, size.Width)/float32(bounds.X), max(1, size.Height)/float32(bounds.Y))
}
func (p *markdownImagePreview) fit() {
	p.setScale(p.fitScale(), nil)
	p.fitted = true
}
func (p *markdownImagePreview) zoom(direction int, anchor *fyne.Position) {
	p.setScale(p.scale*float32(math.Pow(1.25, float64(direction))), anchor)
}
func (p *markdownImagePreview) setScale(scale float32, anchor *fyne.Position) {
	if p.closed {
		return
	}
	size := p.viewport.Size()
	point := fyne.NewPos(size.Width/2, size.Height/2)
	if anchor != nil {
		point = *anchor
	}
	oldSize := p.imageSize()
	origin := fyne.NewPos(max(0, (size.Width-oldSize.Width)/2), max(0, (size.Height-oldSize.Height)/2))
	source := point.Add(p.scroll.Offset).Subtract(origin)
	source.X /= p.scale
	source.Y /= p.scale
	p.scale = max(min(p.fitScale()/4, .05), min(max(p.fitScale(), 8), scale))
	p.fitted = false
	p.scroll.Content.Refresh()
	p.scroll.Refresh()
	next := p.imageSize()
	offset := fyne.NewPos(source.X*p.scale-point.X+max(0, (size.Width-next.Width)/2), source.Y*p.scale-point.Y+max(0, (size.Height-next.Height)/2))
	p.scroll.ScrollToOffset(fyne.NewPos(max(0, offset.X), max(0, offset.Y)))
}

type markdownPreviewImageLayout struct{ preview *markdownImagePreview }

func (l *markdownPreviewImageLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return l.preview.imageSize()
}
func (l *markdownPreviewImageLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	imageSize := l.preview.imageSize()
	objects[0].Resize(imageSize)
	objects[0].Move(fyne.NewPos(max(0, (size.Width-imageSize.Width)/2), max(0, (size.Height-imageSize.Height)/2)))
}

type markdownPreviewViewport struct {
	widget.BaseWidget
	preview *markdownImagePreview
	content fyne.CanvasObject
}

func (v *markdownPreviewViewport) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(v.content)
}
func (v *markdownPreviewViewport) MinSize() fyne.Size { return fyne.NewSize(160, 120) }
func (v *markdownPreviewViewport) Resize(size fyne.Size) {
	oldSize := v.Size()
	if v.preview.closed || size == oldSize || size.Width <= 0 || size.Height <= 0 {
		return
	}
	p := v.preview
	// Capture the viewed point before Scroll.Resize clamps the old offset.
	imageSize := p.imageSize()
	center := fyne.NewPos(oldSize.Width/2, oldSize.Height/2).Add(p.scroll.Offset)
	center = center.Subtract(fyne.NewPos(max(0, (oldSize.Width-imageSize.Width)/2), max(0, (oldSize.Height-imageSize.Height)/2)))
	if p.fitted {
		bounds := p.picture.Image.Bounds().Size()
		// Publish the new fit before laying out children, avoiding a layout at
		// the previous scale followed by a second refresh/zoom cycle.
		p.scale = min(size.Width/float32(bounds.X), size.Height/float32(bounds.Y))
	}
	v.BaseWidget.Resize(size)
	if p.fitted {
		p.scroll.ScrollToOffset(fyne.Position{})
	} else if oldSize.Width > 0 && oldSize.Height > 0 {
		origin := fyne.NewPos(max(0, (size.Width-imageSize.Width)/2), max(0, (size.Height-imageSize.Height)/2))
		p.scroll.ScrollToOffset(center.Add(origin).Subtract(fyne.NewPos(size.Width/2, size.Height/2)))
	}
}

type markdownPreviewInteraction struct {
	widget.BaseWidget
	preview *markdownImagePreview
}

func (s *markdownPreviewInteraction) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}
func (s *markdownPreviewInteraction) Scrolled(event *fyne.ScrollEvent) {
	var modifiers fyne.KeyModifier
	if driver, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		modifiers = driver.CurrentKeyModifiers()
	}
	s.scrollWithModifiers(event, modifiers)
}
func (s *markdownPreviewInteraction) scrollWithModifiers(event *fyne.ScrollEvent, modifiers fyne.KeyModifier) {
	if modifiers&fyne.KeyModifierShortcutDefault != 0 {
		if event.Scrolled.DY > 0 {
			s.preview.zoom(1, &event.Position)
		} else if event.Scrolled.DY < 0 {
			s.preview.zoom(-1, &event.Position)
		}
	} else {
		s.preview.scroll.Scrolled(event)
	}
}
func (s *markdownPreviewInteraction) Dragged(event *fyne.DragEvent) {
	s.preview.scroll.ScrollToOffset(s.preview.scroll.Offset.Subtract(fyne.NewPos(event.Dragged.DX, event.Dragged.DY)))
}
func (*markdownPreviewInteraction) DragEnd() {}

func (s *markdownPreviewInteraction) Tapped(event *fyne.PointEvent) {
	p := s.preview
	size, viewport := p.imageSize(), p.viewport.Size()
	origin := fyne.NewPos(max(0, (viewport.Width-size.Width)/2), max(0, (viewport.Height-size.Height)/2)).Subtract(p.scroll.Offset)
	if event.Position.X < origin.X || event.Position.Y < origin.Y || event.Position.X >= origin.X+size.Width || event.Position.Y >= origin.Y+size.Height {
		p.close()
	}
}
