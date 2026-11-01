package document

import (
	"errors"
	"fmt"
	"unicode"

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
	data, err := d.basePDF()
	if err != nil {
		return TextLayout{}, err
	}
	layout, err := pdfengine.CorrectionTextLayout(data, page)
	if err != nil {
		return TextLayout{}, err
	}
	maskCorrectionText(&layout, coveredCorrectionGlyphs(layout, d.redactionsForPage(page)))
	return layout, nil
}

// A redaction hides the whole source word when it touches any of its glyphs.
// Glyph indexes remain intact for the native correction engine.
func coveredCorrectionGlyphs(layout TextLayout, redactions []Redaction) []bool {
	if len(redactions) == 0 {
		return nil
	}
	covered := make([]bool, len(layout.Glyphs))
	for i, glyph := range layout.Glyphs {
		if unicode.IsSpace(glyph.Rune) {
			continue
		}
		gx0, gy0, gx1, gy1 := glyph.Quad.Bounds()
		for _, redaction := range redactions {
			for _, quad := range redaction.Quads {
				rx0, ry0, rx1, ry1 := quad.Bounds()
				if gx0 < rx1 && rx0 < gx1 && gy0 < ry1 && ry0 < gy1 {
					covered[i] = true
					break
				}
			}
			if covered[i] {
				break
			}
		}
	}
	for i := range covered {
		if !covered[i] {
			continue
		}
		for j := i - 1; j >= 0 && layout.Glyphs[j].Line == layout.Glyphs[i].Line && !unicode.IsSpace(layout.Glyphs[j].Rune); j-- {
			covered[j] = true
		}
		for j := i + 1; j < len(covered) && layout.Glyphs[j].Line == layout.Glyphs[i].Line && !unicode.IsSpace(layout.Glyphs[j].Rune); j++ {
			covered[j] = true
		}
	}
	return covered
}

func maskCorrectionText(layout *TextLayout, covered []bool) {
	for i, hidden := range covered {
		if hidden {
			layout.Glyphs[i].Rune = ' '
		}
	}
}

// PreparedTextCorrection owns a new immutable source until applied or closed.
// The whole base PDF is retained so field values and linked widgets keep their
// identity after native object numbers change. Overlays stay separate.
type PreparedTextCorrection struct {
	source     *Source
	before     editorState
	page       int
	redactions []Redaction
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
	layout, err := pdfengine.CorrectionTextLayout(p.source.original, page)
	if err != nil {
		return TextLayout{}, err
	}
	if page == p.page {
		maskCorrectionText(&layout, coveredCorrectionGlyphs(layout, p.redactions))
	}
	return layout, nil
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
	data, err := d.basePDF()
	if err != nil {
		return nil, err
	}
	redactions := d.redactionsForPage(page)
	if len(redactions) != 0 {
		layout, err := pdfengine.CorrectionTextLayout(data, page)
		if err != nil {
			return nil, err
		}
		covered := coveredCorrectionGlyphs(layout, redactions)
		for i, hidden := range covered {
			if hidden && i >= start && i-start < count {
				return nil, errors.New("text covered by a redaction cannot be edited")
			}
		}
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
	return &PreparedTextCorrection{source: source, before: d.snapshot(), page: page, redactions: redactions}, nil
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
