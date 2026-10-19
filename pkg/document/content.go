package document

import (
	"crypto/sha256"
	"fmt"
)

// SourceRevision changes only when source geometry, forms or sanitation change.
// Overlay gestures, property edits and their Undo/Redo leave it unchanged.
func (d *Document) SourceRevision() uint64 { return d.sourceRevision.Load() }

func sourceStatesEqual(first, second editorState) bool {
	first.signatures, second.signatures = nil, nil
	return editorStatesEqual(first, second, false)
}

func (d *Document) advanceRevision(before, after editorState) {
	if !sourceStatesEqual(before, after) {
		d.sourceRevision.Add(1)
	}
	d.revision.Add(1)
}

// PageBaseKey identifies a PDF source page's pixels, independently of editor
// objects, redactions, page position and unrelated fields on other pages.
// The source owner must remain alive while retaining this key.
type PageBaseKey struct {
	source       *Source
	index, turns int
	forms        [32]byte
}

func (d *Document) BasePageKey(page int) PageBaseKey {
	if d == nil || d.Format() != FormatPDF || d.validatePage(page) != nil {
		return PageBaseKey{}
	}
	ref := d.pages[page]
	key := PageBaseKey{source: ref.source, index: ref.index, turns: ref.turns}
	if len(d.formValues) == 0 {
		return key
	}
	fields, _ := ref.source.formFieldsForPage(ref.index)
	var changed bool
	hash := sha256.New()
	for _, field := range fields {
		if values, ok := d.formValues[formKey{ref.source, field.ID}]; ok {
			fmt.Fprintf(hash, "%d:%q;", field.ID, values)
			changed = true
		}
	}
	if changed {
		copy(key.forms[:], hash.Sum(nil))
	}
	return key
}
