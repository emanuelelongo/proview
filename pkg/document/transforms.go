package document

import (
	"fmt"
	"math"
)

// TransformOverlay applies a command in display coordinates and retains artwork.
func (d *Document) TransformOverlay(id OverlayID, command string) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	i := d.signatureIndex(id)
	if i < 0 {
		return ErrSignatureNotFound
	}
	s := &d.signatures[i]
	next := s.Transform
	rect := s.Rect
	switch command {
	case "left":
		next = next.Rotate(3)
	case "right":
		next = next.Rotate(1)
	case "horizontal":
		next = next.Mirror(true)
	case "vertical":
		next = next.Mirror(false)
	default:
		return fmt.Errorf("Unknown transform")
	}
	if command == "left" || command == "right" {
		size, e := d.PageSize(s.Page)
		if e != nil {
			return e
		}
		w, h := rect.Height*float64(size.Y)/float64(size.X), rect.Width*float64(size.X)/float64(size.Y)
		scale := math.Min(1, math.Min(1/w, 1/h))
		w *= scale
		h *= scale
		rect.X = math.Min(math.Max(rect.X+rect.Width/2-w/2, 0), 1-w)
		rect.Y = math.Min(math.Max(rect.Y+rect.Height/2-h/2, 0), 1-h)
		rect.Width, rect.Height = w, h
	}
	before := d.snapshot()
	s.Transform = next
	s.Rect = rect
	d.record(before)
	return nil
}
func transformRect(r NormalizedRect, command string) NormalizedRect {
	switch command {
	case "right":
		return NormalizedRect{1 - r.Y - r.Height, r.X, r.Height, r.Width}
	case "left":
		return NormalizedRect{r.Y, 1 - r.X - r.Width, r.Height, r.Width}
	case "horizontal":
		r.X = 1 - r.X - r.Width
	case "vertical":
		r.Y = 1 - r.Y - r.Height
	}
	return r
}
func (d *Document) TransformImage(command string) error {
	if d.Format() == FormatPDF {
		return fmt.Errorf("Select an object to transform on a PDF")
	}
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	next := d.imageTransform
	switch command {
	case "left":
		next = next.Rotate(3)
	case "right":
		next = next.Rotate(1)
	case "horizontal":
		next = next.Mirror(true)
	case "vertical":
		next = next.Mirror(false)
	default:
		return fmt.Errorf("Unknown transform")
	}
	before := d.snapshot()
	d.imageTransform = next
	for i := range d.signatures {
		s := &d.signatures[i]
		s.Rect = transformRect(s.Rect, command)
		if s.IsLens() {
			// Whole-image transforms already rotate the sampled scene. Conjugate the
			// lens-local orientation instead of applying the same transform twice.
			if command == "left" || command == "right" {
				s.Transform.FlipX, s.Transform.FlipY = s.Transform.FlipY, s.Transform.FlipX
			} else {
				s.Transform.Turns = (4 - s.Transform.Turns) % 4
			}
			continue
		}
		switch command {
		case "left":
			s.Transform = s.Transform.Rotate(3)
		case "right":
			s.Transform = s.Transform.Rotate(1)
		case "horizontal":
			s.Transform = s.Transform.Mirror(true)
		case "vertical":
			s.Transform = s.Transform.Mirror(false)
		}
	}
	d.record(before)
	return nil
}

// TransformPDFPage rotates one page without rasterizing it. Page content,
// overlays and redactions remain editable and are exported as vectors.
func (d *Document) TransformPDFPage(page int, command string) error {
	if d.Format() != FormatPDF {
		return fmt.Errorf("document: page rotation requires a PDF")
	}
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	if err := d.validatePage(page); err != nil {
		return err
	}
	turns := 0
	switch command {
	case "left":
		turns = 3
	case "right":
		turns = 1
	case "horizontal", "vertical":
		return fmt.Errorf("PDF page flipping is not supported; select an object to flip")
	default:
		return fmt.Errorf("Unknown transform")
	}
	before := d.snapshot()
	d.pages[page].turns = (d.pages[page].turns + turns) % 4
	for i := range d.signatures {
		s := &d.signatures[i]
		if s.Page != page {
			continue
		}
		s.Rect = transformRect(s.Rect, command)
		if s.IsLens() {
			s.Transform.FlipX, s.Transform.FlipY = s.Transform.FlipY, s.Transform.FlipX
		} else {
			s.Transform = s.Transform.Rotate(turns)
		}
	}
	pageID := d.pages[page].id
	for i := range d.redactions {
		if d.redactions[i].PageID != pageID {
			continue
		}
		for j, q := range d.redactions[i].Quads {
			d.redactions[i].Quads[j] = rotateTextQuad(q, command)
		}
	}
	d.record(before)
	return nil
}

func rotateTextPoint(p TextPoint, command string) TextPoint {
	if command == "right" {
		return TextPoint{X: 1 - p.Y, Y: p.X}
	}
	return TextPoint{X: p.Y, Y: 1 - p.X}
}

func rotateTextQuad(q TextQuad, command string) TextQuad {
	if command == "right" {
		return TextQuad{UL: rotateTextPoint(q.LL, command), UR: rotateTextPoint(q.UL, command), LL: rotateTextPoint(q.LR, command), LR: rotateTextPoint(q.UR, command)}
	}
	return TextQuad{UL: rotateTextPoint(q.UR, command), UR: rotateTextPoint(q.LR, command), LL: rotateTextPoint(q.UL, command), LR: rotateTextPoint(q.LL, command)}
}
