package gui

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type cropOverlay struct {
	widget.BaseWidget
	area               image.Rectangle
	onComplete         func(image.Rectangle)
	onActiveChanged    func(bool)
	onSelectionChanged func(image.Rectangle)
	liveSelection      bool
	liveArea           image.Rectangle
	active             bool
	selecting          bool
	hasSelection       bool
	dragMode           cropDragMode
	lastPos            fyne.Position
	anchor             fyne.Position
	selMin             fyne.Position
	selMax             fyne.Position
	rect               *canvas.Rectangle
	handles            []*cropHandle
	activeHandle       *cropHandle
	confirm            *widget.Button
}

type cropDragMode int

const (
	dragNone cropDragMode = iota
	dragNew
	dragMove
	dragResize
)

const (
	minCropSize    float32 = 20
	handleSize     float32 = 12
	overlayPadding float32 = 8
)

func newCropOverlay() *cropOverlay {
	rect := canvas.NewRectangle(color.NRGBA{R: 70, G: 160, B: 255, A: 96})
	rect.StrokeColor = color.NRGBA{R: 0, G: 122, B: 255, A: 200}
	rect.StrokeWidth = 2

	o := &cropOverlay{
		rect: rect,
	}

	o.confirm = widget.NewButtonWithIcon("Crop", theme.ConfirmIcon(), func() {
		o.applySelection()
	})
	o.confirm.Importance = widget.HighImportance
	o.confirm.Hide()

	o.handles = []*cropHandle{
		newCropHandle(o, handleTopLeft),
		newCropHandle(o, handleTopRight),
		newCropHandle(o, handleBottomLeft),
		newCropHandle(o, handleBottomRight),
	}

	o.ExtendBaseWidget(o)
	o.Hide()
	return o
}

func (c *cropOverlay) Activate(area image.Rectangle, onComplete func(image.Rectangle)) {
	if c.active {
		return
	}
	c.liveSelection = false
	c.onSelectionChanged = nil
	c.onComplete = onComplete
	c.clearSelection()
	c.active = true
	c.area = area
	c.Show()
	c.Refresh()
	if c.onActiveChanged != nil {
		c.onActiveChanged(true)
	}
}

// ActivateLive keeps a selection visible and reports changes without committing
// a crop. Ordinary document cropping retains its explicit confirmation behavior.
func (c *cropOverlay) ActivateLive(area image.Rectangle, changed func(image.Rectangle)) {
	c.Activate(area, nil)
	c.liveSelection = true
	c.liveArea = area
	c.onSelectionChanged = changed
	c.layoutLiveSelection()
}

func (c *cropOverlay) layoutLiveSelection() {
	if !c.liveSelection || c.area.Empty() || c.Size().IsZero() {
		return
	}
	sx, sy := c.Size().Width/float32(c.area.Dx()), c.Size().Height/float32(c.area.Dy())
	c.hasSelection = true
	c.rect.Show()
	c.setSelection(float32(c.liveArea.Min.X-c.area.Min.X)*sx, float32(c.liveArea.Min.Y-c.area.Min.Y)*sy, float32(c.liveArea.Max.X-c.area.Min.X)*sx, float32(c.liveArea.Max.Y-c.area.Min.Y)*sy)
	c.showHandles()
	c.positionControls()
}

func (c *cropOverlay) notifyLiveSelection() {
	if !c.liveSelection || c.selectionTooSmall() {
		return
	}
	minX, minY, maxX, maxY := c.selectionBounds()
	selected, ok := c.selectionImageRect(minX, minY, maxX, maxY)
	if !ok || selected.Empty() || selected == c.liveArea {
		return
	}
	c.liveArea = selected
	if c.onSelectionChanged != nil {
		c.onSelectionChanged(selected)
	}
}

func (c *cropOverlay) Deactivate() {
	if !c.active {
		return
	}
	c.clearSelection()
	c.liveSelection = false
	c.onSelectionChanged = nil
	c.active = false
	c.Hide()
	c.Refresh()
	if c.onActiveChanged != nil {
		c.onActiveChanged(false)
	}
}

// A crop owns a fixed source frame; wheel events must not move the view beneath it.
func (c *cropOverlay) Scrolled(*fyne.ScrollEvent) {}

func (c *cropOverlay) Cursor() desktop.Cursor {
	return desktop.CrosshairCursor
}

func (c *cropOverlay) clearSelection() {
	c.selecting = false
	c.hasSelection = false
	c.dragMode = dragNone
	c.activeHandle = nil
	c.rect.Hide()
	c.setSelection(0, 0, 0, 0)
	c.anchor = fyne.NewPos(0, 0)
	c.lastPos = fyne.NewPos(0, 0)
	for _, h := range c.handles {
		h.Hide()
	}
	c.confirm.Hide()
}

