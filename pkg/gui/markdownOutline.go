package gui

import (
	"image/color"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	markdownOutlineWidth     float32 = 220
	markdownOutlineRailWidth float32 = 45
)

type markdownHeading struct {
	title string
	level int
	text  *markdownText
}

// Keep the native Scroll in the renderer tree, and wrap only its navigation
// methods. Fyne's ScrollToOffset does not emit OnScrolled for ordinary jumps.
type markdownReaderScroll struct {
	*container.Scroll
	view *markdownView
}

func (s *markdownReaderScroll) ScrollToOffset(offset fyne.Position) {
	s.Scroll.ScrollToOffset(offset)
	s.view.updateOutlinePosition()
}
func (s *markdownReaderScroll) ScrollToTop() { s.ScrollToOffset(fyne.Position{}) }
func (s *markdownReaderScroll) ScrollToBottom() {
	s.Scroll.ScrollToBottom()
	s.view.updateOutlinePosition()
}

// This wrapper bounds the document's minimum size independently of its full
// reading layout. Markdown no longer owns a crop overlay or canvas capture.
type markdownViewport struct {
	widget.BaseWidget
	view *markdownView
}

func newMarkdownViewport(view *markdownView) *markdownViewport {
	v := &markdownViewport{view: view}
	v.ExtendBaseWidget(v)
	view.viewport = v
	return v
}
func (v *markdownViewport) CreateRenderer() fyne.WidgetRenderer {
	return &markdownViewportRenderer{viewport: v}
}

type markdownViewportRenderer struct{ viewport *markdownViewport }

func (*markdownViewportRenderer) Destroy()             {}
func (r *markdownViewportRenderer) MinSize() fyne.Size { return r.viewport.MinSize() }
func (r *markdownViewportRenderer) Objects() []fyne.CanvasObject {
	if r.viewport.view.outlinePanel == nil {
		return []fyne.CanvasObject{r.viewport.view.scroll.Scroll}
	}
	return []fyne.CanvasObject{r.viewport.view.scroll.Scroll, r.viewport.view.outlinePanel}
}
func (r *markdownViewportRenderer) Refresh() { canvas.Refresh(r.viewport) }
func (r *markdownViewportRenderer) Layout(size fyne.Size) {
	r.viewport.view.scroll.Move(fyne.Position{})
	r.viewport.view.scroll.Resize(size)
	r.viewport.view.layoutOutline(size)
}

func (v *markdownView) layoutOutline(size fyne.Size) {
	if v.outlinePanel == nil {
		return
	}
	const margin float32 = 12
	width := min(markdownOutlineWidth, max(1, size.Width-2*margin))
	height := min(float32(480), max(1, size.Height-2*margin))
	if len(v.headings) > 0 {
		height = min(height, 52+float32(len(v.headings))*40)
	} else {
		height = min(height, 100)
	}
	v.outlinePanel.Resize(fyne.NewSize(width, height))
	v.outlinePanel.Move(fyne.NewPos(max(0, size.Width-width-margin), margin))
}
func (*markdownViewport) MinSize() fyne.Size { return fyne.NewSize(180, 0) }
func (v *markdownViewport) Resize(size fyne.Size) {
	v.BaseWidget.Resize(size)
	v.view.updateTextViewport()
}

func (g *Gui) restoreToolPanel() {
	if g.rightPanel == nil {
		return
	}
	g.toolPropertyPanel.Show()
	g.rightPanel.Objects = []fyne.CanvasObject{g.toolPropertyPanel}
	g.rightPanel.Refresh()
}

