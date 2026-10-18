package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"image"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	signatureHandleSize = float32(12)
	minSignatureSize    = float32(24)
)

type signatureLayer struct {
	preparedPreviews  map[document.OverlayID]image.Image
	highlightPreviews map[document.OverlayID]image.Image
	lensTimer         *time.Timer
	lensGeneration    uint64
	lensRunning       bool
	widget.BaseWidget
	document         *document.Document
	page             int
	pageSize         image.Point
	zoom             float32
	widgets          []*signatureWidget
	selected         document.SignatureID
	hasSelected      bool
	onSelect         func(*signatureLayer, document.SignatureID)
	onActivate       func(int)
	onChanged        func(int)
	editor           *Gui
	input            *annotationInput
	redactInput      *redactionInput
	highlightInput   *highlightInput
	redactionWidgets []*redactionWidget
	selectionBox     *canvas.Rectangle
	textHighlight    *canvas.Raster
	searchHighlight  *canvas.Raster
	textGesture      bool
	textMouse        fyne.Position
}

func newSignatureLayer(
	doc *document.Document,
	page int,
	pageSize image.Point,
	onSelect func(*signatureLayer, document.SignatureID),
	onActivate func(int),
	onChanged func(int),
) *signatureLayer {
	layer := &signatureLayer{
		document:   doc,
		page:       page,
		pageSize:   pageSize,
		onSelect:   onSelect,
		onActivate: onActivate,
		onChanged:  onChanged,
		zoom:       1,
	}
	layer.ExtendBaseWidget(layer)
	layer.reload()
	return layer
}

func (l *signatureLayer) CreateRenderer() fyne.WidgetRenderer {
	return &signatureLayerRenderer{layer: l}
}

func (l *signatureLayer) Tapped(e *fyne.PointEvent) {
	if l.startQRCodeSelection(e.Position, l.editorModifiers()) {
		l.finishTextSelection()
		return
	}
	if l.editor != nil && l.editor.validTextSelection() && l.editor.textSelection.layer == l {
		return
	}
	if l.editor != nil {
		l.editor.clearAreaSelection()
		l.editor.clearRedactionSelection()
		l.editor.refreshAnnotationControls()
	}
	if l.onActivate != nil {
		l.onActivate(l.page)
	}
	if l.onSelect != nil {
		l.onSelect(l, 0)
	}
}

func (l *signatureLayer) SetPageSize(size image.Point) {
	l.pageSize = size
	l.Refresh()
}

func (l *signatureLayer) SetZoom(zoom float32) {
	if zoom <= 0 {
		zoom = 1
	}
	l.zoom = zoom
	l.Refresh()
}

func (l *signatureLayer) SetDocument(doc *document.Document, page int, size image.Point) {
	if l.document != doc || l.page != page {
		l.highlightPreviews = nil
	}
	if l.highlightInput != nil {
		l.highlightInput.cancel()
		l.highlightInput.geometry = nil
	}
	if l.redactInput != nil {
		l.redactInput.cancel()
		l.redactInput.geometry = nil
	}
	l.document = doc
	l.page = page
	l.pageSize = size
	l.selected = 0
	l.hasSelected = false
	l.reload()
}

func (l *signatureLayer) reload() {
	l.widgets = nil
	l.redactionWidgets = nil
	if l.document == nil {
		l.highlightPreviews = nil
		l.Refresh()
		return
	}
	signatures, err := l.document.SignaturesForPage(l.page)
	if err != nil {
		l.Refresh()
		return
	}
	// Rebuilding the widgets must not discard their last completed blend.
	// Keep old fragments visible until the worker publishes the replacement.
	retained := map[document.OverlayID]image.Image{}
	for _, signature := range signatures {
		if signature.IsHighlight() {
			if p := l.highlightPreviews[signature.ID]; p != nil {
				retained[signature.ID] = p
			}
		}
	}
	l.highlightPreviews = retained
	for _, signature := range signatures {
		l.widgets = append(l.widgets, newSignatureWidget(l, signature))
	}
	l.preparedPreviews = nil
	for _, mark := range l.document.RedactionsForPage(l.page) {
		for _, q := range mark.Quads {
			l.redactionWidgets = append(l.redactionWidgets, newRedactionWidget(l, mark.ID, q))
		}
	}
	l.Refresh()
}

