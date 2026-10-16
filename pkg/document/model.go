package document

import (
	"bytes"
	"emanuelelongo/proview/pkg/annotations"
	"errors"
	"fmt"
	"image"
	"image/png"
	"maps"
	"math"
	"reflect"
	"slices"
	"sync/atomic"

	img "emanuelelongo/proview/pkg/images"
	"emanuelelongo/proview/pkg/signatures"
)

var (
	ErrCropUnsupported   = errors.New("document: crop is only supported for images")
	ErrEditInProgress    = errors.New("document: another edit is already in progress")
	ErrInvalidPage       = errors.New("document: page out of range")
	ErrInvalidRect       = errors.New("document: invalid normalized rectangle")
	ErrInvalidStroke     = errors.New("document: signature stroke must be between 0 and 10")
	ErrSignatureNotFound = errors.New("document: signature not found")
)

const maxHistoryEntries = 100

type SignatureID uint64

type NormalizedRect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

func NewNormalizedRect(x, y, width, height float64) (NormalizedRect, error) {
	rect := NormalizedRect{X: x, Y: y, Width: width, Height: height}
	if !rect.Valid() {
		return NormalizedRect{}, ErrInvalidRect
	}
	return rect, nil
}

func (r NormalizedRect) Valid() bool {
	values := [...]float64{r.X, r.Y, r.Width, r.Height}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return r.X >= 0 && r.Y >= 0 && r.Width > 0 && r.Height > 0 &&
		r.X+r.Width <= 1 && r.Y+r.Height <= 1
}

type SignatureOverlay struct {
	ID          SignatureID
	Page        int
	Rect        NormalizedRect
	Transform   img.Transform
	ObjectKind  string
	StrokeWidth int
	png         []byte
	preview     image.Image
	asset       *signatures.Asset
	annotation  *annotations.Data
}

// Overlay and OverlayID are the common interfaces; signature APIs remain valid.
type Overlay = SignatureOverlay
type OverlayID = SignatureID

func (s SignatureOverlay) Annotation() (annotations.Data, bool) {
	if s.annotation == nil {
		return annotations.Data{}, false
	}
	return s.annotation.Clone(), true
}

func (s SignatureOverlay) PNG() []byte {
	return bytes.Clone(s.png)
}

func (s SignatureOverlay) Preview() image.Image {
	if s.IsVector() {
		return s.Transform.Apply(s.preview)
	}
	return s.Transform.Apply(img.ThickenAlpha(s.preview, s.StrokeWidth))
}

func (s SignatureOverlay) IsVector() bool { return s.asset != nil && s.asset.Kind != signatures.PNG }

func (s SignatureOverlay) Asset() signatures.Asset {
	if s.asset != nil {
		return s.asset.Clone()
	}
	return signatures.Asset{Kind: signatures.PNG, Data: bytes.Clone(s.png)}
}

type Document struct {
	// Retain ownership even when history eviction drops an old correction version.
	// Rendering snapshots can still refer to those immutable sources.
	textSources     []*Source
	formValues      map[formKey][]string
	unsaved         bool
	pages           []pageRef
	source          *Source
	crop            *NormalizedRect
	signatures      []SignatureOverlay
	nextSignatureID SignatureID
	undoStack       []historyEntry
	redactions      []Redaction
	nextRedactionID RedactionID
	redoStack       []historyEntry
	saved           editorState
	activeEdit      *signatureEdit
	imageTransform  img.Transform
	revision        atomic.Uint64
}

type editorState struct {
	formValues      map[formKey][]string
	imageTransform  img.Transform
	pages           []pageRef
	crop            *NormalizedRect
	signatures      []SignatureOverlay
	nextSignatureID SignatureID
	redactions      []Redaction
	nextRedactionID RedactionID
}

type historyEntry struct {
	before editorState
	after  editorState
}

type signatureEdit struct {
	id     SignatureID
	before editorState
}

func New(source *Source) (*Document, error) {
	if source == nil || source.PageCount() == 0 {
		return nil, errors.New("document: source has no pages")
	}
	doc := &Document{source: source, nextSignatureID: 1}
	if source.Format() == FormatPDF {
		doc.pages = sourcePages(source)
	}
	doc.saved = doc.snapshot()
	return doc, nil
}

