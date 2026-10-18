package document

import (
	"bytes"
	"emanuelelongo/proview/pkg/annotations"
	img "emanuelelongo/proview/pkg/images"
	"emanuelelongo/proview/pkg/signatures"
	"encoding/json"
	"fmt"
	"image"
	"math"
)

// CopiedObject is a source-independent snapshot, not a reference to a document.
// Dimensions use PDF points or image pixels, regardless of display zoom.
type CopiedObject struct {
	Version       int
	Kind          string
	Rect          NormalizedRect
	Width, Height float64
	StrokeWidth   int
	Transform     img.Transform
	Asset         signatures.Asset
	Annotation    *annotations.Data
}

func (d *Document) CopyObject(id OverlayID) (CopiedObject, error) {
	i := d.signatureIndex(id)
	if i < 0 {
		return CopiedObject{}, ErrSignatureNotFound
	}
	if d.ObjectIntersectsRedaction(id) {
		return CopiedObject{}, fmt.Errorf("The object overlaps a redaction; copy its visible area instead")
	}
	s := d.signatures[i]
	size, e := d.PageSize(s.Page)
	if e != nil {
		return CopiedObject{}, e
	}
	unit := 1.
	if d.Format() == FormatPDF {
		unit = .5
	}
	c := CopiedObject{Version: 1, Kind: s.ObjectKind, Rect: s.Rect, Width: s.Rect.Width * float64(size.X) * unit, Height: s.Rect.Height * float64(size.Y) * unit, StrokeWidth: s.StrokeWidth, Transform: s.Transform, Asset: s.Asset()}
	if s.annotation != nil {
		data := s.annotation.Clone()
		if len(s.pdfAppearance) > 0 {
			asset, _, err := data.Artwork()
			if err != nil {
				return CopiedObject{}, err
			}
			c.Asset = asset
		}
		c.Annotation = &data
	}
	return c, nil
}
func (c CopiedObject) Encode() ([]byte, error) { return json.Marshal(c) }
func DecodeCopiedObject(data []byte) (CopiedObject, error) {
	if len(data) > 24<<20 {
		return CopiedObject{}, fmt.Errorf("Copied object is too large")
	}
	var c CopiedObject
	if e := json.Unmarshal(data, &c); e != nil {
		return c, e
	}
	if c.Version != 1 || !c.Rect.Valid() || c.Width <= 0 || c.Height <= 0 || math.IsNaN(c.Width+c.Height) || math.IsInf(c.Width+c.Height, 0) || c.Transform.Turns < 0 || c.Transform.Turns > 3 || c.StrokeWidth < 0 || c.StrokeWidth > 10 {
		return c, fmt.Errorf("Invalid copied object")
	}
	switch c.Kind {
	case "image", "signature":
		if c.Annotation != nil {
			return c, fmt.Errorf("Invalid copied object kind")
		}
	case "text", "drawing", "highlight", "shape":
		if c.Annotation == nil || string(c.Annotation.Kind) != c.Kind {
			return c, fmt.Errorf("Invalid copied annotation")
		}
	default:
		return c, fmt.Errorf("Unsupported copied object")
	}
	if c.Annotation != nil {
		if e := c.Annotation.Validate(); e != nil {
			return c, e
		}
	}
	return c, c.Asset.Validate()
}

// PasteObject allocates a new ID and records exactly one history operation.
func (d *Document) PasteObject(page int, rect NormalizedRect, c CopiedObject, prepared signatures.Prepared) (Overlay, error) {
	if d.activeEdit != nil {
		return Overlay{}, ErrEditInProgress
	}
	if e := d.validatePage(page); e != nil {
		return Overlay{}, e
	}
	if !rect.Valid() {
		return Overlay{}, ErrInvalidRect
	}
	if prepared.Preview == nil || prepared.Preview.Bounds().Empty() {
		return Overlay{}, fmt.Errorf("Copied object is not prepared")
	}
	before := d.snapshot()
	asset := prepared.Asset.Clone()
	o := Overlay{ID: d.nextSignatureID, Page: page, Rect: rect, StrokeWidth: c.StrokeWidth, Transform: c.Transform, ObjectKind: c.Kind, png: bytes.Clone(prepared.PNG), preview: prepared.Preview, asset: &asset}
	if c.Annotation != nil {
		data := c.Annotation.Clone()
		size, e := d.PageSize(page)
		if e != nil {
			return Overlay{}, e
		}
		unit := 1.
		if d.Format() == FormatPDF {
			unit = .5
		}
		factor := rect.Width * float64(size.X) * unit / c.Width
		data = data.Scaled(factor)
		if e = data.Validate(); e != nil {
			return Overlay{}, e
		}
		o.annotation = &data
	}
	d.nextSignatureID++
	d.signatures = append(d.signatures, o)
	d.record(before)
	return o, nil
}

func (d *Document) ObjectImage(id OverlayID) (image.Image, error) {
	i := d.signatureIndex(id)
	if i < 0 {
		return nil, ErrSignatureNotFound
	}
	if d.ObjectIntersectsRedaction(id) {
		return nil, fmt.Errorf("The object overlaps a redaction; copy its visible area instead")
	}
	s := d.signatures[i]
	if s.IsLens() {
		return d.RenderLens(id, 144)
	}
	if len(s.pdfAppearance) > 0 {
		return s.Preview(), nil
	}
	if s.IsVector() {
		size, e := d.PageSize(s.Page)
		if e != nil {
			return nil, e
		}
		a, e := s.asset.Transformed(s.Transform)
		if e != nil {
			return nil, e
		}
		return a.Rasterize(max(1, int(math.Round(s.Rect.Width*float64(size.X)))), max(1, int(math.Round(s.Rect.Height*float64(size.Y)))))
	}
	return s.Preview(), nil
}

// Redacted intersections must be copied through SafeRenderPage, never raw assets.
func (d *Document) ObjectIntersectsRedaction(id OverlayID) bool {
	i := d.signatureIndex(id)
	if i < 0 {
		return false
	}
	s := d.signatures[i]
	for _, r := range d.RedactionsForPage(s.Page) {
		for _, q := range r.Quads {
			x0, y0, x1, y1 := 1., 1., 0., 0.
			for _, p := range []TextPoint{q.UL, q.UR, q.LL, q.LR} {
				x0 = math.Min(x0, p.X)
				y0 = math.Min(y0, p.Y)
				x1 = math.Max(x1, p.X)
				y1 = math.Max(y1, p.Y)
			}
			if s.Rect.X < x1 && s.Rect.X+s.Rect.Width > x0 && s.Rect.Y < y1 && s.Rect.Y+s.Rect.Height > y0 {
				return true
			}
		}
	}
	return false
}
