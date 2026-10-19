package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type annotationTool int

const (
	toolSelect annotationTool = iota
	toolText
	toolDraw
	toolHighlight
	toolRedact
	toolShapes
)

func (t annotationTool) kind() annotations.Kind {
	switch t {
	case toolText:
		return annotations.Text
	case toolShapes:
		return annotations.Shape
	case toolHighlight:
		return annotations.Highlight
	default:
		return annotations.Drawing
	}
}

// A single queue preserves creation order. Only the UI callback accesses the
// document; expensive outline/raster work runs outside Fyne's event loop.
func (g *Gui) queueAnnotation(work func() func()) {
	if g.annotationJobs == nil {
		g.annotationJobs = make(chan func() func(), 1)
		jobs := g.annotationJobs
		go func() {
			for job := range jobs {
				done := job()
				fyne.DoAndWait(func() {
					done()
					g.annotationRunning = false
					g.annotationPending--
					g.dispatchAnnotationJob()
					if g.annotationPending == 0 {
						g.working = false
						defer g.processPendingFiles()
						after := g.annotationAfterIdle
						g.annotationAfterIdle = nil
						for _, action := range after {
							action()
						}
						g.updateTemporarySelection()
					}
				})
			}
		}()
	}
	g.annotationPending++
	g.working = true
	g.annotationWork = append(g.annotationWork, work)
	g.dispatchAnnotationJob()
}

