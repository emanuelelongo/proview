package gui

import (
	"context"
	"emanuelelongo/proview/internal/model3d"
	"emanuelelongo/proview/internal/modelgpu"
	"emanuelelongo/proview/pkg/document"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type modelViewport struct {
	widget.BaseWidget
	editor                        *Gui
	mesh                          *model3d.Mesh
	renderer                      *modelgpu.Renderer
	camera                        model3d.Camera
	image                         *canvas.Image
	generation                    uint64
	epoch                         uint64
	running, stopped, interacting bool
	cancel                        context.CancelFunc
}

func newModelViewport(g *Gui) *modelViewport {
	v := &modelViewport{editor: g, camera: model3d.DefaultCamera(), image: canvas.NewImageFromImage(nil)}
	v.image.FillMode = canvas.ImageFillContain
	v.image.ScaleMode = canvas.ImageScaleSmooth
	v.ExtendBaseWidget(v)
	return v
}
func (v *modelViewport) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(canvas.NewRectangle(model3d.BackgroundColor), v.image))
}
func (v *modelViewport) MinSize() fyne.Size { return fyne.NewSize(240, 180) }
func (v *modelViewport) Resize(s fyne.Size) {
	old := v.Size()
	v.BaseWidget.Resize(s)
	if old != s {
		if v.editor.modelCrop != nil {
			v.editor.modelCrop.Deactivate()
		}
		v.requestRender()
	}
}
func (v *modelViewport) Cursor() desktop.Cursor        { return desktop.PointerCursor }
func (v *modelViewport) MouseDown(*desktop.MouseEvent) { v.editor.window.Canvas().Focus(nil) }
func (v *modelViewport) MouseUp(*desktop.MouseEvent)   {}
func (v *modelViewport) Dragged(event *fyne.DragEvent) {
	if v.mesh == nil {
		return
	}
	v.interacting = true
	if v.editor.panModifierDown {
		s := v.Size()
		v.camera.PanX += float64(event.Dragged.DX) / float64(max(1, s.Width))
		v.camera.PanY += float64(event.Dragged.DY) / float64(max(1, s.Height))
		v.camera.PanX = math.Max(-2, math.Min(2, v.camera.PanX))
		v.camera.PanY = math.Max(-2, math.Min(2, v.camera.PanY))
	} else {
		v.camera.Yaw += float64(event.Dragged.DX) * .01
		v.camera.Pitch = math.Max(-math.Pi/2, math.Min(math.Pi/2, v.camera.Pitch+float64(event.Dragged.DY)*.01))
	}
	v.requestRender()
}
func (v *modelViewport) DragEnd() { v.interacting = false; v.requestRender() }
func (v *modelViewport) Scrolled(e *fyne.ScrollEvent) {
	v.zoom(math.Exp(float64(e.Scrolled.DY) * .015))
}
func (v *modelViewport) zoom(factor float64) {
	if v.editor.modelCrop != nil {
		v.editor.modelCrop.Deactivate()
	}
	v.camera.Zoom = math.Max(.1, math.Min(20, v.camera.Zoom*factor))
	v.requestRender()
}
func (v *modelViewport) setView(name string) {
	if v.editor.modelCrop != nil {
		v.editor.modelCrop.Deactivate()
	}
	wire := v.camera.Wireframe
	v.camera = model3d.DefaultCamera()
	v.camera.Wireframe = wire
	switch name {
	case "Front":
		v.camera.Yaw, v.camera.Pitch = 0, 0
	case "Right":
		v.camera.Yaw, v.camera.Pitch = math.Pi/2, 0
	case "Left":
		v.camera.Yaw, v.camera.Pitch = -math.Pi/2, 0
	case "Rear":
		v.camera.Yaw, v.camera.Pitch = math.Pi, 0
	case "Top":
		v.camera.Yaw, v.camera.Pitch = 0, math.Pi/2
	case "Bottom":
		v.camera.Yaw, v.camera.Pitch = 0, -math.Pi/2
	}
	v.requestRender()
}
func (v *modelViewport) stop() {
	if v.renderer != nil {
		v.renderer.Close()
		v.renderer = nil
	}
	v.stopped = true
	v.mesh = nil
	v.generation++
	v.epoch++
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	v.image.Image = nil
	v.image.Refresh()
}
func (v *modelViewport) requestRender() {
	// Coalesce camera changes without repeatedly aborting an in-flight frame.
	v.generation++
	if v.running || v.stopped || v.mesh == nil || v.editor.closed {
		return
	}
	v.startRender()
}
func (v *modelViewport) startRender() {
	if v.stopped || v.mesh == nil || v.editor.closed {
		return
	}
	size := v.Size()
	scale := 1.
	if v.editor.window != nil {
		scale = float64(v.editor.window.Canvas().Scale())
	}
	w, h := max(1, int(float64(size.Width)*scale)), max(1, int(float64(size.Height)*scale))
	limit := 2000
	if v.interacting && (v.renderer == nil || v.renderer.Backend() == "CPU") {
		limit = 1000
	}
	if max(w, h) > limit {
		ratio := float64(limit) / float64(max(w, h))
		w, h = max(1, int(float64(w)*ratio)), max(1, int(float64(h)*ratio))
	}
	if v.renderer == nil {
		v.renderer = modelgpu.NewRenderer()
	}
	renderer := v.renderer
	mesh, camera, generation := v.mesh, v.camera, v.generation
	epoch := v.epoch
	interacting := v.interacting
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	v.running = true
	go func() {
		var frame *image.RGBA
		var err error
		if interacting {
			frame, err = renderer.RenderInteractive(ctx, mesh, camera, w, h)
		} else {
			frame, err = renderer.Render(ctx, mesh, camera, w, h)
		}
		cancel()
		fyne.Do(func() {
			v.running = false
			v.cancel = nil
			if v.stopped || v.mesh == nil || v.editor.closed {
				return
			}
			if err == nil && mesh == v.mesh && epoch == v.epoch && !v.editor.modelCrop.active {
				v.image.Image = frame
				v.image.Refresh()
			}
			if generation != v.generation {
				v.startRender()
			}
		})
	}()
}
func (g *Gui) hasOpenContent() bool {
	return g.showingMarkdown() || g.document != nil || (g.modelViewport != nil && g.modelViewport.mesh != nil)
}
func (g *Gui) showingModel() bool { return g.modelViewport != nil && g.modelViewport.mesh != nil }
func (g *Gui) clearModel() {
	if g.modelCrop != nil {
		g.modelCrop.Deactivate()
	}
	if g.modelViewport != nil {
		g.modelViewport.stop()
	}
	if g.modelPanel != nil {
		g.modelPanel.Hide()
	}
}
func (g *Gui) showModel(mesh *model3d.Mesh) {
	g.clearMarkdown()
	if g.document != nil {
		g.closeDocument()
	} else {
		g.clearModel()
		g.detachDocumentView()
	}
	g.panModifierDown = false
	g.modelViewport.stopped = false
	g.modelViewport.interacting = false
	g.modelViewport.mesh = mesh
	g.modelViewport.camera = model3d.DefaultCamera()
	if g.modelWireframeItem != nil {
		g.modelWireframeItem.Checked = false
	}
	g.imageScroll.Hide()
	g.pdfScroll.Hide()
	g.sidebar.container.Hide()
	g.annotationControls.Hide()
	g.annotationPropertyScroll.Hide()
	g.modelPanel.Show()
	g.updateHistoryActions()
	g.modelViewport.requestRender()
}
func newModelPanel(g *Gui) fyne.CanvasObject {
	g.modelViewport = newModelViewport(g)
	g.modelCrop = newCropOverlay()
	g.modelCrop.onActiveChanged = g.setCropToolActive
	return container.NewStack(g.modelViewport, g.modelCrop)
}
func loadModelReader(uri fyne.URIReadCloser) (*model3d.Mesh, error) {
	name := uri.URI().Name()
	mesh, err := model3d.Load(name, uri)
	err = errors.Join(err, uri.Close())
	return mesh, err
}
func openFileExtensions() []string {
	return append(document.OpenExtensions(), ".stl", ".md", ".markdown")
}
func isModelFile(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".stl") }
func (g *Gui) modelInfo() string {
	m := g.modelViewport.mesh
	s := m.Size()
	return fmt.Sprintf("%d triangles · %.3g × %.3g × %.3g units", len(m.Triangles), s.X, s.Y, s.Z)
}