func (c *cropOverlay) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.NRGBA{R: 0, G: 0, B: 0, A: 96})
	objects := []fyne.CanvasObject{bg, c.rect, c.confirm}
	for _, h := range c.handles {
		objects = append(objects, h)
	}
	return &cropOverlayRenderer{overlay: c, background: bg, objects: objects}
}

func (c *cropOverlay) beginDrag(start fyne.Position) {
	if !c.active {
		return
	}
	c.selecting = true
	c.lastPos = start
	c.anchor = start

	if c.hasSelection {
		if h := c.handleAt(start); h != nil {
			c.dragMode = dragResize
			c.activeHandle = h
			return
		}
		if c.pointInSelection(start) && (!c.liveSelection || c.liveArea != c.area) {
			c.dragMode = dragMove
			return
		}
	}

	c.dragMode = dragNew
	c.activeHandle = nil
	c.hasSelection = false
	c.selMin = start
	c.selMax = start
	c.rect.Show()
	c.hideHandles()
	c.confirm.Hide()
	c.updateRect()
}

func (c *cropOverlay) Dragged(e *fyne.DragEvent) {
	if !c.active {
		return
	}

	if !c.selecting {
		start := fyne.NewPos(e.Position.X-e.Dragged.DX, e.Position.Y-e.Dragged.DY)
		c.beginDrag(c.clampPosition(start))
	}
	if !c.selecting {
		return
	}

	pos := c.clampPosition(e.Position)

	switch c.dragMode {
	case dragNew:
		c.updateSelectionFromPoints(c.anchor, pos)
	case dragMove:
		dx := float32(e.Dragged.DX)
		dy := float32(e.Dragged.DY)
		c.moveSelection(dx, dy)
	case dragResize:
		if c.activeHandle != nil {
			dx := float32(e.Dragged.DX)
			dy := float32(e.Dragged.DY)
			c.resizeByHandle(c.activeHandle.kind, dx, dy)
		}
	}
	c.lastPos = pos
	c.notifyLiveSelection()
}

func (c *cropOverlay) DragEnd() {
	c.finishDrag(c.lastPos)
}

func (c *cropOverlay) finishDrag(pos fyne.Position) {
	if !c.active || !c.selecting {
		return
	}
	c.selecting = false
	switch c.dragMode {
	case dragNew:
		c.updateSelectionFromPoints(c.anchor, pos)
		if c.selectionTooSmall() {
			if c.liveSelection {
				c.layoutLiveSelection()
			} else {
				c.clearSelection()
			}
		} else {
			c.hasSelection = true
			c.showHandles()
			c.positionControls()
		}
	case dragMove:
		// selection already updated
	case dragResize:
		if c.hasSelection {
			c.showHandles()
			c.positionControls()
		}
	}
	c.dragMode = dragNone
	c.activeHandle = nil
	c.notifyLiveSelection()
}

func (c *cropOverlay) updateSelectionFromPoints(a, b fyne.Position) {
	minX := clamp(float32(math.Min(float64(a.X), float64(b.X))), 0, c.Size().Width)
	maxX := clamp(float32(math.Max(float64(a.X), float64(b.X))), 0, c.Size().Width)
	minY := clamp(float32(math.Min(float64(a.Y), float64(b.Y))), 0, c.Size().Height)
	maxY := clamp(float32(math.Max(float64(a.Y), float64(b.Y))), 0, c.Size().Height)

	c.setSelection(minX, minY, maxX, maxY)
}

func (c *cropOverlay) moveSelection(dx, dy float32) {
	if !c.hasSelection {
		return
	}

	minX, minY, maxX, maxY := c.selectionBounds()
	size := c.Size()

	lowerX := -minX
	upperX := size.Width - maxX
	if upperX < lowerX {
		lowerX, upperX = 0, 0
	}
	lowerY := -minY
	upperY := size.Height - maxY
	if upperY < lowerY {
		lowerY, upperY = 0, 0
	}

	deltaX := clamp(dx, lowerX, upperX)
	deltaY := clamp(dy, lowerY, upperY)

	minX += deltaX
	maxX += deltaX
	minY += deltaY
	maxY += deltaY

	c.setSelection(minX, minY, maxX, maxY)
	c.positionControls()
}