func (l *signatureLayer) selectSignature(id document.SignatureID) {
	l.selected = id
	l.hasSelected = id != 0
	for _, signature := range l.widgets {
		signature.setSelected(l.hasSelected && signature.id == id)
	}
	l.Refresh()
}

func (l *signatureLayer) selectFromWidget(id document.SignatureID) {
	for _, w := range l.widgets {
		if w.id == id && w.readOnly {
			return
		}
	}
	if l.onActivate != nil {
		l.onActivate(l.page)
	}
	if l.onSelect != nil {
		l.onSelect(l, id)
	}
}

func (l *signatureLayer) displayRect() (fyne.Position, fyne.Size) {
	size := l.Size()
	if l.pageSize.X <= 0 || l.pageSize.Y <= 0 || size.Width <= 0 || size.Height <= 0 {
		return fyne.NewPos(0, 0), size
	}
	scale := float32(math.Min(
		float64(size.Width/float32(l.pageSize.X)),
		float64(size.Height/float32(l.pageSize.Y)),
	))
	displaySize := fyne.NewSize(float32(l.pageSize.X)*scale, float32(l.pageSize.Y)*scale)
	return fyne.NewPos((size.Width-displaySize.Width)/2, (size.Height-displaySize.Height)/2), displaySize
}

func (l *signatureLayer) widgetRect(rect document.NormalizedRect) (fyne.Position, fyne.Size) {
	origin, size := l.displayRect()
	return fyne.NewPos(
			origin.X+float32(rect.X)*size.Width,
			origin.Y+float32(rect.Y)*size.Height,
		), fyne.NewSize(
			float32(rect.Width)*size.Width,
			float32(rect.Height)*size.Height,
		)
}

func (l *signatureLayer) updateSignature(id document.SignatureID, rect document.NormalizedRect) bool {
	if l.document == nil || !rect.Valid() {
		return false
	}
	if err := l.document.UpdateSignature(id, l.page, rect); err != nil {
		return false
	}
	return true
}

func (l *signatureLayer) beginSignatureEdit(id document.SignatureID) bool {
	return l.document != nil && l.document.BeginSignatureEdit(id) == nil
}

func (l *signatureLayer) finishChange(id document.SignatureID) bool {
	if l.document == nil || l.document.CommitSignatureEdit(id) != nil {
		return false
	}
	if l.onChanged != nil {
		l.onChanged(l.page)
	}
	return true
}

type signatureLayerRenderer struct {
	layer *signatureLayer
}

func (r *signatureLayerRenderer) Destroy() {}

func (r *signatureLayerRenderer) Layout(fyne.Size) {
	if r.layer.searchHighlight != nil {
		origin, size := r.layer.displayRect()
		r.layer.searchHighlight.Move(origin)
		r.layer.searchHighlight.Resize(size)
	}
	if r.layer.textHighlight != nil {
		origin, size := r.layer.displayRect()
		r.layer.textHighlight.Move(origin)
		r.layer.textHighlight.Resize(size)
	}
	if r.layer.selectionBox != nil && r.layer.editor != nil && r.layer.editor.areaSelection != nil && r.layer.editor.areaSelection.layer == r.layer {
		pos, sz := r.layer.widgetRect(r.layer.editor.areaSelection.rect)
		r.layer.selectionBox.Move(pos)
		r.layer.selectionBox.Resize(sz)
	}
	if r.layer.input != nil {
		r.layer.input.Resize(r.layer.Size())
	}
	if r.layer.redactInput != nil {
		r.layer.redactInput.Resize(r.layer.Size())
	}
	if r.layer.highlightInput != nil {
		r.layer.highlightInput.Resize(r.layer.Size())
	}
	for _, mark := range r.layer.redactionWidgets {
		position, size := r.layer.widgetRect(mark.rect)
		mark.Move(position)
		mark.Resize(size)
	}
	for _, signature := range r.layer.widgets {
		editing := false
		if g := r.layer.editor; g != nil && g.textPlacement != nil && g.textPlacement.original != nil {
			editing = g.textPlacement.layer == r.layer && g.textPlacement.original.ID == signature.id
		}
		if editing {
			signature.Hide()
		} else {
			signature.Show()
		}
		position, size := r.layer.widgetRect(signature.rect)
		margin := signature.selectionMargin()
		signature.Move(fyne.NewPos(position.X-margin, position.Y-margin))
		signature.Resize(fyne.NewSize(size.Width+2*margin, size.Height+2*margin))
	}
	if g := r.layer.editor; g != nil && g.textPlacement != nil && g.textPlacement.layer == r.layer {
		g.textPlacement.layout()
	}
}

