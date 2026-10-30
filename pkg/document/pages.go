package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"sync/atomic"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Stable references retain lazy rendering caches across page edits and history.
type pageRef struct {
	source *Source
	index  int
	id     uint64
	turns  int
}

var pageSequence atomic.Uint64

// BlankPageAfter prepares an independent blank PDF matching the selected page.
// It does not change this document and can run on a rendering snapshot.
func (d *Document) BlankPageAfter(page int) (*Document, error) {
	if d.Format() != FormatPDF {
		return nil, fmt.Errorf("document: blank PDF pages require a PDF")
	}
	source, index, err := d.pageSource(page)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	var selected, inserted, blank bytes.Buffer
	if err := collectPDFPages(ctx, source.originalBytes(), &selected, []string{strconv.Itoa(index + 1)}); err != nil {
		return nil, err
	}
	boxes, err := api.Boxes(ctx, bytes.NewReader(selected.Bytes()), nil, nil)
	if err != nil {
		return nil, err
	}
	box := boxes[0].CropBox()
	width, height := box.Width(), box.Height()
	if boxes[0].Rot%180 != 0 {
		width, height = height, width
	}
	config := &pdfcpu.PageConfiguration{PageDim: &types.Dim{Width: width, Height: height}}
	if err := api.InsertPages(ctx, bytes.NewReader(selected.Bytes()), &inserted, []string{"1"}, false, config, nil); err != nil {
		return nil, err
	}
	if err := api.Collect(ctx, bytes.NewReader(inserted.Bytes()), &blank, []string{"2"}, nil); err != nil {
		return nil, err
	}
	s, err := LoadFrom("Blank page.pdf", "", bytes.NewReader(blank.Bytes()))
	if err != nil {
		return nil, err
	}
	doc, err := New(s)
	if err != nil {
		s.Close()
	}
	return doc, err
}

// InsertBlankPage takes ownership of the prepared single page and records one
// history step, retaining existing page identities and moving their overlays.
func (d *Document) InsertBlankPage(after int, blank *Document) error {
	if d.Format() != FormatPDF || blank == nil || blank.Format() != FormatPDF || blank.PageCount() != 1 || len(blank.signatures) != 0 || blank.HasRedactions() {
		return fmt.Errorf("document: expected a single blank PDF page")
	}
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	if err := d.validatePage(after); err != nil {
		return err
	}
	before := d.snapshot()
	position := after + 1
	p := blank.pages[0]
	p.id = pageSequence.Add(1)
	d.pages = append(d.pages, pageRef{})
	copy(d.pages[position+1:], d.pages[position:])
	d.pages[position] = p
	for i := range d.signatures {
		if d.signatures[i].Page >= position {
			d.signatures[i].Page++
		}
	}
	d.record(before)
	return nil
}

// AppendDocumentPages transfers an ordered collection of original PDF pages.
// This is used for the prepared file chooser collection (without annotations).
func (d *Document) AppendDocumentPages(collection *Document) error {
	if d.Format() != FormatPDF || collection == nil || collection.Format() != FormatPDF {
		return fmt.Errorf("document: append requires PDFs")
	}
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	before := d.snapshot()
	offset := len(d.pages)
	// Retain owners of detached annotation bases for their full lifetime,
	// including after the prepared collection is garbage collected.
	d.textSources = append(d.textSources, collection.source)
	d.textSources = append(d.textSources, collection.textSources...)
	pageIDs := make(map[uint64]uint64)
	for _, p := range collection.pages {
		oldID := p.id
		p.id = pageSequence.Add(1)
		pageIDs[oldID] = p.id
		d.pages = append(d.pages, p)
	}
	for _, overlay := range collection.signatures {
		overlay.ID = d.nextSignatureID
		d.nextSignatureID++
		overlay.Page += offset
		d.signatures = append(d.signatures, overlay)
	}
	for _, r := range collection.Redactions() {
		d.nextRedactionID++
		r.ID, r.PageID = d.nextRedactionID, pageIDs[r.PageID]
		d.redactions = append(d.redactions, r)
	}
	d.record(before)
	return nil
}