func (d *Document) Name() string {
	return d.source.Name()
}

func (d *Document) Location() string {
	return d.source.Location()
}

func (d *Document) Format() Format {
	return d.source.Format()
}

// SourceSize returns the size in bytes of the loaded file without copying its contents.
func (d *Document) SourceSize() int64 { return int64(len(d.source.original)) }

func (d *Document) Original() []byte {
	return d.source.Original()
}

func (d *Document) Close() {
	sources := map[*Source]bool{d.source: true}
	for _, source := range d.textSources {
		sources[source] = true
	}
	collect := func(pages []pageRef) {
		for _, page := range pages {
			sources[page.source] = true
		}
	}
	collect(d.pages)
	collect(d.saved.pages)
	for _, entry := range d.undoStack {
		collect(entry.before.pages)
		collect(entry.after.pages)
	}
	for _, entry := range d.redoStack {
		collect(entry.before.pages)
		collect(entry.after.pages)
	}
	for source := range sources {
		source.Close()
	}
}

// MarkUnsaved is for generated files that have no saved destination yet.
func (d *Document) MarkUnsaved() { d.unsaved = true }

func (d *Document) PageCount() int {
	if d.Format() == FormatPDF {
		return len(d.pages)
	}
	return d.source.PageCount()
}

func (d *Document) Preview(page int) (image.Image, error) {
	source, index, err := d.pageSource(page)
	if err != nil {
		return nil, err
	}
	preview, changed, err := d.formPreview(source, index, pdfPreviewDPI)
	if !changed {
		preview, err = source.Preview(index)
	}
	if err == nil && d.Format() == FormatPDF {
		preview = (img.Transform{Turns: d.pages[page].turns}).Apply(preview)
	}
	return preview, err
}

func (d *Document) Thumbnail(page int) (image.Image, error) {
	source, index, err := d.pageSource(page)
	if err != nil {
		return nil, err
	}
	thumbnail, changed, err := d.formPreview(source, index, pdfThumbnailDPI)
	if !changed {
		thumbnail, err = source.Thumbnail(index)
	}
	if err != nil {
		return nil, err
	}
	if d.Format() == FormatPDF {
		thumbnail = (img.Transform{Turns: d.pages[page].turns}).Apply(thumbnail)
	}
	return d.paintRedactions(page, thumbnail), nil
}

func (d *Document) PageSize(page int) (image.Point, error) {
	source, index, err := d.pageSource(page)
	if err != nil {
		return image.Point{}, err
	}
	size, err := source.PageSize(index)
	if d.Format() == FormatPDF {
		if d.pages[page].turns%2 != 0 {
			size.X, size.Y = size.Y, size.X
		}
	} else {
		if d.crop != nil {
			size = normalizedImageRect(*d.crop, image.Rect(0, 0, size.X, size.Y)).Size()
		}
		if d.imageTransform.Turns%2 != 0 {
			size.X, size.Y = size.Y, size.X
		}
	}

	return size, err
}

func (d *Document) RenderBasePage(page int) (image.Image, error) {
	if err := d.validatePage(page); err != nil {
		return nil, err
	}
	preview, err := d.Preview(page)
	if err != nil {
		return nil, err
	}

	bounds := preview.Bounds()
	if page == 0 && d.Format() != FormatPDF && d.crop != nil {
		bounds = normalizedImageRect(*d.crop, bounds)
	}
	rendered := d.imageTransform.Apply(img.Crop(preview, bounds))
	return rendered, nil
}

func (d *Document) Dirty() bool {
	if d.unsaved {
		return true
	}
	return !editorStatesEqual(d.snapshot(), d.saved, false)
}

func (d *Document) MarkSaved() {
	d.unsaved = false
	d.saved = d.snapshot()
}

func (d *Document) Revision() uint64 {
	return d.revision.Load()
}

func (d *Document) CanUndo() bool {
	return d.activeEdit == nil && len(d.undoStack) > 0
}

func (d *Document) CanRedo() bool {
	return d.activeEdit == nil && len(d.redoStack) > 0
}

func (d *Document) Undo() bool {
	if !d.CanUndo() {
		return false
	}
	index := len(d.undoStack) - 1
	entry := d.undoStack[index]
	d.undoStack = d.undoStack[:index]
	d.restore(entry.before)
	d.redoStack = append(d.redoStack, entry)
	d.revision.Add(1)
	return true
}