func (r *signatureLayerRenderer) MinSize() fyne.Size {
	return fyne.NewSize(float32(r.layer.pageSize.X)*r.layer.zoom, float32(r.layer.pageSize.Y)*r.layer.zoom)
}

func (r *signatureLayerRenderer) Objects() []fyne.CanvasObject {
	objects := make([]fyne.CanvasObject, len(r.layer.widgets))
	for index, signature := range r.layer.widgets {
		objects[index] = signature
	}
	for _, mark := range r.layer.redactionWidgets {
		objects = append(objects, mark)
	}
	if r.layer.input != nil {
		objects = append(objects, r.layer.input)
	}
	if r.layer.redactInput != nil {
		objects = append(objects, r.layer.redactInput)
	}
	if r.layer.highlightInput != nil {
		objects = append(objects, r.layer.highlightInput)
	}
	if r.layer.selectionBox != nil {
		objects = append(objects, r.layer.selectionBox)
	}
	if r.layer.textHighlight != nil {
		objects = append(objects, r.layer.textHighlight)
	}
	if r.layer.searchHighlight != nil {
		objects = append(objects, r.layer.searchHighlight)
	}
	if g := r.layer.editor; g != nil && g.textPlacement != nil && g.textPlacement.layer == r.layer {
		objects = append(objects, g.textPlacement.frame, g.textPlacement.preview, g.textPlacement.field)
	}
	return objects
}

func (r *signatureLayerRenderer) Refresh() {
	r.Layout(r.layer.Size())
	for _, signature := range r.layer.widgets {
		signature.Refresh()
	}
}

type signatureWidget struct {
	readOnly bool
	widget.BaseWidget
	layer              *signatureLayer
	id                 document.SignatureID
	rect               document.NormalizedRect
	preview            image.Image
	selected           bool
	image              *canvas.Image
	border             *canvas.Rectangle
	handles            []*signatureResizeHandle
	editActive         bool
	editInitialRect    document.NormalizedRect
	areaDrag           bool
	annotation         *annotations.Data
	shapeResizeRunning bool
	shapeResizeFinish  bool
}

func newSignatureWidget(layer *signatureLayer, signature document.SignatureOverlay) *signatureWidget {
	preview := layer.preparedPreviews[signature.ID]
	if signature.IsHighlight() {
		preview = layer.highlightPreviews[signature.ID]
	}
	blended := preview != nil
	if preview == nil {
		preview = signature.Preview()
	}
	w := &signatureWidget{
		readOnly: signature.ObjectKind == "pdf-stamp",
		layer:    layer,
		id:       signature.ID,
		rect:     signature.Rect,
		preview:  preview,
		image:    canvas.NewImageFromImage(preview),
		border:   canvas.NewRectangle(color.Transparent),
	}
	w.image.FillMode = canvas.ImageFillStretch
	if data, ok := signature.Annotation(); ok {
		w.annotation = &data
		if signature.IsHighlight() && !blended {
			w.image.Hide()
		}
	}
	w.border.StrokeColor = color.NRGBA{R: 0, G: 122, B: 255, A: 255}
	w.border.StrokeWidth = 2
	w.handles = []*signatureResizeHandle{
		newSignatureResizeHandle(w, signatureTopLeft),
		newSignatureResizeHandle(w, signatureTopRight),
		newSignatureResizeHandle(w, signatureBottomLeft),
		newSignatureResizeHandle(w, signatureBottomRight),
	}
	w.ExtendBaseWidget(w)
	w.setSelected(false)
	return w
}