func (c *cropOverlay) resizeByHandle(kind handleKind, dx, dy float32) {
	if !c.hasSelection {
		return
	}
	minX, minY, maxX, maxY := c.selectionBounds()
	size := c.Size()

	switch kind {
	case handleTopLeft:
		maxLeft := maxX - minCropSize
		if maxLeft < 0 {
			maxLeft = 0
		}
		maxTop := maxY - minCropSize
		if maxTop < 0 {
			maxTop = 0
		}
		minX = clamp(minX+dx, 0, maxLeft)
		minY = clamp(minY+dy, 0, maxTop)
	case handleTopRight:
		minWidth := minX + minCropSize
		if minWidth > size.Width {
			minWidth = size.Width
		}
		maxTop := maxY - minCropSize
		if maxTop < 0 {
			maxTop = 0
		}
		maxX = clamp(maxX+dx, minWidth, size.Width)
		minY = clamp(minY+dy, 0, maxTop)
	case handleBottomLeft:
		maxLeft := maxX - minCropSize
		if maxLeft < 0 {
			maxLeft = 0
		}
		minHeight := minY + minCropSize
		if minHeight > size.Height {
			minHeight = size.Height
		}
		minX = clamp(minX+dx, 0, maxLeft)
		maxY = clamp(maxY+dy, minHeight, size.Height)
	case handleBottomRight:
		minWidth := minX + minCropSize
		if minWidth > size.Width {
			minWidth = size.Width
		}
		minHeight := minY + minCropSize
		if minHeight > size.Height {
			minHeight = size.Height
		}
		maxX = clamp(maxX+dx, minWidth, size.Width)
		maxY = clamp(maxY+dy, minHeight, size.Height)
	}

	c.setSelection(minX, minY, maxX, maxY)
	c.positionControls()
}

func (c *cropOverlay) handleAt(pos fyne.Position) *cropHandle {
	for _, h := range c.handles {
		if !h.Visible() {
			continue
		}
		size := h.Size()
		hPos := h.Position()
		if pos.X >= hPos.X && pos.X <= hPos.X+size.Width &&
			pos.Y >= hPos.Y && pos.Y <= hPos.Y+size.Height {
			return h
		}
	}
	return nil
}

func (c *cropOverlay) updateRect() {
	if !c.rect.Visible() && !c.hasSelection && c.dragMode != dragNew {
		return
	}
	minX, minY, maxX, maxY := c.selectionBounds()
	c.rect.Move(fyne.NewPos(minX, minY))
	c.rect.Resize(fyne.NewSize(maxX-minX, maxY-minY))
	c.rect.Refresh()
}

func (c *cropOverlay) showHandles() {
	for _, h := range c.handles {
		h.Show()
		h.Resize(fyne.NewSize(handleSize, handleSize))
	}
}

func (c *cropOverlay) hideHandles() {
	for _, h := range c.handles {
		h.Hide()
	}
}

func (c *cropOverlay) positionControls() {
	if !c.hasSelection {
		return
	}
	minX, minY, maxX, maxY := c.selectionBounds()

	for _, h := range c.handles {
		var pos fyne.Position
		switch h.kind {
		case handleTopLeft:
			pos = fyne.NewPos(minX-handleSize/2, minY-handleSize/2)
		case handleTopRight:
			pos = fyne.NewPos(maxX-handleSize/2, minY-handleSize/2)
		case handleBottomLeft:
			pos = fyne.NewPos(minX-handleSize/2, maxY-handleSize/2)
		case handleBottomRight:
			pos = fyne.NewPos(maxX-handleSize/2, maxY-handleSize/2)
		}
		h.Move(pos)
		h.Refresh()
	}

	if c.liveSelection {
		c.confirm.Hide()
		return
	}
	btnSize := c.confirm.MinSize()
	x := maxX - btnSize.Width - overlayPadding
	y := maxY + overlayPadding
	if y+btnSize.Height > c.Size().Height-overlayPadding {
		y = maxY - btnSize.Height - overlayPadding
	}
	if y < overlayPadding {
		y = minY + overlayPadding
	}
	if x < overlayPadding {
		x = minX + overlayPadding
	}
	c.confirm.Move(fyne.NewPos(x, y))
	c.confirm.Resize(btnSize)
	c.confirm.Show()
	c.confirm.Refresh()
}

func (c *cropOverlay) selectionBounds() (float32, float32, float32, float32) {
	return c.selMin.X, c.selMin.Y, c.selMax.X, c.selMax.Y
}

func (c *cropOverlay) setSelection(minX, minY, maxX, maxY float32) {
	c.selMin = fyne.NewPos(minX, minY)
	c.selMax = fyne.NewPos(maxX, maxY)
	c.updateRect()
}

func (c *cropOverlay) selectionTooSmall() bool {
	minX, minY, maxX, maxY := c.selectionBounds()
	return maxX-minX < minCropSize || maxY-minY < minCropSize
}

func (c *cropOverlay) pointInSelection(pos fyne.Position) bool {
	if !c.hasSelection {
		return false
	}
	minX, minY, maxX, maxY := c.selectionBounds()
	return pos.X >= minX && pos.X <= maxX && pos.Y >= minY && pos.Y <= maxY
}