var modelViews = []string{"Isometric", "Top", "Bottom", "Front", "Rear", "Left", "Right"}

func (g *Gui) toggleModelWireframe() {
	if !g.showingModel() {
		return
	}
	g.modelCrop.Deactivate()
	g.modelViewport.camera.Wireframe = !g.modelViewport.camera.Wireframe
	g.modelWireframeItem.Checked = g.modelViewport.camera.Wireframe
	refreshMenuState(g.modelMenu)
	g.modelViewport.requestRender()
}

func (g *Gui) handleModelKey(key fyne.KeyName) bool {
	if !g.showingModel() || g.window.Canvas().Focused() != nil || g.window.Canvas().Overlays().Top() != nil {
		return false
	}
	if driver, ok := g.app.Driver().(desktop.Driver); ok && driver.CurrentKeyModifiers() != 0 {
		return false
	}
	if key == fyne.Key0 {
		g.toggleModelWireframe()
		return true
	}
	for i, name := range modelViews {
		if key == fyne.KeyName(fmt.Sprint(i+1)) {
			g.modelViewport.setView(name)
			return true
		}
	}
	return false
}

func (g *Gui) startModelCrop() {
	crop := g.modelCrop
	if crop.active {
		crop.Deactivate()
		return
	}
	frame := g.modelViewport.image.Image
	if frame == nil {
		return
	}
	name := strings.TrimSuffix(g.modelViewport.mesh.Name, filepath.Ext(g.modelViewport.mesh.Name)) + "-crop.png"
	crop.Activate(frame.Bounds(), func(rect image.Rectangle) {
		g.runFileWork("Capturing model", func() (*document.Document, error) {
			return modelSnapshotDocument(frame, rect, name)
		}, func(doc *document.Document) {
			if g.closed {
				doc.Close()
				return
			}
			g.openNewImage(doc)
		})
	})
}
func modelSnapshotDocument(frame image.Image, rect image.Rectangle, name string) (*document.Document, error) {
	rect = rect.Intersect(frame.Bounds())
	if rect.Empty() {
		return nil, document.ErrInvalidRect
	}
	cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(cropped, cropped.Bounds(), frame, rect.Min, draw.Src)
	return document.NewImageFromImage(name, cropped)
}