func (d *Document) Redo() bool {
	if !d.CanRedo() {
		return false
	}
	index := len(d.redoStack) - 1
	entry := d.redoStack[index]
	d.redoStack = d.redoStack[:index]
	d.restore(entry.after)
	d.undoStack = append(d.undoStack, entry)
	d.revision.Add(1)
	return true
}

func (d *Document) Crop() (NormalizedRect, bool) {
	if d.crop == nil {
		return NormalizedRect{}, false
	}
	return *d.crop, true
}

func (d *Document) ApplyCrop(selection, currentBounds image.Rectangle) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	if d.Format() == FormatPDF {
		return ErrCropUnsupported
	}
	if currentBounds.Empty() {
		return fmt.Errorf("%w: empty image bounds", ErrInvalidRect)
	}

	selection = selection.Canon().Intersect(currentBounds)
	if selection.Empty() {
		return ErrInvalidRect
	}

	current := NormalizedRect{Width: 1, Height: 1}
	if d.crop != nil {
		current = *d.crop
	}

	width := float64(currentBounds.Dx())
	height := float64(currentBounds.Dy())
	relativeX := float64(selection.Min.X-currentBounds.Min.X) / width
	relativeY := float64(selection.Min.Y-currentBounds.Min.Y) / height
	relativeWidth := float64(selection.Dx()) / width
	relativeHeight := float64(selection.Dy()) / height

	relative := NormalizedRect{X: relativeX, Y: relativeY, Width: relativeWidth, Height: relativeHeight}
	for j := 0; j < d.imageTransform.Turns; j++ {
		relative = transformRect(relative, "left")
	}
	if d.imageTransform.FlipX {
		relative = transformRect(relative, "horizontal")
	}
	if d.imageTransform.FlipY {
		relative = transformRect(relative, "vertical")
	}
	relativeX, relativeY, relativeWidth, relativeHeight = relative.X, relative.Y, relative.Width, relative.Height
	next := NormalizedRect{
		X:      current.X + relativeX*current.Width,
		Y:      current.Y + relativeY*current.Height,
		Width:  relativeWidth * current.Width,
		Height: relativeHeight * current.Height,
	}
	if !next.Valid() {
		return ErrInvalidRect
	}
	if next == current {
		return nil
	}

	before := d.snapshot()
	d.crop = &next
	d.fitImageSignatureAspectRatios()
	d.record(before)
	return nil
}

func (d *Document) fitImageSignatureAspectRatios() {
	if d.Format() == FormatPDF {
		return
	}
	preview, err := d.source.Preview(0)
	if err != nil {
		return
	}
	pageBounds := preview.Bounds()
	if d.crop != nil {
		pageBounds = normalizedImageRect(*d.crop, pageBounds)
	}
	if pageBounds.Empty() {
		return
	}
	pageWidth, pageHeight := float64(pageBounds.Dx()), float64(pageBounds.Dy())
	if d.imageTransform.Turns%2 != 0 {
		pageWidth, pageHeight = pageHeight, pageWidth
	}
	for index := range d.signatures {
		signature := &d.signatures[index]
		bounds := signature.preview.Bounds()
		if signature.Page != 0 || bounds.Empty() {
			continue
		}
		aspect := float64(bounds.Dx()) / float64(bounds.Dy())
		if signature.Transform.Turns%2 != 0 {
			aspect = 1 / aspect
		}
		height := signature.Rect.Width * pageWidth / (aspect * pageHeight)
		width := signature.Rect.Width
		if height > 1 {
			height = 1
			width = height * pageHeight * aspect / pageWidth
		}
		centerX := signature.Rect.X + signature.Rect.Width/2
		centerY := signature.Rect.Y + signature.Rect.Height/2
		signature.Rect.Width = width
		signature.Rect.Height = height
		signature.Rect.X = math.Min(math.Max(centerX-width/2, 0), 1-width)
		signature.Rect.Y = math.Min(math.Max(centerY-height/2, 0), 1-height)
	}
}

