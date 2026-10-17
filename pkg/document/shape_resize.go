package document

import (
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
)

// PrepareShapeResize operates on an immutable overlay snapshot. GUI callers run
// this outside the event loop and publish only the latest requested geometry.
func PrepareShapeResize(original Overlay, rect NormalizedRect) (annotations.Data, signatures.Prepared, error) {
	data, ok := original.Annotation()
	if !ok || data.Kind != annotations.Shape || !rect.Valid() {
		return annotations.Data{}, signatures.Prepared{}, fmt.Errorf("Invalid shape resize")
	}
	_, bounds, err := data.Artwork()
	if err != nil {
		return data, signatures.Prepared{}, err
	}
	sx, sy := rect.Width/original.Rect.Width, rect.Height/original.Rect.Height
	if original.Transform.Turns%2 != 0 {
		sx, sy = sy, sx
	}
	data, err = data.ResizeShape(bounds.W()*sx, bounds.H()*sy)
	if err != nil {
		return data, signatures.Prepared{}, err
	}
	asset, _, err := data.Artwork()
	if err != nil {
		return data, signatures.Prepared{}, err
	}
	prepared, err := asset.Prepare()
	return data, prepared, err
}

// CommitPreparedShapeResize publishes artwork prepared in a worker, preserving
// the transaction's original state as a single Undo step.
func (d *Document) CommitPreparedShapeResize(id OverlayID, rect NormalizedRect, data annotations.Data, prepared signatures.Prepared) error {
	if d.activeEdit == nil || d.activeEdit.id != id || data.Kind != annotations.Shape {
		return ErrEditInProgress
	}
	if err := d.ReplaceAnnotation(id, rect, data, prepared); err != nil {
		return err
	}
	before := d.activeEdit.before
	d.activeEdit = nil
	d.record(before)
	return nil
}