func (g *Gui) dispatchAnnotationJob() {
	if g.annotationRunning || len(g.annotationWork) == 0 {
		return
	}
	work := g.annotationWork[0]
	g.annotationWork[0] = nil
	g.annotationWork = g.annotationWork[1:]
	g.annotationRunning = true
	g.annotationJobs <- work
}
func (g *Gui) ensureAnnotationTools() {
	if g.annotationDefaults == nil {
		g.annotationDefaults = map[annotations.Kind]annotations.Data{}
		for _, kind := range []annotations.Kind{annotations.Text, annotations.Drawing, annotations.Highlight, annotations.Shape} {
			g.annotationDefaults[kind] = annotations.Default(kind)
		}
	}
}
func (g *Gui) bindAnnotationLayer(layer *signatureLayer) {
	layer.editor = g
	if layer.input == nil {
		layer.input = newAnnotationInput(layer)
	}
	tool := g.effectiveAnnotationTool()
	layer.input.SetVisible(tool != toolSelect && tool != toolRedact && tool != toolHighlight)
	if layer.highlightInput == nil {
		layer.highlightInput = newHighlightInput(layer)
	}
	if tool == toolHighlight {
		layer.highlightInput.Show()
	} else {
		layer.highlightInput.Hide()
	}
	if layer.document != nil && layer.document.Format() == document.FormatPDF {
		if layer.redactInput == nil {
			layer.redactInput = newRedactionInput(layer)
		}
		if g.annotationTool == toolRedact {
			layer.redactInput.Show()
			if layer.page == g.activePDFPage {
				layer.redactInput.load()
			}
		} else {
			layer.redactInput.Hide()
		}
	}
}
func (g *Gui) setAnnotationTool(tool annotationTool) {
	if g.document == nil {
		return
	}
	if tool == toolRedact && (g.document == nil || g.document.Format() != document.FormatPDF) {
		return
	}
	if g.working && g.annotationPending == 0 {
		return
	}
	g.setFormsActive(false)
	g.setTextEditing(false)
	g.commitTextObject()
	g.stopSelectionModifierTimer()
	g.temporarySelection = false
	g.clearAreaSelection()
	g.ensureAnnotationTools()
	g.deactivatePDFCrops()
	if g.cropOverlay != nil {
		g.cropOverlay.Deactivate()
	}
	for _, layer := range g.signatureLayers {
		layer.hideRedactionReveals()
		if layer.input != nil {
			layer.input.cancel()
		}
		if layer.highlightInput != nil {
			layer.highlightInput.cancel()
		}
		if layer.redactInput != nil {
			layer.redactInput.cancel()
		}
	}
	g.annotationTool = tool
	if tool != toolSelect {
		g.clearSignatureSelection()
		g.clearRedactionSelection()
	}
	for _, layer := range g.signatureLayers {
		g.bindAnnotationLayer(layer)
		layer.Refresh()
	}
	if g.imageSignatureLayer != nil {
		g.bindAnnotationLayer(g.imageSignatureLayer)
		g.imageSignatureLayer.Refresh()
	}
	for t, action := range g.annotationActions {
		action.SetActive(t == tool)
	}
	g.refreshAnnotationControls()
	g.scheduleSelectionModifiers()
}
func (g *Gui) selectedOverlay() (document.Overlay, bool) {
	if g.document == nil || !g.hasSelectedSignature {
		return document.Overlay{}, false
	}
	for _, o := range g.document.Overlays() {
		if o.ID == g.selectedSignature {
			return o, true
		}
	}
	return document.Overlay{}, false
}
func (g *Gui) annotationPageSize(layer *signatureLayer) (float64, float64) {
	// PDF previews are rendered at 144 DPI; annotations use physical points.
	scale := 1.
	if layer.document.Format() == document.FormatPDF {
		scale = 72. / 144.
	}
	return float64(layer.pageSize.X) * scale, float64(layer.pageSize.Y) * scale
}
func (g *Gui) refreshAnnotationControls() {
	if g.annotationControls == nil {
		return
	}
	if g.temporarySelection {
		g.setAnnotationControlsEnabled(false)
		return
	}
	g.ensureAnnotationTools()
	tool := g.effectiveAnnotationTool()
	if tool == toolRedact || tool == toolSelect && g.selectedRedaction != 0 {
		g.annotationControls.Objects = nil
		if g.selectedRedaction != 0 {
			g.annotationControls.Objects = []fyne.CanvasObject{container.NewHBox(widget.NewLabel("Redaction"), newIconButton(tablerIcon("trash"), "Delete redaction", g.deleteSelectedRedaction))}
			g.annotationControls.Show()
			if g.annotationPropertyScroll != nil {
				g.annotationPropertyScroll.Show()
			}
		} else {
			g.annotationControls.Hide()
			if g.annotationPropertyScroll != nil {
				g.annotationPropertyScroll.Hide()
			}
		}
		g.refreshAnnotationLayout()
		return
	}
	var selected document.Overlay
	data := g.annotationDefaults[tool.kind()]
	editing := false
	if tool == toolSelect || (tool == toolText || tool == toolShapes) && g.hasSelectedSignature {
		var ok bool
		selected, ok = g.selectedOverlay()
		if ok {
			current, annotation := selected.Annotation()
			editableText := tool == toolText && current.Kind == annotations.Text
			editableLens := tool == toolShapes && current.Kind == annotations.Shape && current.Shape == annotations.Magnifier
			if tool == toolSelect || annotation && (editableText || editableLens) {
				data, editing = current, annotation
			}
		}
		if !editing && tool == toolSelect {
			if ok {
				g.showSignatureProperties(selected)
				return
			}
			g.showImageTransformProperties()
			return
		}
	}
	g.annotationControls.Show()
	if g.annotationPropertyScroll != nil {
		g.annotationPropertyScroll.Show()
	}
	g.annotationControls.Objects = nil
	// Keep an editable copy in the controls, independent from document snapshots.
	data = data.Clone()
	if g.textPlacement != nil && (g.textPlacement.original != nil || !editing && tool == toolText) {
		data = g.textPlacement.data.Clone()
	}
	publish := func(commit bool) {
		if placement := g.textPlacement; placement != nil && placement.original != nil && placement.original.ID == selected.ID {
			text := placement.entry.Text
			placement.data = data.Clone()
			placement.data.Text = text
			placement.layer.Refresh()
			return
		}
		if !editing {
			g.annotationDefaults[data.Kind] = data.Clone()
			if data.Kind == annotations.Text {
				defaults := g.annotationDefaults[data.Kind]
				defaults.Text = ""
				g.annotationDefaults[data.Kind] = defaults
				if placement := g.textPlacement; placement != nil {
					text := placement.entry.Text
					placement.data = data.Clone()
					placement.data.Text = text
					placement.layer.Refresh()
				}
			}
			return
		}
		if data.Kind == annotations.Text && tool == toolText {
			g.annotationDefaults[annotations.Text] = data.Clone()
		}
		g.updateAnnotationProperties(selected, data.Clone(), commit)
	}
	row := container.NewHBox()
	wrap := func(width float32, object fyne.CanvasObject) {
		row.Add(container.NewGridWrap(fyne.NewSize(width, 36), object))
	}
	if data.Kind == annotations.Text {
		g.textEntry = nil
		if g.textPlacement != nil {
			g.textEntry = g.textPlacement.entry
		} else if editing {
			wrap(36, newIconButton(tablerIcon("pencil"), "Edit text on page", func() {
				layer := g.signatureLayers[selected.Page]
				if layer == nil {
					layer = g.imageSignatureLayer
				}
				g.beginExistingText(layer, selected.ID)
			}))
		}
		font := widget.NewSelect(annotations.Fonts, nil)
		font.SetSelected(data.Font)
		font.OnChanged = func(name string) { data.Font = name; publish(true) }
		wrap(180, font)
		wrap(85, numberSelect([]float64{6, 8, 9, 10, 11, 12, 14, 16, 18, 20, 24, 28, 32, 36, 48, 60, 72, 96, 120, 144}, data.Size, "", func(v float64) { data.Size = v; publish(true) }))
		for _, style := range []struct {
			icon  string
			value *bool
		}{{"bold", &data.Bold}, {"italic", &data.Italic}, {"underline", &data.Underline}, {"strikethrough", &data.Strikethrough}} {
			value := style.value
			var action *toggleToolbarAction
			action = newToggleToolbarAction(tablerIcon(style.icon), func() { *value = !*value; action.SetActive(*value); publish(true) })
			action.button.tooltip = style.icon
			action.SetActive(*value)
			wrap(36, action.button)
		}
	} else {
		if data.Kind == annotations.Shape {
			selector := g.shapeSelector(data.Shape, func(kind annotations.ShapeKind) {
				if data.Shape == annotations.Polygon && kind != annotations.Polygon {
					minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
					for _, p := range data.Points {
						minX = math.Min(minX, p.X)
						minY = math.Min(minY, p.Y)
						maxX = math.Max(maxX, p.X)
						maxY = math.Max(maxY, p.Y)
					}
					data.Points = []annotations.Point{{X: minX, Y: minY}, {X: maxX, Y: maxY}}
				}
				data.Shape = kind
				if kind == annotations.Magnifier {
					data.Color = color.NRGBA{A: 255}
					data.Width = 1.5
					data.Filled = false
					if len(data.Points) == 2 {
						a, b := data.Points[0], data.Points[1]
						diameter := math.Min(math.Abs(b.X-a.X), math.Abs(b.Y-a.Y))
						data.Points[1] = annotations.Point{X: a.X + math.Copysign(diameter, b.X-a.X), Y: a.Y + math.Copysign(diameter, b.Y-a.Y)}
					}
				}
				publish(true)
				if !editing {
					g.refreshAnnotationControls()
				}
			})
			wrap(44, selector)
			if data.Shape == annotations.Magnifier {
				row.Add(g.annotationSlider(1, 8, .1, data.Magnification, func(v float64) { data.Magnification = v }, publish))
			} else if data.Shape != annotations.Line && data.Shape != annotations.Arrow {
				var fill *iconButton
				refreshFill := func() {
					if data.Filled {
						fill.SetIcon(tablerIcon("texture"))
						fill.tooltip = "Filled"
					} else {
						fill.SetIcon(tablerIcon("square"))
						fill.tooltip = "Outline"
					}
				}
				fill = newIconButton(nil, "", func() { data.Filled = !data.Filled; refreshFill(); publish(true) })
				refreshFill()
				wrap(36, fill)
			}
		}
		widths := []float64{.5, 1, 1.5, 2, 3, 4, 6, 8, 10, 12, 16, 20, 24, 32, 40}
		if data.Kind != annotations.Highlight && (data.Kind != annotations.Shape || data.Shape != annotations.Magnifier) {
			wrap(36, strokeStyleButton(&data, publish))
			wrap(85, numberSelect(widths, data.Width, "", func(v float64) { data.Width = v; publish(true) }))
		}
		if data.Kind == annotations.Drawing {
			wrap(36, widget.NewIcon(tablerIcon("brightness-up")))
			wrap(90, numberSelect([]float64{5, 10, 15, 20, 25, 30, 35, 50, 65, 75, 90, 100}, data.Opacity*100, "%", func(v float64) { data.Opacity = v / 100; publish(true) }))
		}
	}
	var swatch *colorSwatch
	swatch = newColorSwatch(data.Color, func() {
		picker := dialog.NewColorPicker("Color", "Choose a color", func(c color.Color) {
			v := color.NRGBAModel.Convert(c).(color.NRGBA)
			v.A = 255
			data.Color = v
			swatch.setColor(v)
			publish(true)
		}, g.window)
		picker.Advanced = true
		picker.Show()
		picker.SetColor(data.Color)
	})
	if data.Kind != annotations.Shape || data.Shape != annotations.Magnifier {
		wrap(36, swatch)
	}
	if editing {
		wrap(36, newIconButton(tablerIcon("trash"), "Delete object", g.deleteSelectedSignature))
	}
	g.annotationControls.Add(row)
	if g.annotationPropertyScroll != nil {
		g.annotationPropertyScroll.SetMinSize(fyne.NewSize(0, g.annotationControls.MinSize().Height+14))
	}
	g.annotationControls.Refresh()
	g.refreshAnnotationLayout()
}

