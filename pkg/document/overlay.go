package document

import (
	"bytes"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
)

func (d *Document) Overlays() []Overlay                         { return d.Signatures() }
func (d *Document) OverlaysForPage(page int) ([]Overlay, error) { return d.SignaturesForPage(page) }
func (d *Document) UpdateOverlay(id OverlayID, page int, rect NormalizedRect) error {
	return d.UpdateSignature(id, page, rect)
}
func (d *Document) RemoveOverlay(id OverlayID) error     { return d.RemoveSignature(id) }
func (d *Document) BeginOverlayEdit(id OverlayID) error  { return d.BeginSignatureEdit(id) }
func (d *Document) CommitOverlayEdit(id OverlayID) error { return d.CommitSignatureEdit(id) }
func (d *Document) CancelOverlayEdit(id OverlayID) error { return d.CancelSignatureEdit(id) }

// RenderingSnapshot owns editor data and shares only the synchronized source.
// Capture on the UI thread before rendering thumbnails in a worker.
func (d *Document) RenderingSnapshot() *Document {
	state := d.snapshot()
	return &Document{source: d.source, pages: state.pages, crop: state.crop, signatures: state.signatures, redactions: state.redactions, imageTransform: state.imageTransform}
}

// ReplaceAnnotation can participate in a property gesture transaction, allowing
// several live previews to be committed as one undo operation.
func (d *Document) ReplaceAnnotation(id OverlayID, rect NormalizedRect, data annotations.Data, prepared signatures.Prepared) error {
	index := d.signatureIndex(id)
	if index < 0 {
		return ErrSignatureNotFound
	}
	if d.signatures[index].annotation == nil {
		return fmt.Errorf("document: object is a signature")
	}
	if d.activeEdit != nil && d.activeEdit.id != id {
		return ErrEditInProgress
	}
	if !rect.Valid() {
		return ErrInvalidRect
	}
	if err := data.Validate(); err != nil {
		return err
	}
	if prepared.Preview == nil || prepared.Preview.Bounds().Empty() {
		return fmt.Errorf("document: object is not prepared")
	}
	var before editorState
	if d.activeEdit == nil {
		before = d.snapshot()
	}
	asset := prepared.Asset.Clone()
	data = data.Clone()
	s := &d.signatures[index]
	s.Rect = rect
	s.annotation = &data
	s.asset = &asset
	s.preview = prepared.Preview
	s.png = bytes.Clone(prepared.PNG)
	if d.activeEdit == nil {
		d.record(before)
	}
	return nil
}