func (v *markdownView) buildOutline() {
	g := v.editor
	if g == nil || g.rightPanel == nil {
		return
	}
	v.outline = widget.NewList(func() int { return len(v.headings) }, func() fyne.CanvasObject {
		r := &markdownOutlineRow{view: v, label: widget.NewLabel(""), marker: canvas.NewRectangle(theme.PrimaryColor())}
		r.label.Truncation = fyne.TextTruncateEllipsis
		r.ExtendBaseWidget(r)
		return r
	}, func(id widget.ListItemID, object fyne.CanvasObject) {
		row := object.(*markdownOutlineRow)
		row.index = id
		row.label.SetText(v.headings[id].title)
		row.Refresh()
	})
	v.outline.HideSeparators = true
	// Row taps also work on the already-current heading, unlike List.Select.
	v.outline.OnSelected = func(id widget.ListItemID) { v.navigateHeading(id); v.outline.UnselectAll() }
	v.outlineTitle = container.NewPadded(widget.NewLabelWithStyle("Contents", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	v.outlineToggle = newToggleToolbarAction(tablerIcon("list-tree"), v.toggleOutline)
	button := container.NewPadded(container.NewGridWrap(fyne.NewSize(36, 36), v.outlineToggle.button))
	var content fyne.CanvasObject = v.outline
	if len(v.headings) == 0 {
		label := widget.NewLabel("No headings")
		label.Wrapping = fyne.TextWrapWord
		content = container.NewPadded(label)
	}
	v.outlineContent = content
	v.outlinePanel = container.New(&markdownOutlineOverlayLayout{}, newMarkdownOutlineSurface(), container.NewBorder(v.outlineTitle, nil, nil, nil, content))
	v.applyOutlineVisibility()
	rail := container.New(&markdownOutlineLayout{}, container.NewBorder(button, nil, widget.NewSeparator(), nil, canvas.NewRectangle(color.Transparent)))
	g.toolPropertyPanel.Hide()
	g.rightPanel.Objects = []fyne.CanvasObject{rail}
	g.rightPanel.Refresh()
	if v.viewport != nil {
		v.layoutOutline(v.viewport.Size())
		canvas.Refresh(v.viewport)
	}
	v.updateOutlinePosition()
}

func (v *markdownView) applyOutlineVisibility() {
	expanded := v.editor.markdownOutlineExpanded
	v.outlineToggle.SetActive(expanded)
	if expanded {
		v.outlineToggle.button.tooltip = "Hide contents"
		v.outlineTitle.Show()
		v.outlineContent.Show()
		v.outlinePanel.Show()
	} else {
		v.outlineToggle.button.tooltip = "Show contents"
		v.outlineTitle.Hide()
		v.outlineContent.Hide()
		v.outlinePanel.Hide()
	}
}

func (v *markdownView) dismissOutline() {
	if v.editor.markdownView == v && v.editor.markdownOutlineExpanded {
		v.toggleOutline()
	}
}

func (v *markdownView) toggleOutline() {
	g := v.editor
	if g.markdownView != v || g.closed {
		return
	}
	g.markdownOutlineExpanded = !g.markdownOutlineExpanded
	v.applyOutlineVisibility()
	if v.viewport != nil {
		v.layoutOutline(v.viewport.Size())
		canvas.Refresh(v.viewport)
	}
	if g.markdownOutlineExpanded {
		v.outlinePanel.Refresh()
	}
	if g.markdownOutlineExpanded && v.activeHeading >= 0 {
		v.outline.ScrollTo(v.activeHeading)
	}
}

type markdownOutlineLayout struct{}

func (l *markdownOutlineLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(markdownOutlineRailWidth, 0)
}

// A normal canvas-tree layer leaves the reading area and toolbar interactive.
// A Fyne canvas PopUp would take input for the entire window instead.
type markdownOutlineOverlayLayout struct{}

func (*markdownOutlineOverlayLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }
func (*markdownOutlineOverlayLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		object.Move(fyne.Position{})
		object.Resize(size)
	}
}

type markdownOutlineSurface struct{ widget.BaseWidget }

func newMarkdownOutlineSurface() *markdownOutlineSurface {
	s := &markdownOutlineSurface{}
	s.ExtendBaseWidget(s)
	return s
}
func (*markdownOutlineSurface) Tapped(*fyne.PointEvent)    {}
func (*markdownOutlineSurface) Scrolled(*fyne.ScrollEvent) {}
func (*markdownOutlineSurface) Dragged(*fyne.DragEvent)    {}
func (*markdownOutlineSurface) DragEnd()                   {}
func (*markdownOutlineSurface) MinSize() fyne.Size         { return fyne.Size{} }
func (s *markdownOutlineSurface) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(color.Transparent)
	background.CornerRadius = 6
	background.StrokeWidth = 1
	r := &markdownOutlineSurfaceRenderer{WidgetRenderer: widget.NewSimpleRenderer(background), surface: s, background: background}
	r.Refresh()
	return r
}

