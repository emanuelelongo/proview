package document

import (
	"errors"
	"fmt"

	"emanuelelongo/proview/internal/pdfengine"
)

// CorrectionTextGeometry must run on an immutable rendering snapshot. It uses
// the composed base PDF so indexes include the current page order and rotation,
// while excluding annotation appearances and Proview's added overlays.
func (d *Document) CorrectionTextGeometry(page int) (TextLayout, error) {
	if d.Format() != FormatPDF {
		return TextLayout{}, errors.New("text editing requires a PDF")
	}
	if err := d.validatePage(page); err != nil {
		return TextLayout{}, err
	}
	if d.HasRedactions() {
		return TextLayout{}, errors.New("Edit text before adding redactions")
	}
	data, err := d.basePDF()
	if err != nil {
		return TextLayout{}, err
	}
	return pdfengine.CorrectionTextLayout(data, page)
}

// PreparedTextCorrection owns a new immutable source until applied or closed.
// The whole base PDF is retained so field values and linked widgets keep their
// identity after native object numbers change. Overlays stay separate.
type PreparedTextCorrection struct {
	source *Source
	before editorState
}

func (p *PreparedTextCorrection) Close() {
	if p != nil && p.source != nil {
		p.source.Close()
		p.source = nil
	}
}

// TextGeometry identifies the newly written content before committing it. Run
// in the preparation worker, so a continuing move needs no UI-thread parsing.
func (p *PreparedTextCorrection) TextGeometry(page int) (TextLayout, error) {
	if p == nil || p.source == nil {
		return TextLayout{}, errors.New("missing prepared text edit")
	}
	return pdfengine.CorrectionTextLayout(p.source.original, page)
}

func (d *Document) PrepareTextCorrection(page, start, count int, value string, dx, dy float64) (*PreparedTextCorrection, error) {
	return d.prepareTextEdit(page, start, count, value, dx, dy, false)
}

func (d *Document) PrepareTextDuplication(page, start, count int, value string, dx, dy float64) (*PreparedTextCorrection, error) {
	return d.prepareTextEdit(page, start, count, value, dx, dy, true)
}

func (d *Document) prepareTextEdit(page, start, count int, value string, dx, dy float64, duplicate bool) (*PreparedTextCorrection, error) {
	if d.Format() != FormatPDF {
		return nil, errors.New("text editing requires a PDF")
	}
	if err := d.validatePage(page); err != nil {
		return nil, err
	}
	if d.HasRedactions() {
		return nil, errors.New("Edit text before adding redactions")
	}
	data, err := d.basePDF()
	if err != nil {
		return nil, err
	}
	var corrected []byte
	if duplicate {
		corrected, err = pdfengine.DuplicateText(data, page, start, count, value, dx, dy)
	} else {
		corrected, err = pdfengine.CorrectText(data, page, start, count, value, dx, dy)
	}
	if err != nil {
		return nil, err
	}
	source, err := LoadBytes("Edited text.pdf", "Edited text.pdf", corrected)
	if err != nil {
		return nil, err
	}
	return &PreparedTextCorrection{source: source, before: d.snapshot()}, nil
}

func (d *Document) ApplyTextCorrection(prepared *PreparedTextCorrection) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	if prepared == nil || prepared.source == nil || prepared.source.PageCount() != d.PageCount() {
		return errors.New("invalid prepared text edit")
	}
	if !d.stateEquals(prepared.before, false) {
		return fmt.Errorf("document changed while preparing text edit")
	}
	before := d.snapshot()
	for page := range d.pages {
		d.pages[page].source = prepared.source
		d.pages[page].index = page
		d.pages[page].turns = 0
	}
	// Pending form values were applied into the prepared source and remain editable.
	d.formValues = nil
	d.textSources = append(d.textSources, prepared.source)
	prepared.source = nil
	d.record(before)
	return nil
}