func (g *Gui) setAnnotationControlsEnabled(enabled bool) {
	var setEnabled func(fyne.CanvasObject)
	setEnabled = func(object fyne.CanvasObject) {
		if group, ok := object.(*fyne.Container); ok {
			for _, child := range group.Objects {
				setEnabled(child)
			}
		}
		if control, ok := object.(fyne.Disableable); ok {
			if enabled {
				control.Enable()
			} else {
				control.Disable()
			}
		}
	}
	setEnabled(g.annotationControls)
	g.annotationControls.Refresh()
}

func (g *Gui) refreshAnnotationLayout() {
	if g.annotationPropertyScroll != nil {
		g.annotationPropertyScroll.Refresh()
	}
	if g.toolPanelLayout != nil && g.toolPanel != nil {
		g.toolPanelLayout.updateMode(g.toolPanel.Size().Width)
	}
	if g.window != nil && g.window.Content() != nil {
		g.window.Content().Refresh()
	}
}
func (g *Gui) annotationSlider(low, high, step, value float64, set func(float64), publish func(bool)) fyne.CanvasObject {
	slider := widget.NewSlider(low, high)
	slider.Step = step
	slider.SetValue(value)
	slider.OnChanged = func(v float64) { set(v); publish(false) }
	slider.OnChangeEnded = func(float64) { publish(true) }
	return container.NewGridWrap(fyne.NewSize(190, 36), slider)
}