type markdownOutlineSurfaceRenderer struct {
	fyne.WidgetRenderer
	surface    *markdownOutlineSurface
	background *canvas.Rectangle
}

func (r *markdownOutlineSurfaceRenderer) Refresh() {
	c := color.NRGBAModel.Convert(theme.ColorForWidget(theme.ColorNameBackground, r.surface)).(color.NRGBA)
	c.A = 224
	r.background.FillColor = c
	r.background.StrokeColor = theme.ColorForWidget(theme.ColorNameInputBorder, r.surface)
	r.background.Refresh()
}
func (*markdownOutlineLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		object.Move(fyne.Position{})
		object.Resize(size)
	}
}

func (v *markdownView) navigateHeading(index int) {
	if v.editor.markdownView != v || index < 0 || index >= len(v.headings) {
		return
	}
	v.pendingReadingOffset = nil
	v.editor.window.Canvas().Focus(nil)
	v.scroll.ScrollToOffset(fyne.NewPos(0, max(0, v.headings[index].text.pageY-markdownPageMargin)))
}

func (v *markdownView) updateOutlinePosition() {
	if v.outline == nil || v.scroll == nil || len(v.headings) == 0 || !v.headings[len(v.headings)-1].text.positioned {
		return
	}
	position := v.scroll.Offset.Y + markdownPageMargin + 1
	index := sort.Search(len(v.headings), func(i int) bool { return v.headings[i].text.pageY > position }) - 1
	if v.scroll.Offset.Y == 0 && v.headings[0].text.pageY <= markdownPageMargin+markdownPagePadding+1 {
		index = 0
	}
	if v.scroll.Offset.Y > 0 && v.scroll.Offset.Y+v.scroll.Size().Height >= v.page.Size().Height-1 {
		index = len(v.headings) - 1
	}
	if index == v.activeHeading {
		return
	}
	previous := v.activeHeading
	v.activeHeading = index
	if !v.editor.markdownOutlineExpanded {
		return
	}
	if previous >= 0 {
		v.outline.RefreshItem(previous)
	}
	if index >= 0 {
		v.outline.RefreshItem(index)
		v.outline.ScrollTo(index)
	}
}

type markdownOutlineRow struct {
	widget.BaseWidget
	view   *markdownView
	index  int
	label  *widget.Label
	marker *canvas.Rectangle
}

func (r *markdownOutlineRow) Tapped(*fyne.PointEvent) { r.view.navigateHeading(r.index) }
func (r *markdownOutlineRow) MinSize() fyne.Size      { return fyne.NewSize(100, 36) }
func (r *markdownOutlineRow) CreateRenderer() fyne.WidgetRenderer {
	return &markdownOutlineRowRenderer{row: r}
}
func (r *markdownOutlineRow) Refresh() {
	r.label.TextStyle.Bold = r.view.activeHeading == r.index
	r.marker.FillColor = theme.ColorForWidget(theme.ColorNamePrimary, r)
	if r.view.activeHeading == r.index {
		r.marker.Show()
	} else {
		r.marker.Hide()
	}
	r.label.Refresh()
	r.BaseWidget.Refresh()
}

type markdownOutlineRowRenderer struct{ row *markdownOutlineRow }

func (*markdownOutlineRowRenderer) Destroy()             {}
func (r *markdownOutlineRowRenderer) MinSize() fyne.Size { return r.row.MinSize() }
func (r *markdownOutlineRowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.row.marker, r.row.label}
}
func (r *markdownOutlineRowRenderer) Refresh() { r.row.marker.Refresh() }
func (r *markdownOutlineRowRenderer) Layout(size fyne.Size) {
	row := r.row
	indent := float32(0)
	if row.index < len(row.view.headings) {
		indent = float32(row.view.headings[row.index].level-1) * 12
	}
	row.marker.Move(fyne.NewPos(4, 8))
	row.marker.Resize(fyne.NewSize(3, max(0, size.Height-16)))
	row.label.Move(fyne.NewPos(10+indent, 0))
	row.label.Resize(fyne.NewSize(max(1, size.Width-10-indent), size.Height))
}