func (d *Document) ClearCrop() bool {
	if d.crop == nil || d.activeEdit != nil {
		return false
	}
	before := d.snapshot()
	d.crop = nil
	d.fitImageSignatureAspectRatios()
	d.record(before)
	return true
}

func (d *Document) AddSignature(page int, rect NormalizedRect, pngData []byte) (SignatureOverlay, error) {
	return d.addRasterOverlay(page, rect, pngData, "signature")
}

// AddImage creates an independent raster object without signature-only controls.
func (d *Document) AddImage(page int, rect NormalizedRect, pngData []byte) (Overlay, error) {
	return d.addRasterOverlay(page, rect, pngData, "image")
}

func (d *Document) addRasterOverlay(page int, rect NormalizedRect, pngData []byte, kind string) (SignatureOverlay, error) {
	if d.activeEdit != nil {
		return SignatureOverlay{}, ErrEditInProgress
	}
	if err := d.validatePage(page); err != nil {
		return SignatureOverlay{}, err
	}
	if !rect.Valid() {
		return SignatureOverlay{}, ErrInvalidRect
	}
	preview, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return SignatureOverlay{}, fmt.Errorf("document: decode signature PNG: %w", err)
	}

	before := d.snapshot()
	signature := SignatureOverlay{
		ID:         d.nextSignatureID,
		Page:       page,
		Rect:       rect,
		ObjectKind: kind,
		png:        bytes.Clone(pngData),
		preview:    preview,
	}
	d.nextSignatureID++
	d.signatures = append(d.signatures, signature)
	d.record(before)
	return cloneSignature(signature), nil
}

func (d *Document) AddSignatureAsset(page int, rect NormalizedRect, prepared signatures.Prepared) (SignatureOverlay, error) {
	return d.addOverlay(page, rect, prepared, nil)
}

func (d *Document) AddAnnotation(page int, rect NormalizedRect, data annotations.Data, prepared signatures.Prepared) (Overlay, error) {
	if err := data.Validate(); err != nil {
		return Overlay{}, err
	}
	data = data.Clone()
	return d.addOverlay(page, rect, prepared, &data)
}

func (d *Document) addOverlay(page int, rect NormalizedRect, prepared signatures.Prepared, annotation *annotations.Data) (SignatureOverlay, error) {
	if d.activeEdit != nil {
		return SignatureOverlay{}, ErrEditInProgress
	}
	if err := d.validatePage(page); err != nil {
		return SignatureOverlay{}, err
	}
	if !rect.Valid() {
		return SignatureOverlay{}, ErrInvalidRect
	}
	if prepared.Preview == nil || prepared.Preview.Bounds().Empty() || len(prepared.PNG) == 0 {
		return SignatureOverlay{}, errors.New("document: signature is not prepared")
	}
	before := d.snapshot()
	asset := prepared.Asset.Clone()
	signature := SignatureOverlay{ID: d.nextSignatureID, Page: page, Rect: rect, png: bytes.Clone(prepared.PNG), preview: prepared.Preview, asset: &asset, annotation: annotation, ObjectKind: "signature"}
	if annotation != nil {
		signature.ObjectKind = string(annotation.Kind)
	}
	d.nextSignatureID++
	d.signatures = append(d.signatures, signature)
	d.record(before)
	return cloneSignature(signature), nil
}

func (d *Document) ReplaceSignatureAsset(id SignatureID, prepared signatures.Prepared) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	index := d.signatureIndex(id)
	if index < 0 {
		return ErrSignatureNotFound
	}
	if prepared.Preview == nil || prepared.Preview.Bounds().Empty() {
		return errors.New("document: signature is not prepared")
	}
	before := d.snapshot()
	asset := prepared.Asset.Clone()
	d.signatures[index].asset = &asset
	d.signatures[index].png = bytes.Clone(prepared.PNG)
	d.signatures[index].preview = prepared.Preview
	d.signatures[index].StrokeWidth = 0
	d.record(before)
	return nil
}

func (d *Document) BeginSignatureEdit(id SignatureID) error {
	if d.activeEdit != nil {
		if d.activeEdit.id == id {
			return nil
		}
		return ErrEditInProgress
	}
	if d.signatureIndex(id) < 0 {
		return ErrSignatureNotFound
	}
	d.activeEdit = &signatureEdit{id: id, before: d.snapshot()}
	return nil
}