type annotationEntry struct {
	widget.Entry
	commit func()
	cancel func()
	inline bool
}

func newAnnotationEntry() *annotationEntry { e := &annotationEntry{}; e.ExtendBaseWidget(e); return e }
func (e *annotationEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.commit != nil && !e.inline {
		e.commit()
	}
}

func (e *annotationEntry) TypedKey(event *fyne.KeyEvent) {
	if event.Name == fyne.KeyEscape && e.cancel != nil {
		e.cancel()
		return
	}
	e.Entry.TypedKey(event)
}

func (g *Gui) addPageAnnotation(layer *signatureLayer, data annotations.Data, x, y float64, done func()) {
	g.addPageAnnotationWithResult(layer, data, x, y, false, func(error) {
		if done != nil {
			done()
		}
	})
}

func (g *Gui) addPageAnnotationWithResult(layer *signatureLayer, data annotations.Data, x, y float64, textAnchor bool, done func(error)) {
	if g.document == nil {
		return
	}
	doc := g.document
	page := layer.page
	w, h := g.annotationPageSize(layer)
	data = data.Clone()
	g.queueAnnotation(func() func() {
		asset, bounds, metrics, err := data.TextArtwork()
		if textAnchor {
			if err == nil {
				x -= metrics.Left
				y -= data.Padding
			}
		}
		var prepared signatures.Prepared
		if err == nil {
			prepared, err = asset.Prepare()
		}
		rect := document.NormalizedRect{}
		if err == nil {
			if data.Kind != annotations.Text {
				x = bounds.X0
				y = bounds.Y0
			}
			rect, err = annotationPlacementRect(bounds.W(), bounds.H(), w, h, x, y)
		}
		return func() {
			defer func() {
				if done != nil {
					done(err)
				}
			}()
			if g.closed || g.document != doc {
				return
			}
			if err != nil {
				g.showAnnotationError(page, data, err)
				return
			}
			added, addErr := doc.AddAnnotation(page, rect, data, prepared)
			err = addErr
			if err != nil {
				g.showAnnotationError(page, data, err)
				return
			}
			layer.reload()
			if data.Kind == annotations.Shape && data.Shape == annotations.Magnifier && g.effectiveAnnotationTool() == toolShapes && (layer.input == nil || !layer.input.active) {
				g.selectSignature(layer, added.ID)
			}
			if data.Kind == annotations.Text {
				next := data.Clone()
				next.Text = ""
				g.annotationDefaults[annotations.Text] = next
				if g.annotationTool == toolText {
					g.clearSignatureSelection()
					g.refreshAnnotationControls()
				}
			}
			g.documentChanged(page)
		}
	})
}
func (g *Gui) updateAnnotationProperties(original document.Overlay, data annotations.Data, commit bool) {
	if !commit {
		g.annotationPreviewWork = func() { g.renderAnnotationProperties(original, data, false) }
		if g.annotationPreviewTimer == nil {
			g.annotationPreviewTimer = time.AfterFunc(60*time.Millisecond, func() {
				fyne.Do(func() {
					g.annotationPreviewTimer = nil
					work := g.annotationPreviewWork
					g.annotationPreviewWork = nil
					if work != nil {
						work()
					}
				})
			})
		}
		return
	}
	if g.annotationPreviewTimer != nil {
		g.annotationPreviewTimer.Stop()
		g.annotationPreviewTimer = nil
	}
	g.annotationPreviewWork = nil
	g.renderAnnotationProperties(original, data, true)
}