func (w *signatureWidget) CreateRenderer() fyne.WidgetRenderer {
	objects := []fyne.CanvasObject{w.image, w.border}
	for _, handle := range w.handles {
		objects = append(objects, handle)
	}
	return &signatureWidgetRenderer{signature: w, objects: objects}
}

func (w *signatureWidget) Tapped(*fyne.PointEvent) {
	if w.readOnly {
		return
	}
	if !w.selected {
		w.layer.selectFromWidget(w.id)
	}
}

func (w *signatureWidget) DoubleTapped(*fyne.PointEvent) {
	if !w.readOnly && w.annotation != nil && w.annotation.Kind == annotations.Text && w.layer.editor != nil {
		w.layer.selectFromWidget(w.id)
		w.layer.editor.beginExistingText(w.layer, w.id)
	}
}

func (w *signatureWidget) Cursor() desktop.Cursor {
	if w.readOnly {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (w *signatureWidget) Dragged(event *fyne.DragEvent) {
	if w.readOnly {
		return
	}
	if w.areaDrag {
		p, _ := w.layer.widgetRect(w.rect)
		w.layer.updateArea(fyne.NewPos(p.X+event.Position.X, p.Y+event.Position.Y))
		return
	}
	if w.layer.editor != nil && w.layer.editor.working {
		return
	}
	if !w.selected {
		w.layer.selectFromWidget(w.id)
	}
	// Highlights stay anchored to the text they mark. Selection and color
	// editing remain available, but dragging must not create a history edit.
	if w.annotation != nil && w.annotation.Kind == annotations.Highlight {
		return
	}
	_, displaySize := w.layer.displayRect()
	if displaySize.Width <= 0 || displaySize.Height <= 0 {
		return
	}
	next := w.rect
	next.X = clampFloat64(next.X+float64(event.Dragged.DX/displaySize.Width), 0, 1-next.Width)
	next.Y = clampFloat64(next.Y+float64(event.Dragged.DY/displaySize.Height), 0, 1-next.Height)
	if next == w.rect {
		return
	}
	if !w.editActive {
		w.editInitialRect = w.rect
		w.editActive = w.layer.beginSignatureEdit(w.id)
	}
	if !w.editActive || !w.layer.updateSignature(w.id, next) {
		return
	}
	w.rect = next
	if w.layer.editor != nil {
		w.layer.editor.scheduleLensPreviews(w.layer)
	}
	w.layer.Refresh()
}

func (w *signatureWidget) DragEnd() {
	if w.areaDrag {
		w.areaDrag = false
		w.layer.finishArea()
		return
	}
	w.finishChange()
}

func (w *signatureWidget) setSelected(selected bool) {
	selected = selected && !w.readOnly
	w.selected = selected
	if selected {
		w.border.Show()
	} else {
		w.border.Hide()
	}
	for _, handle := range w.handles {
		if selected && w.canResize() {
			handle.Show()
		} else {
			handle.Hide()
		}
	}
	w.Refresh()
}

func (w *signatureWidget) canResize() bool {
	return !w.readOnly && (w.annotation == nil || w.annotation.Kind != annotations.Highlight)
}

func (w *signatureWidget) resize(kind signatureHandleKind, dragged fyne.Delta) {
	if !w.canResize() {
		return
	}
	if w.layer.editor != nil && w.layer.editor.working && !(w.editActive && w.shapeResizeRunning) {
		return
	}
	origin, displaySize := w.layer.displayRect()
	if displaySize.Width <= 0 || displaySize.Height <= 0 || w.preview == nil {
		return
	}
	position, size := w.layer.widgetRect(w.rect)
	aspect := float32(w.preview.Bounds().Dx()) / float32(w.preview.Bounds().Dy())
	if aspect <= 0 {
		return
	}

	deltaWidth := float32(dragged.DX)
	switch kind {
	case signatureTopLeft:
		deltaWidth = chooseLargerMagnitude(-float32(dragged.DX), -float32(dragged.DY)*aspect)
	case signatureTopRight:
		deltaWidth = chooseLargerMagnitude(float32(dragged.DX), -float32(dragged.DY)*aspect)
	case signatureBottomLeft:
		deltaWidth = chooseLargerMagnitude(-float32(dragged.DX), float32(dragged.DY)*aspect)
	case signatureBottomRight:
		deltaWidth = chooseLargerMagnitude(float32(dragged.DX), float32(dragged.DY)*aspect)
	}

	var anchor fyne.Position
	var maxWidth float32
	switch kind {
	case signatureTopLeft:
		anchor = fyne.NewPos(position.X+size.Width, position.Y+size.Height)
		maxWidth = min(anchor.X-origin.X, (anchor.Y-origin.Y)*aspect)
	case signatureTopRight:
		anchor = fyne.NewPos(position.X, position.Y+size.Height)
		maxWidth = min(origin.X+displaySize.Width-anchor.X, (anchor.Y-origin.Y)*aspect)
	case signatureBottomLeft:
		anchor = fyne.NewPos(position.X+size.Width, position.Y)
		maxWidth = min(anchor.X-origin.X, (origin.Y+displaySize.Height-anchor.Y)*aspect)
	case signatureBottomRight:
		anchor = position
		maxWidth = min(origin.X+displaySize.Width-anchor.X, (origin.Y+displaySize.Height-anchor.Y)*aspect)
	}

	freeShape := w.annotation != nil && w.annotation.Kind == annotations.Shape && w.annotation.Shape != annotations.Magnifier && w.layer.editor != nil && !constrainShapeProportions(w.layer.editor.modifiers())
	if freeShape {
		w.resizeShape(kind, dragged, position, size, origin, displaySize)
		return
	}
	minimumWidth := minSignatureSize
	if w.annotation != nil && w.annotation.Kind == annotations.Shape && (w.annotation.Shape == annotations.Line || w.annotation.Shape == annotations.Arrow) {
		minimumWidth = min(minimumWidth, size.Width)
	}
	newWidth := clampFloat32(size.Width+deltaWidth, minimumWidth, maxWidth)
	newHeight := newWidth / aspect
	var newPosition fyne.Position
	switch kind {
	case signatureTopLeft:
		newPosition = fyne.NewPos(anchor.X-newWidth, anchor.Y-newHeight)
	case signatureTopRight:
		newPosition = fyne.NewPos(anchor.X, anchor.Y-newHeight)
	case signatureBottomLeft:
		newPosition = fyne.NewPos(anchor.X-newWidth, anchor.Y)
	case signatureBottomRight:
		newPosition = anchor
	}

	x := clampFloat64(float64((newPosition.X-origin.X)/displaySize.Width), 0, 1)
	y := clampFloat64(float64((newPosition.Y-origin.Y)/displaySize.Height), 0, 1)
	width := clampFloat64(float64(newWidth/displaySize.Width), 0, 1-x)
	height := clampFloat64(float64(newHeight/displaySize.Height), 0, 1-y)
	next := document.NormalizedRect{X: x, Y: y, Width: width, Height: height}
	if w.annotation != nil && w.annotation.Kind != annotations.Shape {
		initialWidth := w.rect.Width
		if w.editActive {
			initialWidth = w.editInitialRect.Width
		}
		if w.annotation.Scaled(next.Width/initialWidth).Validate() != nil {
			return
		}
	}
	if next == w.rect {
		return
	}
	if !w.editActive {
		w.editInitialRect = w.rect
		w.editActive = w.layer.beginSignatureEdit(w.id)
	}
	if !w.editActive || !w.layer.updateSignature(w.id, next) {
		return
	}
	w.rect = next
	if w.annotation != nil && w.annotation.Kind == annotations.Shape {
		w.scheduleShapeResize()
	}
	if w.layer.editor != nil {
		w.layer.editor.scheduleLensPreviews(w.layer)
	}
	w.layer.Refresh()
}

func constrainShapeProportions(modifiers fyne.KeyModifier) bool {
	return modifiers&fyne.KeyModifierControl != 0
}

func (w *signatureWidget) finishChange() {
	if !w.editActive {
		return
	}
	if w.annotation != nil && w.annotation.Kind == annotations.Shape && w.layer.editor != nil && (w.rect.Width != w.editInitialRect.Width || w.rect.Height != w.editInitialRect.Height) {
		w.shapeResizeFinish = true
		w.scheduleShapeResize()
		return
	}
	if w.layer.finishChange(w.id) {
		w.editActive = false
		for _, o := range w.layer.document.Overlays() {
			if o.ID == w.id {
				if data, ok := o.Annotation(); ok {
					w.annotation = &data
				}
				break
			}
		}
		if w.layer.editor != nil {
			w.layer.editor.updateTemporarySelection()
			w.layer.editor.refreshAnnotationControls()
		}
	}
}

type signatureWidgetRenderer struct {
	signature *signatureWidget
	objects   []fyne.CanvasObject
}

func (r *signatureWidgetRenderer) Destroy() {}

func (r *signatureWidgetRenderer) Layout(size fyne.Size) {
	margin := r.signature.selectionMargin()
	contentSize := fyne.NewSize(max(0, size.Width-2*margin), max(0, size.Height-2*margin))
	contentPosition := fyne.NewPos(margin, margin)
	r.signature.image.Move(contentPosition)
	r.signature.image.Resize(contentSize)
	r.signature.border.Move(contentPosition)
	r.signature.border.Resize(contentSize)
	positions := []fyne.Position{
		fyne.NewPos(0, 0),
		fyne.NewPos(size.Width-signatureHandleSize, 0),
		fyne.NewPos(0, size.Height-signatureHandleSize),
		fyne.NewPos(size.Width-signatureHandleSize, size.Height-signatureHandleSize),
	}
	for index, handle := range r.signature.handles {
		handle.Move(positions[index])
		handle.Resize(fyne.NewSize(signatureHandleSize, signatureHandleSize))
	}
}

// Text commonly reaches both horizontal edges of its artwork. Put its resize
// handles outside the selected artwork so the first and last glyph stay visible.
func (w *signatureWidget) selectionMargin() float32 {
	if w.selected && w.annotation != nil && w.annotation.Kind == annotations.Text {
		return signatureHandleSize
	}
	return 0
}

func (r *signatureWidgetRenderer) MinSize() fyne.Size {
	return fyne.NewSize(minSignatureSize, minSignatureSize)
}

func (r *signatureWidgetRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *signatureWidgetRenderer) Refresh() {
	r.signature.image.Refresh()
	r.signature.border.Refresh()
	for _, handle := range r.signature.handles {
		handle.Refresh()
	}
}

type signatureHandleKind int

const (
	signatureTopLeft signatureHandleKind = iota
	signatureTopRight
	signatureBottomLeft
	signatureBottomRight
)

type signatureResizeHandle struct {
	widget.BaseWidget
	signature *signatureWidget
	kind      signatureHandleKind
	rectangle *canvas.Rectangle
}

func newSignatureResizeHandle(signature *signatureWidget, kind signatureHandleKind) *signatureResizeHandle {
	rectangle := canvas.NewRectangle(color.White)
	rectangle.StrokeColor = color.NRGBA{R: 0, G: 122, B: 255, A: 255}
	rectangle.StrokeWidth = 1
	handle := &signatureResizeHandle{signature: signature, kind: kind, rectangle: rectangle}
	handle.ExtendBaseWidget(handle)
	return handle
}

func (h *signatureResizeHandle) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.rectangle)
}

