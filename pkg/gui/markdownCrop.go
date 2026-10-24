package gui

import (
	"emanuelelongo/proview/pkg/document"
	"image"
	"image/draw"
	"math"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Capture uses the visible reading viewport, preserving its theme, zoom and
// diagrams. The frozen pixels own the selection until confirmation/cancellation.
type markdownViewport struct {
	widget.BaseWidget
	view    *markdownView
	content *fyne.Container
}

func newMarkdownViewport(view *markdownView) *markdownViewport {
	view.cropSnapshot = canvas.NewImageFromImage(nil)
	view.cropSnapshot.FillMode = canvas.ImageFillContain
	view.cropSnapshot.ScaleMode = canvas.ImageScaleFastest
	view.cropSnapshot.Hide()
	view.crop = newCropOverlay()
	view.crop.onActiveChanged = func(active bool) {
		view.editor.setCropToolActive(active)
		if !active {
			view.cropGeneration++
			view.cropSnapshot.Hide()
			view.cropSnapshot.Image = nil
			view.scroll.Show()
			updates := view.pendingDiagramUpdates
			view.pendingDiagramUpdates = nil
			for _, update := range updates {
				view.updateDiagramLayout(update)
			}
			view.editor.updateHistoryActions()
		}
	}
	viewport := &markdownViewport{view: view, content: container.NewStack(view.scroll, view.cropSnapshot, view.crop)}
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
func (v *markdownView) cancelCrop() {
	if v.crop != nil {
		v.crop.Deactivate()
	}
}
func (g *Gui) startMarkdownCrop() {
	v := g.markdownView
	if v == nil || v.crop == nil {
		return
	}
	if v.crop.active {
		v.cancelCrop()
		return
	}
	g.window.Canvas().Focus(nil)
	if surface, ok := g.window.Content().(*tooltipSurface); ok && surface.owner != nil {
		surface.hide(surface.owner)
	}
	frame := g.window.Canvas().Capture()
	position := g.app.Driver().AbsolutePositionForObject(v.scroll)
	canvasSize, viewportSize := g.window.Canvas().Size(), v.scroll.Size()
	region, err := markdownViewportRegion(frame, canvasSize, position, viewportSize)
	if err != nil {
		g.showOperationError("crop", err)
		return
	}
	name := strings.TrimSuffix(v.doc.Name, filepath.Ext(v.doc.Name)) + "-crop.png"
	v.cropGeneration++
	generation := v.cropGeneration
	// Capture owns immutable pixels. Copying a native GPU capture uses generic
	// per-pixel conversion, which must not delay pointer events on the UI thread.
	ready := make(chan struct{})
	var snapshot *image.RGBA
	v.crop.Activate(image.Rect(0, 0, region.Dx(), region.Dy()), func(rect image.Rectangle) {
		g.runFileWork("Cropping Markdown", func() (*document.Document, error) {
			<-ready
			return modelSnapshotDocument(snapshot, rect, name)
		}, g.openNewImage)
	})
	driver := g.app.Driver()
	go func() {
		snapshot = image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
		draw.Draw(snapshot, snapshot.Bounds(), frame, region.Min, draw.Src)
		close(ready)
		driver.DoFromGoroutine(func() {
			if g.closed || g.markdownView != v || !v.crop.active || v.cropGeneration != generation {
				return
			}
			v.cropSnapshot.Image = snapshot
			v.cropSnapshot.Show()
			v.cropSnapshot.Refresh()
			v.scroll.Hide()
		}, false)
	}()
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