func (c *cropOverlay) selectionImageRect(minX, minY, maxX, maxY float32) (image.Rectangle, bool) {
	imgWidth := float32(c.area.Dx())
	imgHeight := float32(c.area.Dy())
	if imgWidth == 0 || imgHeight == 0 {
		return image.Rectangle{}, false
	}

	viewSize := c.Size()
	if viewSize.Width == 0 || viewSize.Height == 0 {
		return image.Rectangle{}, false
	}

	scale := float32(math.Min(
		float64(viewSize.Width/imgWidth),
		float64(viewSize.Height/imgHeight),
	))
	if scale <= 0 {
		return image.Rectangle{}, false
	}

	displayW := imgWidth * scale
	displayH := imgHeight * scale
	offsetX := (viewSize.Width - displayW) / 2
	offsetY := (viewSize.Height - displayH) / 2

	selMinX := clamp(minX-offsetX, 0, displayW)
	selMinY := clamp(minY-offsetY, 0, displayH)
	selMaxX := clamp(maxX-offsetX, 0, displayW)
	selMaxY := clamp(maxY-offsetY, 0, displayH)

	if selMaxX <= selMinX || selMaxY <= selMinY {
		return image.Rectangle{}, false
	}

	x0 := int(math.Round(float64(selMinX / scale)))
	y0 := int(math.Round(float64(selMinY / scale)))
	x1 := int(math.Round(float64(selMaxX / scale)))
	y1 := int(math.Round(float64(selMaxY / scale)))

	cropped := image.Rect(x0, y0, x1, y1)
	return cropped.Intersect(c.area), true
}

func (c *cropOverlay) applySelection() {
	if !c.hasSelection {
		return
	}
	minX, minY, maxX, maxY := c.selectionBounds()
	imgRect, ok := c.selectionImageRect(minX, minY, maxX, maxY)
	if !ok || imgRect.Empty() {
		c.Deactivate()
		return
	}
	if c.onComplete != nil {
		c.onComplete(imgRect)
	}
	c.clearSelection()
	c.Deactivate()
}

func (c *cropOverlay) clampPosition(pos fyne.Position) fyne.Position {
	size := c.Size()
	return fyne.NewPos(
		clamp(pos.X, 0, size.Width),
		clamp(pos.Y, 0, size.Height),
	)
}

type cropOverlayRenderer struct {
	overlay    *cropOverlay
	background *canvas.Rectangle
	objects    []fyne.CanvasObject
}

func (r *cropOverlayRenderer) Destroy() {}

func (r *cropOverlayRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	r.background.Move(fyne.NewPos(0, 0))
	if r.overlay.liveSelection && !r.overlay.selecting {
		r.overlay.layoutLiveSelection()
	}
	r.overlay.updateRect()
	if r.overlay.hasSelection {
		r.overlay.positionControls()
	}
}

func (r *cropOverlayRenderer) MinSize() fyne.Size {
	return fyne.NewSize(0, 0)
}

func (r *cropOverlayRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *cropOverlayRenderer) Refresh() {
	r.background.Refresh()
	if r.overlay.rect.Visible() {
		r.overlay.rect.Refresh()
	}
	for _, h := range r.overlay.handles {
		h.Refresh()
	}
	r.overlay.confirm.Refresh()
}

type handleKind int

const (
	handleTopLeft handleKind = iota
	handleTopRight
	handleBottomLeft
	handleBottomRight
)

type cropHandle struct {
	widget.BaseWidget
	overlay *cropOverlay
	kind    handleKind
	rect    *canvas.Rectangle
}

func newCropHandle(overlay *cropOverlay, kind handleKind) *cropHandle {
	rect := canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 220})
	rect.StrokeColor = color.NRGBA{R: 0, G: 0, B: 0, A: 220}
	rect.StrokeWidth = 1

	h := &cropHandle{
		overlay: overlay,
		kind:    kind,
		rect:    rect,
	}
	h.ExtendBaseWidget(h)
	h.Hide()
	return h
}

func (h *cropHandle) CreateRenderer() fyne.WidgetRenderer {
	return &cropHandleRenderer{handle: h, rect: h.rect}
}

func (h *cropHandle) Dragged(e *fyne.DragEvent) {
	if !h.overlay.active || !h.overlay.hasSelection {
		return
	}
	h.overlay.resizeByHandle(h.kind, float32(e.Dragged.DX), float32(e.Dragged.DY))
	h.overlay.notifyLiveSelection()
}

func (h *cropHandle) DragEnd() {}

type cropHandleRenderer struct {
	handle *cropHandle
	rect   *canvas.Rectangle
}

func (r *cropHandleRenderer) Destroy() {}

func (r *cropHandleRenderer) Layout(size fyne.Size) {
	r.rect.Resize(size)
}

func (r *cropHandleRenderer) MinSize() fyne.Size {
	return fyne.NewSize(handleSize, handleSize)
}

func (r *cropHandleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.rect}
}

func (r *cropHandleRenderer) Refresh() {
	r.rect.Refresh()
}

func clamp(val, min, max float32) float32 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
