package gui

import (
	"emanuelelongo/proview/pkg/document"
)

// Coalesce pointer updates: one resize preparation runs per object, followed
// by the latest requested size. The final preparation commits the whole drag.
func (w *signatureWidget) scheduleShapeResize() {
	g := w.layer.editor
	if g == nil || w.shapeResizeRunning || !w.editActive {
		return
	}
	doc, target := w.layer.document, w.rect
	var original document.Overlay
	for _, object := range doc.Overlays() {
		if object.ID == w.id {
			original = object
			break
		}
	}
	original.Rect = w.editInitialRect
	w.shapeResizeRunning = true
	g.queueAnnotation(func() func() {
		data, prepared, err := document.PrepareShapeResize(original, target)
		return func() {
			w.shapeResizeRunning = false
			if g.closed || g.document != doc || !w.editActive {
				return
			}
			if target != w.rect {
				w.scheduleShapeResize()
				return
			}
			if err != nil {
				if w.shapeResizeFinish {
					_ = doc.CancelOverlayEdit(w.id)
					w.editActive = false
					w.layer.reload()
					g.showOperationError("shape_resize", err)
					g.updateTemporarySelection()
				}
				return
			}
			w.preview = original.Transform.Apply(prepared.Preview)
			w.image.Image = w.preview
			w.image.Refresh()
			w.layer.Refresh()
			if w.shapeResizeFinish {
				if err := doc.CommitPreparedShapeResize(w.id, target, data, prepared); err != nil {
					g.showOperationError("shape_resize", err)
					return
				}
				w.editActive, w.shapeResizeFinish = false, false
				w.annotation = &data
				if w.layer.onChanged != nil {
					w.layer.onChanged(w.layer.page)
				}
				g.updateTemporarySelection()
				g.refreshAnnotationControls()
			}
		}
	})
}
