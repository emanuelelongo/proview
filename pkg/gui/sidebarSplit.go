package gui

import (
	"emanuelelongo/proview/pkg/document"
	"image"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	minSidebarWidth     float32 = 140
	maxSidebarWidth     float32 = 360
	sidebarDividerWidth float32 = 6
	defaultSidebarWidth float32 = minSidebarWidth
)

type fixedSidebarSplit struct {
	widget.BaseWidget
	leading       fyne.CanvasObject
	trailing      fyne.CanvasObject
	width         float32
	onWidthChange func()
}

func newFixedSidebarSplit(leading, trailing fyne.CanvasObject, width float32, onWidthChange func()) *fixedSidebarSplit {
	split := &fixedSidebarSplit{leading: leading, trailing: trailing, onWidthChange: onWidthChange}
	split.SetSidebarWidth(width)
	split.ExtendBaseWidget(split)
	return split
}

func (s *fixedSidebarSplit) CreateRenderer() fyne.WidgetRenderer {
	divider := newSidebarDivider(func(delta float32) {
		s.SetSidebarWidth(s.width + delta)
		s.Refresh()
		if s.onWidthChange != nil {
			s.onWidthChange()
		}
	})
	return &fixedSidebarSplitRenderer{split: s, divider: divider}
}

func (s *fixedSidebarSplit) SetSidebarWidth(width float32) {
	s.width = fyne.Max(minSidebarWidth, fyne.Min(maxSidebarWidth, width))
}

func (r *fixedSidebarSplitRenderer) Layout(size fyne.Size) {
	s := r.split
	if !s.leading.Visible() {
		r.divider.Hide()
		s.trailing.Move(fyne.NewPos(0, 0))
		s.trailing.Resize(size)
		return
	}
	if !s.trailing.Visible() {
		r.divider.Hide()
		s.leading.Move(fyne.NewPos(0, 0))
		s.leading.Resize(size)
		return
	}

	width := s.width
	if width+sidebarDividerWidth > size.Width {
		width = fyne.Max(0, size.Width-sidebarDividerWidth)
	}
	r.divider.Show()
	s.leading.Move(fyne.NewPos(0, 0))
	s.leading.Resize(fyne.NewSize(width, size.Height))
	r.divider.Move(fyne.NewPos(width, 0))
	r.divider.Resize(fyne.NewSize(sidebarDividerWidth, size.Height))
	s.trailing.Move(fyne.NewPos(width+sidebarDividerWidth, 0))
	s.trailing.Resize(fyne.NewSize(fyne.Max(0, size.Width-width-sidebarDividerWidth), size.Height))
}

func (r *fixedSidebarSplitRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (r *fixedSidebarSplitRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.split.leading, r.divider, r.split.trailing}
}
func (r *fixedSidebarSplitRenderer) Destroy() {}
func (r *fixedSidebarSplitRenderer) Refresh() {
	r.Layout(r.split.Size())
	r.divider.Refresh()
	r.split.leading.Refresh()
	r.split.trailing.Refresh()
	canvas.Refresh(r.split)
}

type fixedSidebarSplitRenderer struct {
	split   *fixedSidebarSplit
	divider *sidebarDivider
}

type sidebarDivider struct {
	widget.BaseWidget
	background *canvas.Rectangle
	dragging   bool
	onDrag     func(float32)
}

func newSidebarDivider(onDrag func(float32)) *sidebarDivider {
	divider := &sidebarDivider{background: canvas.NewRectangle(theme.SeparatorColor()), onDrag: onDrag}
	divider.ExtendBaseWidget(divider)
	return divider
}

func (d *sidebarDivider) CreateRenderer() fyne.WidgetRenderer {
	return &sidebarDividerRenderer{divider: d}
}
func (d *sidebarDivider) MouseIn(*desktop.MouseEvent) {}
func (d *sidebarDivider) MouseOut()                   {}
func (d *sidebarDivider) Cursor() desktop.Cursor      { return desktop.HResizeCursor }
func (d *sidebarDivider) MouseDown(event *desktop.MouseEvent) {
	d.dragging = event.Button == desktop.MouseButtonPrimary
}
func (d *sidebarDivider) MouseUp(*desktop.MouseEvent) { d.dragging = false }
func (d *sidebarDivider) Dragged(event *fyne.DragEvent) {
	if d.dragging && d.onDrag != nil {
		d.onDrag(event.Dragged.DX)
	}
}
func (d *sidebarDivider) DragEnd() { d.dragging = false }

type sidebarDividerRenderer struct{ divider *sidebarDivider }

func (r *sidebarDividerRenderer) Layout(size fyne.Size) {
	r.divider.background.Resize(size)
}
func (r *sidebarDividerRenderer) MinSize() fyne.Size { return fyne.NewSize(sidebarDividerWidth, 0) }
func (r *sidebarDividerRenderer) Refresh() {
	r.divider.background.FillColor = theme.SeparatorColor()
	r.divider.background.Refresh()
}
func (r *sidebarDividerRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.divider.background}
}
func (*sidebarDividerRenderer) Destroy() {}

func thumbnailSizeForSidebar(pageSize image.Point, sidebarWidth float32) fyne.Size {
	width := fyne.Max(1, sidebarWidth-2*theme.Padding())
	if pageSize.X <= 0 || pageSize.Y <= 0 {
		return fyne.NewSize(width, width*1.4)
	}
	height := width * float32(pageSize.Y) / float32(pageSize.X)
	return fyne.NewSize(width, height)
}

func (g *Gui) updatePDFThumbnailSizes() {
	if g.document == nil || g.document.Format() != document.FormatPDF || g.sidebarSplit == nil {
		return
	}
	for page, thumbnail := range g.pdfThumbImages {
		if page >= len(g.pdfPageSizes) {
			break
		}
		thumbnail.SetMinSize(thumbnailSizeForSidebar(g.pdfPageSizes[page], g.sidebarSplit.width))
		thumbnail.Refresh()
	}
	g.thumbList.Refresh()
}