func (g *Gui) renderAnnotationProperties(original document.Overlay, data annotations.Data, commit bool) {
	if g.document == nil || g.working && g.annotationPending == 0 {
		return
	}
	current, ok := g.selectedOverlay()
	if !ok || current.ID != original.ID {
		return
	}
	oldData, _ := current.Annotation()
	if equalAnnotationProperties(oldData, data) {
		if commit {
			g.restoreAnnotationPreview()
		}
		return
	}
	doc := g.document
	snapshot := doc.RenderingSnapshot()
	lensDPI := g.lensPreviewDPI()
	if g.previewRenderer == nil {
		g.previewRenderer = &document.PreviewRenderer{}
	}
	renderer := g.previewRenderer
	g.annotationPropertyGeneration++
	generation := g.annotationPropertyGeneration
	// Preview changes do not touch history. The final slider/text action replaces
	// the artwork once, preserving one undo step per property gesture.
	g.queueAnnotation(func() func() {
		_, oldBounds, err := oldData.Artwork()
		if current.Transform.Turns%2 != 0 {
			oldBounds.X1, oldBounds.Y1 = oldBounds.X0+oldBounds.H(), oldBounds.Y0+oldBounds.W()
		}
		var prepared signatures.Prepared
		var displayPreview image.Image
		rect := current.Rect
		if err == nil {
			asset, bounds, e := data.Artwork()
			err = e
			if err == nil {
				if current.Transform.Turns%2 != 0 {
					bounds.X1, bounds.Y1 = bounds.X0+bounds.H(), bounds.Y0+bounds.W()
				}
				if data.Kind == annotations.Text {
					pageSize, e := snapshot.PageSize(current.Page)
					if e != nil {
						err = e
					} else {
						unit := 1.
						if snapshot.Format() == document.FormatPDF {
							unit = .5
						}
						// bounds were already swapped above for page rotation.
						bw, bh := bounds.W(), bounds.H()
						if current.Transform.Turns%2 != 0 {
							bw, bh = bh, bw
						}
						rect, err = naturalTextRect(current, data, bw, bh, float64(pageSize.X)*unit, float64(pageSize.Y)*unit)
					}
				} else {
					factor := current.Rect.Width / oldBounds.W()
					rect.Width = bounds.W() * factor
					rect.Height = current.Rect.Height * bounds.H() / oldBounds.H()
					if data.Kind != annotations.Text && current.Transform.Turns == 0 && !current.Transform.FlipX && !current.Transform.FlipY {
						rect.X += (bounds.X0 - oldBounds.X0) * factor
						rect.Y += (bounds.Y0 - oldBounds.Y0) * current.Rect.Height / oldBounds.H()
					}
				}
				if oldData.Kind == annotations.Shape && oldData.Shape == annotations.Magnifier && data.Kind == annotations.Shape && data.Shape == annotations.Magnifier {
					rect = current.Rect
				}
				if err == nil {
					if rect.Width > 1 || rect.Height > 1 {
						err = fmt.Errorf("The object is too large for the page")
					} else {
						rect.X = math.Min(math.Max(rect.X, 0), 1-rect.Width)
						rect.Y = math.Min(math.Max(rect.Y, 0), 1-rect.Height)
						prepared, err = asset.Prepare()
					}
				}
			}
		}
		if err == nil {
			displayPreview = current.Transform.Apply(prepared.Preview)
			if data.Kind == annotations.Shape && data.Shape == annotations.Magnifier {
				err = snapshot.ReplaceAnnotation(current.ID, rect, data, prepared)
				if err == nil {
					displayPreview, err = renderer.RenderLens(snapshot, current.ID, lensDPI)
				}
			}
		}
		return func() {
			if g.document != doc {
				return
			}
			if !commit && generation != g.annotationPropertyGeneration {
				return
			}
			if err != nil {
				if commit {
					g.restoreAnnotationPreview()
					g.showAnnotationError(original.Page, data, err)
					g.refreshAnnotationControls()
				}
				return
			}
			if commit {
				if err := doc.ReplaceAnnotation(original.ID, rect, data, prepared); err != nil {
					g.showAnnotationError(original.Page, data, err)
					return
				}
				if layer := g.signatureLayers[original.Page]; layer != nil {
					layer.reload()
					if g.hasSelectedSignature && g.selectedSignature == original.ID {
						layer.selectSignature(original.ID)
					}
					if data.Kind == annotations.Shape && data.Shape == annotations.Magnifier {
						for _, w := range layer.widgets {
							if w.id == original.ID {
								w.preview = displayPreview
								w.image.Image = displayPreview
								w.image.Refresh()
							}
						}
					}
				}
				g.documentChanged(original.Page)
				g.annotationPreviewID = 0
				if data.Kind == annotations.Shape {
					g.refreshAnnotationControls()
				}
			} else if layer := g.signatureLayers[original.Page]; layer != nil && g.selectedSignature == original.ID {
				for _, w := range layer.widgets {
					if w.id == original.ID {
						g.annotationPreviewID = original.ID
						w.image.Image = displayPreview
						w.preview = w.image.Image
						w.rect = rect
						w.Refresh()
					}
				}
				layer.Refresh()
			}
		}
	})
}
func equalAnnotationProperties(a, b annotations.Data) bool {
	return a.Text == b.Text && a.Font == b.Font && a.Size == b.Size && a.Bold == b.Bold && a.Italic == b.Italic && a.Underline == b.Underline && a.Strikethrough == b.Strikethrough && a.Color == b.Color && a.Width == b.Width && a.Opacity == b.Opacity && a.Shape == b.Shape && a.Filled == b.Filled && a.ArrowClosed == b.ArrowClosed && a.StrokeStyle == b.StrokeStyle && a.Magnification == b.Magnification
}