func (h *signatureResizeHandle) Cursor() desktop.Cursor {
	return desktop.CrosshairCursor
}

func (h *signatureResizeHandle) Dragged(event *fyne.DragEvent) {
	h.signature.resize(h.kind, event.Dragged)
}

func (h *signatureResizeHandle) DragEnd() {
	h.signature.finishChange()
}

func chooseLargerMagnitude(first, second float32) float32 {
	if float32(math.Abs(float64(second))) > float32(math.Abs(float64(first))) {
		return second
	}
	return first
}

func clampFloat32(value, minimum, maximum float32) float32 {
	if maximum < minimum {
		return minimum
	}
	return min(max(value, minimum), maximum)
}

func clampFloat64(value, minimum, maximum float64) float64 {
	if maximum < minimum {
		return minimum
	}
	return math.Min(math.Max(value, minimum), maximum)
}

func (w *signatureWidget) MouseDown(e *desktop.MouseEvent) {
	if w.readOnly {
		return
	}
	if e.Button != desktop.MouseButtonPrimary || w.layer.editor == nil {
		return
	}
	if e.Modifier&fyne.KeyModifierAlt != 0 {
		p, _ := w.layer.widgetRect(w.rect)
		w.areaDrag = w.layer.startArea(fyne.NewPos(p.X+e.Position.X, p.Y+e.Position.Y))
		return
	}
	// Tapped is delayed by Fyne while it waits for a possible double click.
	// Select immediately on mouse down and retain DoubleTapped for text editing.
	if !w.selected {
		w.layer.selectFromWidget(w.id)
	}
}
func (w *signatureWidget) MouseUp(e *desktop.MouseEvent) {
	if w.areaDrag {
		p, _ := w.layer.widgetRect(w.rect)
		w.layer.updateArea(fyne.NewPos(p.X+e.Position.X, p.Y+e.Position.Y))
		w.layer.finishArea()
		w.areaDrag = false
	}
}

