package gui

import (
	"emanuelelongo/proview/pkg/document"
	"image"
	"image/draw"
	"math"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Capture uses the visible reading viewport, preserving its theme, zoom and
// diagrams. Capture is deferred until confirmation; layout stays fixed until then.
type markdownViewport struct {
	widget.BaseWidget
	view    *markdownView
	content *fyne.Container
}

func newMarkdownViewport(view *markdownView) *markdownViewport {
	view.crop = newCropOverlay()
	view.crop.onActiveChanged = func(active bool) {
		view.editor.setCropToolActive(active)
		if !active && !view.cropCapturing {
			view.resumeDiagramUpdates()
		}
	}
	viewport := &markdownViewport{view: view, content: container.NewStack(view.scroll, view.crop)}
	viewport.ExtendBaseWidget(viewport)
	return viewport
}
func (v *markdownViewport) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(v.content)
}
func (*markdownViewport) MinSize() fyne.Size { return fyne.NewSize(180, 0) }
func (v *markdownViewport) Resize(size fyne.Size) {
	if v.Size() != size {
		v.view.cancelCrop()
	}
	v.BaseWidget.Resize(size)
	v.view.scheduleThumbnail()
}
func (v *markdownView) resumeDiagramUpdates() {
	updates := v.pendingDiagramUpdates
	v.pendingDiagramUpdates = nil
	for _, update := range updates {
		v.updateDiagramLayout(update)
	}
	v.editor.updateHistoryActions()
}

func (v *markdownView) cancelCrop() {
	v.cropGeneration++
	pending := v.cropCapturing
	v.cropCapturing = false
	if v.crop != nil {
		v.crop.Deactivate()
	}
	if pending {
		v.resumeDiagramUpdates()
	}
}
func (g *Gui) startMarkdownCrop() {
	v := g.markdownView
	if v == nil || v.crop == nil {
		return
	}
	if v.crop.active || v.cropCapturing {
		v.cancelCrop()
		return
	}
	v.clearSelection()
	g.window.Canvas().Focus(nil)
	if surface, ok := g.window.Content().(*tooltipSurface); ok && surface.owner != nil {
		surface.hide(surface.owner)
	}
	viewportSize := v.scroll.Size()
	if (viewportSize.Width <= 0 || viewportSize.Height <= 0) && g.markdownPanel != nil {
		viewportSize = g.markdownPanel.Size()
	}
	if viewportSize.Width <= 0 || viewportSize.Height <= 0 {
		g.showOperationError("crop", document.ErrInvalidRect)
		return
	}
	name := strings.TrimSuffix(v.doc.Name, filepath.Ext(v.doc.Name)) + "-crop.png"
	// Use logical viewport coordinates while selecting. Canvas capture can be
	// expensive when diagrams are still rendering, so defer it until confirmation.
	area := image.Rect(0, 0, int(math.Round(float64(viewportSize.Width))), int(math.Round(float64(viewportSize.Height))))
	v.cropGeneration++
	generation := v.cropGeneration
	v.crop.Activate(area, func(rect image.Rectangle) {
		v.cropCapturing = true
		// Let the crop overlay deactivate and the canvas redraw before capturing,
		// otherwise Canvas.Capture can return the last frame with the selection tint.
		v.crop.Hide()
		fyne.Do(func() {
			if g.closed || g.markdownView != v || v.cropGeneration != generation || !v.cropCapturing {
				return
			}
			defer func() {
				v.cropCapturing = false
				v.resumeDiagramUpdates()
			}()
			frame := g.window.Canvas().Capture()
			position := g.app.Driver().AbsolutePositionForObject(v.scroll)
			canvasSize := g.window.Canvas().Size()
			region, err := markdownViewportRegion(frame, canvasSize, position, viewportSize)
			if err != nil {
				g.showOperationError("crop", err)
				return
			}
			sx, sy := float64(region.Dx())/float64(area.Dx()), float64(region.Dy())/float64(area.Dy())
			pixelRect := image.Rect(
				region.Min.X+int(math.Floor(float64(rect.Min.X)*sx)),
				region.Min.Y+int(math.Floor(float64(rect.Min.Y)*sy)),
				region.Min.X+int(math.Ceil(float64(rect.Max.X)*sx)),
				region.Min.Y+int(math.Ceil(float64(rect.Max.Y)*sy)),
			).Intersect(region)
			g.runFileWork("Cropping Markdown", func() (*document.Document, error) {
				return modelSnapshotDocument(frame, pixelRect, name)
			}, g.openNewImage)
		})
	})
}

// Canvas captures use device pixels; viewport bounds use logical Fyne points.
func markdownViewportSnapshot(frame image.Image, canvasSize fyne.Size, position fyne.Position, size fyne.Size) (*image.RGBA, error) {
	region, err := markdownViewportRegion(frame, canvasSize, position, size)
	if err != nil {
		return nil, err
	}
	snapshot := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	draw.Draw(snapshot, snapshot.Bounds(), frame, region.Min, draw.Src)
	return snapshot, nil
}

func markdownViewportRegion(frame image.Image, canvasSize fyne.Size, position fyne.Position, size fyne.Size) (image.Rectangle, error) {
	if frame == nil || canvasSize.Width <= 0 || canvasSize.Height <= 0 || size.Width <= 0 || size.Height <= 0 {
		return image.Rectangle{}, document.ErrInvalidRect
	}
	bounds := frame.Bounds()
	sx, sy := float64(bounds.Dx())/float64(canvasSize.Width), float64(bounds.Dy())/float64(canvasSize.Height)
	region := image.Rect(bounds.Min.X+int(math.Floor(float64(position.X)*sx)), bounds.Min.Y+int(math.Floor(float64(position.Y)*sy)), bounds.Min.X+int(math.Ceil(float64(position.X+size.Width)*sx)), bounds.Min.Y+int(math.Ceil(float64(position.Y+size.Height)*sy))).Intersect(bounds)
	if region.Empty() {
		return image.Rectangle{}, document.ErrInvalidRect
	}
	return region, nil
}
