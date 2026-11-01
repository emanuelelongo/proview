package gui

import (
	"emanuelelongo/proview/internal/model3d"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

func (v *modelViewport) initMeasurementOverlay() {
	v.measureLine = canvas.NewLine(color.RGBA{255, 190, 55, 255})
	v.measureLine.StrokeWidth = 2
	v.measureLine.Hide()
	for i := range v.measureMarkers {
		v.measureMarkers[i] = canvas.NewCircle(color.RGBA{255, 190, 55, 255})
		v.measureMarkers[i].Hide()
	}
	v.measureLabel = canvas.NewText("", color.White)
	v.measureLabel.TextSize = 14
	v.measureLabel.Hide()
	v.gridLabel = canvas.NewText("", color.RGBA{195, 205, 215, 255})
	v.gridLabel.TextSize = 12
	v.gridLabel.Hide()
}

func (v *modelViewport) frameScale() (scale, left, top float32) {
	if v.publishedSize.X <= 0 || v.publishedSize.Y <= 0 {
		return 0, 0, 0
	}
	size := v.Size()
	scale = min(size.Width/float32(v.publishedSize.X), size.Height/float32(v.publishedSize.Y))
	left = (size.Width - float32(v.publishedSize.X)*scale) / 2
	top = (size.Height - float32(v.publishedSize.Y)*scale) / 2
	return
}

func (v *modelViewport) updateMeasurementOverlay() {
	if v.measureLabel == nil {
		return
	}
	for _, marker := range v.measureMarkers {
		marker.Hide()
	}
	v.measureLine.Hide()
	v.measureLabel.Hide()
	v.gridLabel.Hide()
	if v.mesh == nil || v.publishedSize.X <= 0 {
		return
	}
	scale, left, top := v.frameScale()
	if scale <= 0 {
		return
	}
	if v.grid {
		step := model3d.GridStep(v.mesh, v.publishedCamera, v.publishedSize.X, v.publishedSize.Y)
		if step > 0 {
			v.gridLabel.Text = "Grid " + model3d.FormatMetricLength(step)
			v.gridLabel.Move(fyne.NewPos(12, 10))
			v.gridLabel.Refresh()
			v.gridLabel.Show()
		}
	}
	if !v.measuring {
		return
	}
	message := "Click first point"
	if len(v.measurePoints) == 1 {
		message = "Click second point"
	} else if len(v.measurePoints) == 2 {
		message = "Distance " + model3d.FormatMetricLength(model3d.Distance(v.measurePoints[0], v.measurePoints[1]))
	}
	v.measureLabel.Text = message
	v.measureLabel.Move(fyne.NewPos(12, max(0, v.Size().Height-30)))
	v.measureLabel.Refresh()
	v.measureLabel.Show()
	positions := [2]fyne.Position{}
	for i, point := range v.measurePoints {
		if i >= 2 {
			break
		}
		projected := model3d.Project(v.mesh, v.publishedCamera, point, v.publishedSize.X, v.publishedSize.Y)
		positions[i] = fyne.NewPos(left+float32(projected.X)*scale, top+float32(projected.Y)*scale)
		v.measureMarkers[i].Move(positions[i].Subtract(fyne.NewPos(5, 5)))
		v.measureMarkers[i].Resize(fyne.NewSize(10, 10))
		v.measureMarkers[i].Show()
	}
	if len(v.measurePoints) == 2 {
		v.measureLine.Position1, v.measureLine.Position2 = positions[0], positions[1]
		v.measureLine.Show()
	}
}

func (v *modelViewport) Tapped(event *fyne.PointEvent) {
	if !v.measuring || v.mesh == nil || v.pickPending || v.editor.modelCrop != nil && v.editor.modelCrop.active || v.image.Image == nil {
		return
	}
	scale, left, top := v.frameScale()
	if scale <= 0 {
		return
	}
	x, y := float64((event.Position.X-left)/scale), float64((event.Position.Y-top)/scale)
	if math.IsNaN(x) || math.IsNaN(y) {
		return
	}
	mesh, camera, size, epoch := v.mesh, v.publishedCamera, v.publishedSize, v.epoch
	v.pickPending = true
	v.pickGeneration++
	pickGeneration := v.pickGeneration
	go func() {
		point, hit := model3d.PickSnapped(mesh, camera, size.X, size.Y, x, y, float64(scale))
		fyne.Do(func() {
			if v.pickGeneration != pickGeneration {
				return
			}
			v.pickPending = false
			if !hit || v.stopped || !v.measuring || v.mesh != mesh || v.epoch != epoch {
				return
			}
			if len(v.measurePoints) >= 2 {
				v.measurePoints = v.measurePoints[:0]
			}
			v.measurePoints = append(v.measurePoints, point)
			v.updateMeasurementOverlay()
		})
	}()
}

func (g *Gui) toggleModelGrid() {
	if !g.showingModel() {
		return
	}
	v := g.modelViewport
	v.grid = !v.grid
	g.updateModelMeasurementMenu()
	v.requestRender()
}

func (g *Gui) toggleModelMeasurement() {
	if !g.showingModel() {
		return
	}
	v := g.modelViewport
	v.measuring = !v.measuring
	v.pickPending = false
	v.pickGeneration++
	if !v.measuring {
		v.measurePoints = nil
	}
	v.updateMeasurementOverlay()
	g.updateModelMeasurementMenu()
}

func (g *Gui) updateModelMeasurementMenu() {
	if g.modelGridItem == nil || g.modelViewport == nil {
		return
	}
	v := g.modelViewport
	g.modelGridItem.Checked = v.grid
	g.modelMeasureItem.Checked = v.measuring
	refreshMenuState(g.modelMenu)
}
