package gui

import (
	"emanuelelongo/proview/pkg/document"
	"emanuelelongo/proview/pkg/signatures"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"image"
	"image/color"
)

// Keep the panel width stable across tools, without making tall property lists
// increase the minimum window height.
func (g *Gui) newToolPropertyPanel() {
	properties := container.NewVScroll(container.NewPadded(g.annotationControls))
	properties.SetMinSize(fyne.NewSize(44, 0))
	properties.Hide()
	g.annotationPropertyScroll = properties
	g.currentToolIndicator = newToolIndicator()
	g.toolIndicatorHeader = container.NewPadded(container.NewVBox(g.currentToolIndicator, widget.NewSeparator()))
	// Reserve the column even when the property scroll has no visible controls.
	space := canvas.NewRectangle(color.Transparent)
	space.SetMinSize(fyne.NewSize(44, 0))
	rail := container.NewBorder(g.toolIndicatorHeader, nil, nil, nil, container.NewStack(space, properties))
	g.toolPropertyPanel = container.NewBorder(nil, nil, widget.NewSeparator(), nil, rail)
	g.refreshToolIndicator()
}

func (g *Gui) commitTextObject() {
	if g.textPlacement != nil {
		g.finishTextPlacement()
		return
	}
	if g.annotationTool != toolText {
		return
	}
	if g.hasSelectedSignature && g.textEntry != nil && g.textEntry.commit != nil {
		if g.window != nil && g.window.Canvas().Focused() == g.textEntry {
			g.window.Canvas().Focus(nil)
		} else {
			g.textEntry.commit()
		}
	}
}
func (g *Gui) showImageTransformProperties() {
	g.annotationControls.Objects = nil
	g.annotationControls.Hide()
	if g.annotationPropertyScroll != nil {
		g.annotationPropertyScroll.Hide()
	}
	g.refreshAnnotationLayout()
}
func (g *Gui) showProperties() {
	g.annotationControls.Show()
	if g.annotationPropertyScroll != nil {
		g.annotationPropertyScroll.Show()
	}
	g.annotationControls.Refresh()
	g.refreshAnnotationLayout()
}
func (g *Gui) showSignatureProperties(o document.Overlay) {
	row := container.NewVBox()
	wrap := func(obj fyne.CanvasObject) { row.Add(container.NewGridWrap(fyne.NewSize(36, 36), obj)) }
	if o.ObjectKind != "image" {
		if o.IsVector() {
			a := o.Asset()
			c := a.InkColor()
			swatch := newColorSwatch(c, func() {
				picker := dialog.NewColorPicker("Signature color", "Choose a color", func(chosen color.Color) {
					v := color.NRGBAModel.Convert(chosen).(color.NRGBA)
					v.A = 255
					g.replaceSignatureStyle(o, func(a signatures.Asset) (signatures.Asset, error) { return a.WithColor(v) })
				}, g.window)
				picker.Advanced = true
				picker.Show()
				picker.SetColor(c)
			})
			wrap(swatch)
			if a.Drawing != nil {
				wrap(numberSelect("stroke-straight", "Thickness", []float64{1, 2, 3, 4, 6, 8, 10, 12, 16, 20}, a.Drawing.Width, "", func(v float64) {
					g.replaceSignatureStyle(o, func(a signatures.Asset) (signatures.Asset, error) {
						d := a.Drawing.Clone()
						d.Width = v
						return signatures.FromDrawing(d)
					})
				}))
			} else {
				wrap(numberSelect("stroke-straight", "Thickness", []float64{0, 1, 2, 3, 4, 6, 8, 10}, a.Thickness, "", func(v float64) {
					g.replaceSignatureStyle(o, func(a signatures.Asset) (signatures.Asset, error) { return a.WithThickness(v) })
				}))
			}
		} else {
			wrap(numberSelect("stroke-straight", "Thickness", []float64{0, 1, 2, 3, 4, 6, 8, 10}, float64(o.StrokeWidth), "", func(v float64) {
				if err := g.document.SetSignatureStroke(o.ID, int(v)); err != nil {
					g.showOperationError("object_edit", err)
					return
				}
				g.reloadObjectPage(o.Page, o.ID)
			}))
		}
	}
	wrap(newIconButton(tablerIcon("trash"), "Delete object", g.deleteSelectedSignature))
	g.annotationControls.Objects = []fyne.CanvasObject{row}
	g.showProperties()
}
func (g *Gui) replaceSignatureStyle(original document.Overlay, change func(signatures.Asset) (signatures.Asset, error)) {
	if g.document == nil || g.working {
		return
	}
	doc := g.document
	current, ok := g.selectedOverlay()
	if !ok || current.ID != original.ID {
		return
	}
	g.queueAnnotation(func() func() {
		a, err := change(current.Asset())
		var p signatures.Prepared
		if err == nil {
			p, err = a.Prepare()
		}
		return func() {
			if g.document != doc {
				return
			}
			if err != nil {
				g.showOperationError("object_edit", err)
				return
			}
			if err = doc.ReplaceSignatureAsset(current.ID, p); err != nil {
				g.showOperationError("object_edit", err)
				return
			}
			g.reloadObjectPage(current.Page, current.ID)
		}
	})
}
func (g *Gui) reloadObjectPage(page int, id document.OverlayID) {
	l := g.signatureLayers[page]
	if g.document.Format() != document.FormatPDF {
		l = g.imageSignatureLayer
	}
	if l != nil {
		l.reload()
		g.selectSignature(l, id)
	}
	g.documentChanged(page)
}
func (g *Gui) transformSelected(command string) {
	if g.waitAnnotationChanges(func() { g.transformSelected(command) }) || g.document == nil || g.working {
		return
	}
	doc := g.document
	snapshot := doc.RenderingSnapshot()
	selected, hasSelection := g.selectedOverlay()
	pageTransform := !hasSelection && doc.Format() == document.FormatPDF
	page := g.activePDFPage
	g.queueAnnotation(func() func() {
		previews := map[document.OverlayID]image.Image{}
		var base image.Image
		var err error
		if hasSelection {
			err = snapshot.TransformOverlay(selected.ID, command)
		} else if pageTransform {
			err = snapshot.TransformPDFPage(page, command)
			if err == nil {
				base, err = snapshot.RenderBasePage(page)
			}
		} else {
			err = snapshot.TransformImage(command)
			if err == nil {
				base, err = snapshot.RenderBasePage(0)
			}
		}
		if err == nil && !pageTransform {
			for _, o := range snapshot.Overlays() {
				if hasSelection && o.ID != selected.ID {
					continue
				}
				if o.IsLens() {
					previews[o.ID], err = snapshot.RenderLens(o.ID, 144)
					if err != nil {
						break
					}
				} else {
					previews[o.ID] = o.Preview()
				}
			}
		}
		return func() {
			if g.document != doc || g.closed {
				return
			}
			if err != nil {
				g.showOperationError("object_edit", err)
				return
			}
			if hasSelection {
				if err = doc.TransformOverlay(selected.ID, command); err != nil {
					g.showOperationError("object_edit", err)
					return
				}
				l := g.signatureLayers[selected.Page]
				if doc.Format() != document.FormatPDF {
					l = g.imageSignatureLayer
				}
				if l != nil {
					l.preparedPreviews = previews
				}
				g.reloadObjectPage(selected.Page, selected.ID)
			} else if pageTransform {
				if err = doc.TransformPDFPage(page, command); err != nil {
					g.showOperationError("object_edit", err)
					return
				}
				g.clearAreaSelection()
				g.annotationAfterIdle = append(g.annotationAfterIdle, func() { g.rebuildPDF(page) })
			} else {
				if err = doc.TransformImage(command); err != nil {
					g.showOperationError("object_edit", err)
					return
				}
				g.clearAreaSelection()
				g.image.Image = base
				g.image.Refresh()
				g.imageSignatureLayer.preparedPreviews = previews
				g.imageSignatureLayer.SetDocument(doc, 0, base.Bounds().Size())
				g.applyZoom()
				g.updateHistoryActions()
				g.refreshAnnotationControls()
				g.scheduleLensPreviews(g.imageSignatureLayer)
			}
		}
	})
}
