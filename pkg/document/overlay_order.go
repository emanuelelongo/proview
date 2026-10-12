package document

import "fmt"

type OverlayOrder int

const (
	BringToFront OverlayOrder = iota
	BringForward
	SendBackward
	SendToBack
)

// ReorderOverlay changes stacking within the object's page. Later entries are
// painted above earlier ones by both preview and export rendering.
func (d *Document) ReorderOverlay(id OverlayID, order OverlayOrder) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	index := d.signatureIndex(id)
	if index < 0 {
		return ErrSignatureNotFound
	}
	var indices []int
	position := 0
	for i, overlay := range d.signatures {
		if overlay.Page == d.signatures[index].Page {
			if i == index {
				position = len(indices)
			}
			indices = append(indices, i)
		}
	}
	target := position
	switch order {
	case BringToFront:
		target = len(indices) - 1
	case BringForward:
		target = min(position+1, len(indices)-1)
	case SendBackward:
		target = max(position-1, 0)
	case SendToBack:
		target = 0
	default:
		return fmt.Errorf("document: invalid overlay order %d", order)
	}
	if target == position {
		return nil
	}
	before := d.snapshot()
	overlay := d.signatures[index]
	for position < target {
		d.signatures[indices[position]] = d.signatures[indices[position+1]]
		position++
	}
	for position > target {
		d.signatures[indices[position]] = d.signatures[indices[position-1]]
		position--
	}
	d.signatures[indices[target]] = overlay
	d.record(before)
	return nil
}
