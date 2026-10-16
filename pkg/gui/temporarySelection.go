package gui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) effectiveAnnotationTool() annotationTool {
	if g.temporarySelection {
		return toolSelect
	}
	return g.annotationTool
}

func (g *Gui) handleSelectionShift(key fyne.KeyName, down bool) {
	var bit uint8
	switch key {
	case desktop.KeyShiftLeft:
		bit = 1
	case desktop.KeyShiftRight:
		bit = 2
	default:
		return
	}
	if down {
		g.selectionShiftKeys |= bit
	} else {
		g.selectionShiftKeys &^= bit
	}
	g.updateTemporarySelection()
	g.refreshTextMoveAlignment()
}

func (g *Gui) updateTemporarySelection() {
	active := g.selectionShiftKeys != 0 && g.document != nil && !g.closed &&
		g.supportsTemporarySelection() && allowsTemporarySelection(g.modifiers())
	if active == g.temporarySelection {
		return
	}
	if active {
		if g.window != nil {
			switch g.window.Canvas().Focused().(type) {
			case *annotationEntry, *widget.Entry:
				return
			}
		}
		if g.cropOverlay != nil && g.cropOverlay.active {
			return
		}
		for _, crop := range g.pdfCrops {
			if crop.active {
				return
			}
		}
	} else {
		if g.window != nil {
			if _, editing := g.window.Canvas().Focused().(*annotationEntry); editing {
				g.window.Canvas().Focus(nil)
			}
		}
		if g.annotationPending > 0 {
			return
		}
	}
	gestureActive := func(layer *signatureLayer) bool {
		if layer == nil {
			return false
		}
		if layer.textGesture {
			return true
		}
		if layer.input != nil && layer.input.active {
			return true
		}
		for _, object := range layer.widgets {
			if object.editActive {
				return true
			}
		}
		return false
	}
	// Finish the current mouse gesture before changing its input surface.
	for _, layer := range g.signatureLayers {
		if gestureActive(layer) {
			return
		}
	}
	if gestureActive(g.imageSignatureLayer) {
		return
	}
	g.temporarySelection = active
	g.clearAreaSelection()
	if !active {
		g.clearSignatureSelection()
	}
	for _, layer := range g.signatureLayers {
		g.bindAnnotationLayer(layer)
		layer.Refresh()
	}
	if g.imageSignatureLayer != nil {
		g.bindAnnotationLayer(g.imageSignatureLayer)
		g.imageSignatureLayer.Refresh()
	}
	for tool, action := range g.annotationActions {
		action.SetActive(tool == g.effectiveAnnotationTool())
	}
	g.refreshAnnotationControls()
}

func allowsTemporarySelection(modifiers fyne.KeyModifier) bool {
	// Control is intentionally allowed so Shift+Control can select temporarily
	// while constraining a shape's resize proportions.
	return modifiers&(fyne.KeyModifierSuper|fyne.KeyModifierAlt) == 0
}

func (g *Gui) supportsTemporarySelection() bool {
	return g.annotationTool == toolText || g.annotationTool == toolDraw || g.annotationTool == toolHighlight || g.annotationTool == toolShapes
}

func (g *Gui) stopSelectionModifierTimer() {
	if g.selectionModifierTimer != nil {
		g.selectionModifierTimer.Stop()
		g.selectionModifierTimer = nil
	}
}

func (g *Gui) scheduleSelectionModifiers() {
	if g.selectionModifierTimer != nil || g.closed || g.document == nil || !g.supportsTemporarySelection() || g.app == nil {
		return
	}
	driver, native := g.app.Driver().(desktop.Driver)
	if !native {
		return
	}
	// Fyne routes key events to focused widgets instead of canvas handlers.
	// Poll the driver too, so releases and presses over property controls work.
	var timer *time.Timer
	timer = time.AfterFunc(50*time.Millisecond, func() {
		fyne.Do(func() {
			if g.selectionModifierTimer != timer || g.closed {
				return
			}
			g.selectionModifierTimer = nil
			g.selectionShiftKeys = 0
			if driver.CurrentKeyModifiers()&fyne.KeyModifierShift != 0 {
				g.selectionShiftKeys = 1
			}
			g.updateTemporarySelection()
			g.scheduleSelectionModifiers()
		})
	})
	g.selectionModifierTimer = timer
}