func (d *Document) CommitSignatureEdit(id SignatureID) error {
	if d.activeEdit == nil || d.activeEdit.id != id {
		return ErrSignatureNotFound
	}
	before := d.activeEdit.before
	index := d.signatureIndex(id)
	if index >= 0 && d.signatures[index].annotation != nil {
		for _, old := range before.signatures {
			if old.ID == id {
				data := d.signatures[index].annotation.Scaled(d.signatures[index].Rect.Width / old.Rect.Width)
				if data.Kind == annotations.Shape {
					sx, sy := d.signatures[index].Rect.Width/old.Rect.Width, d.signatures[index].Rect.Height/old.Rect.Height
					if d.signatures[index].Transform.Turns%2 != 0 {
						sx, sy = sy, sx
					}
					data = old.annotation.Clone()
					for j := range data.Points {
						data.Points[j].X *= sx
						data.Points[j].Y *= sy
					}
					data.Width *= math.Min(sx, sy)
					data.Padding *= math.Min(sx, sy)
				}
				if err := data.Validate(); err != nil {
					return err
				}
				d.signatures[index].annotation = &data
				break
			}
		}
	}
	d.activeEdit = nil
	d.record(before)
	return nil
}

func (d *Document) CancelSignatureEdit(id SignatureID) error {
	if d.activeEdit == nil || d.activeEdit.id != id {
		return ErrSignatureNotFound
	}
	before := d.activeEdit.before
	d.activeEdit = nil
	d.restore(before)
	return nil
}

func (d *Document) UpdateSignature(id SignatureID, page int, rect NormalizedRect) error {
	if err := d.validatePage(page); err != nil {
		return err
	}
	if !rect.Valid() {
		return ErrInvalidRect
	}
	index := d.signatureIndex(id)
	if index < 0 {
		return ErrSignatureNotFound
	}
	signature := &d.signatures[index]
	if signature.Page == page && signature.Rect == rect {
		return nil
	}
	if d.activeEdit != nil && d.activeEdit.id != id {
		return ErrEditInProgress
	}
	var before editorState
	if d.activeEdit == nil {
		before = d.snapshot()
		if signature.annotation != nil && signature.Rect.Width != rect.Width {
			data := signature.annotation.Scaled(rect.Width / signature.Rect.Width)
			if err := data.Validate(); err != nil {
				return err
			}
			signature.annotation = &data
		}
	}
	signature.Page = page
	signature.Rect = rect
	if d.activeEdit == nil {
		d.record(before)
	}
	return nil
}

func (d *Document) RemoveSignature(id SignatureID) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	for index, signature := range d.signatures {
		if signature.ID != id {
			continue
		}
		before := d.snapshot()
		copy(d.signatures[index:], d.signatures[index+1:])
		d.signatures = d.signatures[:len(d.signatures)-1]
		d.record(before)
		return nil
	}
	return ErrSignatureNotFound
}

func (d *Document) SetSignatureStroke(id SignatureID, width int) error {
	if d.activeEdit != nil {
		return ErrEditInProgress
	}
	if width < 0 || width > 10 {
		return ErrInvalidStroke
	}
	index := d.signatureIndex(id)
	if index < 0 {
		return ErrSignatureNotFound
	}
	if d.signatures[index].IsVector() {
		return errors.New("document: edit vector stroke width through the drawing settings")
	}
	if d.signatures[index].StrokeWidth == width {
		return nil
	}
	before := d.snapshot()
	d.signatures[index].StrokeWidth = width
	d.record(before)
	return nil
}

func (d *Document) Signatures() []SignatureOverlay {
	result := make([]SignatureOverlay, len(d.signatures))
	for index, signature := range d.signatures {
		result[index] = cloneSignature(signature)
	}
	return result
}

func (d *Document) SignaturesForPage(page int) ([]SignatureOverlay, error) {
	if err := d.validatePage(page); err != nil {
		return nil, err
	}
	result := make([]SignatureOverlay, 0)
	for _, signature := range d.signatures {
		if signature.Page == page {
			result = append(result, cloneSignature(signature))
		}
	}
	return result, nil
}

func (d *Document) validatePage(page int) error {
	if page < 0 || page >= d.PageCount() {
		return fmt.Errorf("%w: %d", ErrInvalidPage, page)
	}
	return nil
}