func sourcePages(source *Source) []pageRef {
	if source.annotationBase != nil {
		source = source.annotationBase
	}
	pages := make([]pageRef, source.PageCount())
	for i := range pages {
		pages[i] = pageRef{source: source, index: i, id: pageSequence.Add(1)}
	}
	return pages
}
func (d *Document) pageSource(page int) (*Source, int, error) {
	if err := d.validatePage(page); err != nil {
		return nil, 0, err
	}
	if d.Format() == FormatPDF {
		p := d.pages[page]
		return p.source, p.index, nil
	}
	return d.source, page, nil
}
func (d *Document) PageIDs() []uint64 {
	ids := make([]uint64, len(d.pages))
	for i, p := range d.pages {
		ids[i] = p.id
	}
	return ids
}
func (d *Document) PageLabel(page int) string {
	source, index, err := d.pageSource(page)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s · %d", source.Name(), index+1)
}
func (d *Document) AppendPDFs(sources ...*Source) error {
	if d.Format() != FormatPDF {
		return fmt.Errorf("document: open a PDF before appending pages")
	}
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	for _, s := range sources {
		if s == nil || s.Format() != FormatPDF || s.PageCount() == 0 {
			return fmt.Errorf("document: only PDF sources can be appended")
		}
	}
	before := d.snapshot()
	for _, s := range sources {
		if s.annotationBase != nil {
			d.textSources = append(d.textSources, s)
		}
		offset := len(d.pages)
		d.pages = append(d.pages, sourcePages(s)...)
		d.importSourceOverlays(s, offset)
	}
	d.record(before)
	return nil
}

// SetPageOrder accepts existing page indices in the desired order. Omitted
// pages are deleted; duplicates and an empty document are rejected atomically.
func (d *Document) SetPageOrder(order []int) error {
	if d.Format() != FormatPDF {
		return fmt.Errorf("document: page editing requires a PDF")
	}
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	if len(order) == 0 {
		return fmt.Errorf("document: keep at least one page")
	}
	mapping := make(map[int]int, len(order))
	pages := make([]pageRef, len(order))
	for i, old := range order {
		if err := d.validatePage(old); err != nil {
			return err
		}
		if _, exists := mapping[old]; exists {
			return fmt.Errorf("document: duplicate page %d", old+1)
		}
		mapping[old] = i
		pages[i] = d.pages[old]
	}
	before := d.snapshot()
	overlays := make([]SignatureOverlay, 0, len(d.signatures))
	for _, o := range d.signatures {
		if page, keep := mapping[o.Page]; keep {
			o.Page = page
			overlays = append(overlays, o)
		}
	}
	var redactions []Redaction
	for _, r := range d.redactions {
		for _, p := range pages {
			if p.id == r.PageID {
				redactions = append(redactions, r)
				break
			}
		}
	}
	d.pages, d.signatures, d.redactions = pages, overlays, redactions
	d.record(before)
	return nil
}