func (w *signatureWidget) resizeShape(kind signatureHandleKind, d fyne.Delta, pos fyne.Position, size fyne.Size, origin fyne.Position, display fyne.Size) {
	left, top, right, bottom := pos.X, pos.Y, pos.X+size.Width, pos.Y+size.Height
	switch kind {
	case signatureTopLeft:
		left += d.DX
		top += d.DY
	case signatureTopRight:
		right += d.DX
		top += d.DY
	case signatureBottomLeft:
		left += d.DX
		bottom += d.DY
	case signatureBottomRight:
		right += d.DX
		bottom += d.DY
	}
	minWidth, minHeight := minSignatureSize, minSignatureSize
	if w.annotation.Shape == annotations.Line || w.annotation.Shape == annotations.Arrow {
		// A horizontal/vertical segment's frame can be only a stroke thick.
		// Resizing its length must not force that thin axis into a diagonal.
		minWidth, minHeight = min(minWidth, size.Width), min(minHeight, size.Height)
	}
	left = clampFloat32(left, origin.X, right-minWidth)
	right = clampFloat32(right, left+minWidth, origin.X+display.Width)
	top = clampFloat32(top, origin.Y, bottom-minHeight)
	bottom = clampFloat32(bottom, top+minHeight, origin.Y+display.Height)
	next := document.NormalizedRect{X: float64((left - origin.X) / display.Width), Y: float64((top - origin.Y) / display.Height), Width: float64((right - left) / display.Width), Height: float64((bottom - top) / display.Height)}
	if next == w.rect || !next.Valid() {
		return
	}
	if !w.editActive {
		w.editInitialRect = w.rect
		w.editActive = w.layer.beginSignatureEdit(w.id)
	}
	if w.editActive && w.layer.updateSignature(w.id, next) {
		w.rect = next
		w.scheduleShapeResize()
		w.layer.Refresh()
	}
}