func (d *Document) signatureIndex(id SignatureID) int {
	for index, signature := range d.signatures {
		if signature.ID == id {
			return index
		}
	}
	return -1
}

func (d *Document) snapshot() editorState {
	state := editorState{
		formValues:      cloneFormValues(d.formValues),
		imageTransform:  d.imageTransform,
		pages:           append([]pageRef(nil), d.pages...),
		signatures:      make([]SignatureOverlay, len(d.signatures)),
		nextSignatureID: d.nextSignatureID,
		redactions:      cloneRedactions(d.redactions),
		nextRedactionID: d.nextRedactionID,
	}
	if d.crop != nil {
		crop := *d.crop
		state.crop = &crop
	}
	for index, signature := range d.signatures {
		state.signatures[index] = cloneSignature(signature)
	}
	return state
}

func (d *Document) restore(state editorState) {
	d.formValues = cloneFormValues(state.formValues)
	d.imageTransform = state.imageTransform
	d.pages = append([]pageRef(nil), state.pages...)
	d.crop = nil
	if state.crop != nil {
		crop := *state.crop
		d.crop = &crop
	}
	d.signatures = make([]SignatureOverlay, len(state.signatures))
	for index, signature := range state.signatures {
		d.signatures[index] = cloneSignature(signature)
	}
	d.nextSignatureID = state.nextSignatureID
	d.redactions = cloneRedactions(state.redactions)
	d.nextRedactionID = state.nextRedactionID
}

func (d *Document) record(before editorState) {
	after := d.snapshot()
	if editorStatesEqual(before, after, true) {
		return
	}
	d.undoStack = append(d.undoStack, historyEntry{before: before, after: after})
	if len(d.undoStack) > maxHistoryEntries {
		copy(d.undoStack, d.undoStack[len(d.undoStack)-maxHistoryEntries:])
		d.undoStack = d.undoStack[:maxHistoryEntries]
	}
	d.redoStack = nil
	d.revision.Add(1)
}

func editorStatesEqual(first, second editorState, includeNextID bool) bool {
	if !maps.EqualFunc(first.formValues, second.formValues, slices.Equal[[]string]) {
		return false
	}
	if first.imageTransform != second.imageTransform {
		return false
	}
	if !slices.Equal(first.pages, second.pages) {
		return false
	}
	if includeNextID && first.nextSignatureID != second.nextSignatureID {
		return false
	}
	if !reflect.DeepEqual(first.redactions, second.redactions) || includeNextID && first.nextRedactionID != second.nextRedactionID {
		return false
	}
	if (first.crop == nil) != (second.crop == nil) ||
		first.crop != nil && *first.crop != *second.crop ||
		len(first.signatures) != len(second.signatures) {
		return false
	}
	for index, signature := range first.signatures {
		other := second.signatures[index]
		if signature.ID != other.ID || signature.Page != other.Page || signature.Rect != other.Rect ||
			signature.Transform != other.Transform || signature.ObjectKind != other.ObjectKind || signature.StrokeWidth != other.StrokeWidth ||
			!reflect.DeepEqual(signature.annotation, other.annotation) ||
			!reflect.DeepEqual(signature.asset, other.asset) ||
			!bytes.Equal(signature.png, other.png) {
			return false
		}
	}
	return true
}

func cloneSignature(signature SignatureOverlay) SignatureOverlay {
	if signature.annotation != nil {
		data := signature.annotation.Clone()
		signature.annotation = &data
	}
	signature.png = bytes.Clone(signature.png)
	if signature.asset != nil {
		asset := signature.asset.Clone()
		signature.asset = &asset
	}
	return signature
}

func normalizedImageRect(rect NormalizedRect, bounds image.Rectangle) image.Rectangle {
	width := float64(bounds.Dx())
	height := float64(bounds.Dy())
	return image.Rect(
		bounds.Min.X+int(math.Round(rect.X*width)),
		bounds.Min.Y+int(math.Round(rect.Y*height)),
		bounds.Min.X+int(math.Round((rect.X+rect.Width)*width)),
		bounds.Min.Y+int(math.Round((rect.Y+rect.Height)*height)),
	).Intersect(bounds)
}