func (d *Document) basePDF() ([]byte, error) {
	// Native edits replace the complete page collection with a new source while
	// d.source retains the original bytes. Reuse any complete, ordered source:
	// collecting its pages again needlessly drops the logical structure tree.
	source := d.source
	if len(d.pages) > 0 {
		source = d.pages[0].source
	}
	unchanged := len(d.pages) == source.PageCount()
	for i, p := range d.pages {
		if p.source != source || p.index != i || p.turns != 0 {
			unchanged = false
			break
		}
	}
	if unchanged {
		return d.sourcePDF(source)
	}
	ctx := context.Background()
	var chunks []io.ReadSeeker
	for start := 0; start < len(d.pages); {
		source := d.pages[start].source
		end := start
		var selection []string
		for end < len(d.pages) && d.pages[end].source == source {
			selection = append(selection, strconv.Itoa(d.pages[end].index+1))
			end++
		}
		var chunk bytes.Buffer
		original, err := d.sourcePDF(source)
		if err != nil {
			return nil, err
		}
		if err := collectPDFPages(ctx, original, &chunk, selection); err != nil {
			return nil, fmt.Errorf("document: collect PDF pages: %w", err)
		}
		chunks = append(chunks, bytes.NewReader(chunk.Bytes()))
		start = end
	}
	var data []byte
	var err error
	if len(chunks) == 1 {
		data, err = io.ReadAll(chunks[0])
	} else {
		var output bytes.Buffer
		if err = api.MergeRaw(ctx, chunks, &output, false, nil); err == nil {
			data = output.Bytes()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("document: merge PDF pages: %w", err)
	}
	for turns := 1; turns < 4; turns++ {
		var selection []string
		for page, ref := range d.pages {
			if ref.turns == turns {
				selection = append(selection, strconv.Itoa(page+1))
			}
		}
		if len(selection) == 0 {
			continue
		}
		var rotated bytes.Buffer
		if err := api.Rotate(ctx, bytes.NewReader(data), &rotated, turns*90, selection, nil); err != nil {
			return nil, fmt.Errorf("document: rotate PDF pages: %w", err)
		}
		data = rotated.Bytes()
	}
	return data, nil
}

// DuplicatePage keeps the immutable source, but assigns independent editing IDs.
func (d *Document) DuplicatePage(page int) error { return d.DuplicatePages([]int{page}) }

// DuplicatePages inserts an ordered group of copies after the last selected page.
// Validation and all copies form one atomic history operation.
func (d *Document) DuplicatePages(selection []int) error {
	if d.Format() != FormatPDF {
		return fmt.Errorf("document: page duplication requires a PDF")
	}
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	if len(selection) == 0 {
		return fmt.Errorf("document: select at least one page")
	}
	selection = slices.Clone(selection)
	slices.Sort(selection)
	for i, page := range selection {
		if err := d.validatePage(page); err != nil {
			return err
		}
		if i > 0 && selection[i-1] == page {
			return fmt.Errorf("document: duplicate page selection")
		}
	}
	before := d.snapshot()
	position := selection[len(selection)-1] + 1
	pages := make([]pageRef, 0, len(d.pages)+len(selection))
	pages = append(pages, d.pages[:position]...)
	copies := make(map[int]int, len(selection))
	copiedIDs := make(map[uint64]uint64, len(selection))
	for _, page := range selection {
		p := d.pages[page]
		oldID := p.id
		p.id = pageSequence.Add(1)
		copies[page] = len(pages)
		copiedIDs[oldID] = p.id
		pages = append(pages, p)
	}
	pages = append(pages, d.pages[position:]...)
	overlays := make([]SignatureOverlay, 0, len(d.signatures))
	var copiedOverlays []SignatureOverlay
	for _, o := range d.signatures {
		if page, ok := copies[o.Page]; ok {
			c := o
			c.ID = d.nextSignatureID
			d.nextSignatureID++
			c.Page = page
			copiedOverlays = append(copiedOverlays, c)
		}
		if o.Page >= position {
			o.Page += len(selection)
		}
		overlays = append(overlays, o)
	}
	redactions := slices.Clone(d.redactions)
	for _, r := range d.redactions {
		if id, ok := copiedIDs[r.PageID]; ok {
			d.nextRedactionID++
			r.ID = d.nextRedactionID
			r.PageID = id
			redactions = append(redactions, r)
		}
	}
	d.pages = pages
	d.signatures = append(overlays, copiedOverlays...)
	d.redactions = redactions
	d.record(before)
	return nil
}

// Imported payloads and appearances are immutable, just like newly created ones.
func (d *Document) importSourceOverlays(source *Source, offset int) {
	for _, o := range source.importedOverlays {
		o.ID = d.nextSignatureID
		d.nextSignatureID++
		o.Page += offset
		d.signatures = append(d.signatures, o)
	}
}