func strokeStyleButton(data *annotations.Data, publish func(bool)) *iconButton {
	styles := []annotations.StrokeStyle{annotations.StrokeSolid, annotations.StrokeDashed, annotations.StrokeDotted}
	if data.Kind == annotations.Drawing {
		styles = []annotations.StrokeStyle{annotations.StrokeRound, annotations.StrokeDashed, annotations.StrokeDotted}
		if data.StrokeStyle == annotations.StrokeSolid {
			data.StrokeStyle = annotations.StrokeRound
		}
	}
	icons := map[annotations.StrokeStyle]string{annotations.StrokeSolid: "stroke-straight", annotations.StrokeDashed: "line-dashed", annotations.StrokeDotted: "line-dotted", annotations.StrokeRound: "circle-filled"}
	labels := map[annotations.StrokeStyle]string{annotations.StrokeSolid: "Solid line", annotations.StrokeDashed: "Dashed line", annotations.StrokeDotted: "Dotted line", annotations.StrokeRound: "Round tip"}
	if data.StrokeStyle == "" {
		data.StrokeStyle = styles[0]
	}
	var button *iconButton
	refresh := func() {
		button.SetIcon(tablerIcon(icons[data.StrokeStyle]))
		button.tooltip = labels[data.StrokeStyle]
	}
	button = newIconButton(nil, "", func() {
		for i, style := range styles {
			if data.StrokeStyle == style {
				data.StrokeStyle = styles[(i+1)%len(styles)]
				break
			}
		}
		refresh()
		publish(true)
	})
	refresh()
	return button
}

func (g *Gui) restoreAnnotationPreview() {
	if g.annotationPreviewID == 0 || g.document == nil {
		return
	}
	for _, o := range g.document.Overlays() {
		if o.ID == g.annotationPreviewID {
			if layer := g.signatureLayers[o.Page]; layer != nil {
				for _, w := range layer.widgets {
					if w.id == o.ID {
						w.rect = o.Rect
						w.preview = o.Preview()
						w.image.Image = w.preview
						w.Refresh()
					}
				}
				layer.Refresh()
			}
			break
		}
	}
	g.annotationPreviewID = 0
}

// Save/open/undo wait for pending vector preparation and commit entry edits.
func (g *Gui) waitAnnotationChanges(action func()) bool {
	g.finishTextPlacement()
	if g.window != nil {
		if _, ok := g.window.Canvas().Focused().(*annotationEntry); ok {
			g.window.Canvas().Focus(nil)
		}
	}
	for _, layer := range g.signatureLayers {
		if layer.highlightInput != nil {
			layer.highlightInput.released = true
			layer.highlightInput.finish()
			if layer.highlightInput.pending {
				layer.highlightInput.cancel()
			}
		}
		if layer.redactInput != nil {
			layer.redactInput.released = true
			layer.redactInput.finish()
		}
		if layer.input != nil {
			layer.input.finish()
		}
		for _, object := range layer.widgets {
			object.finishChange()
		}
	}
	if g.annotationPreviewTimer != nil {
		g.annotationPreviewTimer.Stop()
		g.annotationPreviewTimer = nil
		g.annotationPreviewWork = nil
	}
	if g.annotationPending == 0 {
		return false
	}
	g.annotationAfterIdle = append(g.annotationAfterIdle, action)
	return true
}
