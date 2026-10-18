package gui

import (
	"fmt"
	"fyne.io/fyne/v2/canvas"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"emanuelelongo/proview/pkg/signatures"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// A draft is view state until confirmed. Typing and style changes never enter
// document history, and confirmation creates or replaces one vector text object.
type textPlacement struct {
	original    *document.Overlay
	doc         *document.Document
	layer       *signatureLayer
	point       annotations.Point
	data        annotations.Data
	entry       *annotationEntry
	field       *container.ThemeOverride
	size        float32
	frame       *canvas.Rectangle
	preview     *canvas.Image
	metrics     annotations.TextMetrics
	rect        document.NormalizedRect
	requested   annotations.Data
	scale       float64
	running     bool
	ready       bool
	baseline    float64
	hasBaseline bool
}

func (g *Gui) beginTextPlacement(layer *signatureLayer, point annotations.Point) {
	if g.closed || g.document == nil || layer.document != g.document || g.effectiveAnnotationTool() != toolText {
		return
	}
	g.commitTextObject()
	g.activatePDFPage(layer.page)
	g.clearSignatureSelection()
	g.clearTextSelection()
	g.ensureAnnotationTools()
	data := g.annotationDefaults[annotations.Text].Clone()
	data.Text = ""
	data.PageWidth, data.PageHeight = g.annotationPageSize(layer)
	// Half the nominal height of size-18 text, in page units (PDF points or
	// original image pixels). Its visible displacement scales with view zoom.
	const insertionOffset = 18.0 / 2
	point.Y = math.Max(0, point.Y-insertionOffset)
	p := &textPlacement{doc: g.document, layer: layer, point: point, data: data, entry: newAnnotationEntry()}
	g.openTextDraft(p)
}

func (g *Gui) openTextDraft(p *textPlacement) {
	layer := p.layer
	p.entry.inline = true
	p.entry.Wrapping = fyne.TextWrapOff
	p.entry.Scroll = fyne.ScrollNone
	p.entry.SetPlaceHolder("Type here…")
	p.frame = canvas.NewRectangle(color.Transparent)
	p.frame.StrokeWidth = 1
	p.frame.CornerRadius = 2
	p.frame.StrokeColor = theme.Current().Color(theme.ColorNamePrimary, fyne.CurrentApp().Settings().ThemeVariant())
	p.preview = canvas.NewImageFromImage(nil)
	p.preview.FillMode = canvas.ImageFillStretch
	p.preview.Hide()
	p.entry.OnChanged = func(value string) {
		if g.textPlacement != p {
			return
		}
		clean := annotations.SingleLine(value)
		p.data.Text = clean
		if strings.TrimSpace(clean) == "" {
			p.ready = false
			p.preview.Hide()
		}
		if clean != value {
			p.entry.SetText(clean)
		}
		p.layer.Refresh()
	}
	p.entry.commit = func() {
		if g.textPlacement == p {
			g.finishTextPlacement()
		}
	}
	p.entry.cancel = func() {
		if g.textPlacement == p {
			g.cancelTextPlacement()
		}
	}
	p.entry.OnSubmitted = func(string) { p.entry.commit() }
	p.field = container.NewThemeOverride(p.entry, theme.Current())
	g.textPlacement = p
	g.refreshAnnotationControls()
	layer.Refresh()
	g.window.Canvas().Focus(p.entry)
}

// Existing text uses the same page editor, anchored to its saved artwork frame.
func (g *Gui) beginExistingText(layer *signatureLayer, id document.OverlayID) {
	if layer == nil || g.document == nil || g.closed || g.working {
		return
	}
	if p := g.textPlacement; p != nil && p.original != nil && p.original.ID == id {
		g.window.Canvas().Focus(p.entry)
		return
	}
	g.commitTextObject()
	var original document.Overlay
	found := false
	for _, o := range g.document.Overlays() {
		if o.ID == id {
			original = o
			found = true
			break
		}
	}
	data, ok := original.Annotation()
	if !found || !ok || data.Kind != annotations.Text {
		return
	}
	g.ensureAnnotationTools()
	data.PageWidth, data.PageHeight = g.annotationPageSize(layer)
	p := &textPlacement{doc: g.document, layer: layer, data: data, entry: newAnnotationEntry(), original: &original, point: annotations.Point{X: original.Rect.X * data.PageWidth, Y: original.Rect.Y * data.PageHeight}}
	p.entry.SetText(data.Text)
	g.openTextDraft(p)
}

func (g *Gui) detachTextPlacement() *textPlacement {
	p := g.textPlacement
	if p == nil {
		return nil
	}
	g.textPlacement = nil
	if g.textEntry == p.entry {
		g.textEntry = nil
	}
	if g.window.Canvas().Focused() == p.entry {
		g.window.Canvas().Focus(nil)
	}
	p.layer.Refresh()
	return p
}

func (g *Gui) finishTextPlacement() {
	p := g.detachTextPlacement()
	if p == nil {
		return
	}
	if p.original != nil {
		g.finishExistingText(p)
		return
	}
	if !g.closed && g.document == p.doc && p.layer.document == p.doc && strings.TrimSpace(p.data.Text) != "" {
		g.addPageAnnotationWithResult(p.layer, p.data.Clone(), p.point.X, p.point.Y, true, func(err error) {
			if err == nil || g.closed || g.document != p.doc {
				return
			}
			// A failed placement must not lose entered text or continue a queued
			// save/close operation as if the annotation had been created.
			g.annotationAfterIdle = nil
			if g.textPlacement == nil && g.annotationTool == toolText {
				g.textPlacement = p
				g.refreshAnnotationControls()
				p.layer.Refresh()
			}
		})
	}
}

func (g *Gui) finishExistingText(p *textPlacement) {
	if g.closed || g.document != p.doc {
		return
	}
	original := *p.original
	old, _ := original.Annotation()
	data := p.data.Clone()
	if equalAnnotationProperties(old, data) {
		return
	}
	if strings.TrimSpace(data.Text) == "" {
		if err := p.doc.RemoveOverlay(original.ID); err != nil {
			g.showAnnotationError(original.Page, data, err)
			return
		}
		p.layer.reload()
		g.clearSignatureSelection()
		g.documentChanged(original.Page)
		return
	}
	g.queueAnnotation(func() func() {
		asset, _, rect, err := p.artwork(data)
		var prepared signatures.Prepared
		if err == nil {
			prepared, err = asset.Prepare()
		}
		return func() {
			if g.closed || g.document != p.doc {
				return
			}
			if err == nil {
				err = p.doc.ReplaceAnnotation(original.ID, rect, data, prepared)
			}
			if err != nil {
				g.annotationAfterIdle = nil
				g.showAnnotationError(original.Page, data, err)
				if g.textPlacement == nil {
					g.textPlacement = p
					g.refreshAnnotationControls()
					p.layer.Refresh()
				}
				return
			}
			p.layer.reload()
			if g.hasSelectedSignature && g.selectedSignature == original.ID {
				p.layer.selectSignature(original.ID)
			}
			g.documentChanged(original.Page)
			g.refreshAnnotationControls()
		}
	})
}

// Existing text uses natural glyph dimensions and its visible ink anchor,
// without applying the insertion offset or scaling from its PDF layout box.
func (p *textPlacement) artwork(data annotations.Data) (signatures.Asset, annotations.TextMetrics, document.NormalizedRect, error) {
	asset, bounds, metrics, err := data.TextArtwork()
	if err != nil {
		return asset, metrics, document.NormalizedRect{}, err
	}
	rect, err := annotationPlacementRect(bounds.W(), bounds.H(), data.PageWidth, data.PageHeight, p.point.X-metrics.Left, p.point.Y-data.Padding)
	if p.original != nil {
		rect, err = naturalTextRect(*p.original, data, bounds.W(), bounds.H(), data.PageWidth, data.PageHeight)
	}
	// Keep the writing baseline fixed as ascenders/descenders change. The
	// artwork's tight ink bounds otherwise make the whole line jump while typing.
	if err == nil && p.hasBaseline && (p.original == nil || p.original.Transform.Turns == 0) {
		rect.Y = min(max((p.baseline-metrics.Baseline)/data.PageHeight, 0), 1-rect.Height)
	}
	return asset, metrics, rect, err
}

// FreeText Rect is a layout box, not a glyph scaling instruction. Regenerated
// text always uses its font size and one physical scale on both axes.
func naturalTextRect(original document.Overlay, data annotations.Data, width, height, pageWidth, pageHeight float64) (document.NormalizedRect, error) {
	rect := original.Rect
	if original.Asset().Kind == signatures.PNG {
		// Imported appearances may place text anywhere inside a large layout box.
		// Preserve the visible ink's anchor when replacing that box with glyphs.
		preview := original.Preview()
		if preview != nil {
			bounds := preview.Bounds()
			ink := image.Rectangle{}
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					_, _, _, a := preview.At(x, y).RGBA()
					if a > 0x1000 {
						p := image.Rect(x, y, x+1, y+1)
						if ink.Empty() {
							ink = p
						} else {
							ink = ink.Union(p)
						}
					}
				}
			}
			if !ink.Empty() {
				rect.X += float64(ink.Min.X-bounds.Min.X)/float64(bounds.Dx())*original.Rect.Width - data.Padding/pageWidth
				rect.Y += float64(ink.Min.Y-bounds.Min.Y)/float64(bounds.Dy())*original.Rect.Height - data.Padding/pageHeight
			}
		}
	}
	if original.Transform.Turns%2 != 0 {
		width, height = height, width
	}
	rect.Width, rect.Height = width/pageWidth, height/pageHeight
	if rect.Width > 1 || rect.Height > 1 {
		return rect, fmt.Errorf("The object is too large for the page")
	}
	rect.X = min(max(rect.X, 0), 1-rect.Width)
	rect.Y = min(max(rect.Y, 0), 1-rect.Height)
	return rect, nil
}

func (g *Gui) cancelTextPlacement() bool {
	p := g.detachTextPlacement()
	if p == nil {
		return false
	}
	g.refreshAnnotationControls()
	g.updateTemporarySelection()
	return true
}

// The click fixes the glyph origin horizontally and the visible ink's top
// vertically. The same padded bounds and page-edge clamp are used on commit.
func annotationPlacementRect(width, height, pageWidth, pageHeight, x, y float64) (document.NormalizedRect, error) {
	width, height = width/pageWidth, height/pageHeight
	if width > 1 || height > 1 {
		return document.NormalizedRect{}, fmt.Errorf("The object is too large for the page: reduce its size")
	}
	return document.NormalizedRect{X: math.Min(math.Max(x/pageWidth, 0), 1-width), Y: math.Min(math.Max(y/pageHeight, 0), 1-height), Width: width, Height: height}, nil
}

func (p *textPlacement) layout() {
	origin, display := p.layer.displayRect()
	if display.Width <= 0 || display.Height <= 0 {
		return
	}
	scale := float64(display.Width) / p.data.PageWidth
	size := float32(p.data.Size * scale)
	style := fyne.TextStyle{Bold: p.data.Bold, Italic: p.data.Italic}
	face := textPlacementFont(p.data.Font, style)
	t := textPlacementTheme{Theme: theme.Current(), size: size, face: face, ink: p.data.Color, hidden: p.ready}
	if old, ok := p.field.Theme.(textPlacementTheme); !ok || old.size != size || old.face != face || old.hidden != t.hidden || old.ink != t.ink {
		p.size = size
		p.entry.TextStyle = style
		p.field.Theme = t
		p.field.Refresh()
	}
	x, y := float32(p.point.X*scale), float32(p.point.Y*scale)
	// The transparent field stays visible before typing; its surrounding frame
	// has external padding so it never shifts the text away from the click.
	measured, baseline := fyne.CurrentApp().Driver().RenderedTextSize(p.entry.Text, size, style, face)
	if p.ready {
		position, dimensions := p.layer.widgetRect(p.rect)
		p.preview.Move(position)
		p.preview.Resize(dimensions)
		p.preview.Show()
		x = float32(p.rect.X*p.data.PageWidth*scale + p.metrics.Left*scale)
		y = float32(p.rect.Y*p.data.PageHeight*scale+p.metrics.Baseline*scale) - baseline
	} else {
		p.preview.Hide()
		// Before the first outline is ready, keep the caret near the clicked top.
		y -= size * .2
	}
	p.field.Move(origin.Add(fyne.NewPos(x, y)))
	p.field.Resize(fyne.NewSize(min(max(float32(160), measured.Width+size*.5), max(size, display.Width-x)), max(float32(24), max(size*1.5, p.entry.MinSize().Height))))
	const padding float32 = 4
	p.frame.Move(p.field.Position().Subtract(fyne.NewPos(padding, padding)))
	p.frame.Resize(p.field.Size().Add(fyne.NewSize(2*padding, 2*padding)))
	p.frame.Refresh()
	p.requestPreview(scale)
}

// Keep at most one outline/raster job per draft. Fast typing replaces the next
// requested state rather than queuing one render for every keystroke.
func (p *textPlacement) requestPreview(scale float64) {
	g := p.layer.editor
	if g == nil || g.closed || g.textPlacement != p || p.running {
		return
	}
	if p.scale == scale && equalAnnotationProperties(p.requested, p.data) {
		return
	}
	data := p.data.Clone()
	// Retain the last outline and field position until the replacement is ready.
	// Switching back to native Entry glyphs per keystroke visibly flickers.
	p.requested, p.scale = data, scale
	if strings.TrimSpace(data.Text) == "" {
		return
	}
	p.running = true
	g.queueAnnotation(func() func() {
		asset, metrics, rect, err := p.artwork(data)
		var picture image.Image
		if err == nil {
			width, height := rect.Width*data.PageWidth, rect.Height*data.PageHeight
			renderScale := math.Min(scale*2, 2048/max(width, height))
			rw, rh := max(1, int(math.Ceil(width*renderScale))), max(1, int(math.Ceil(height*renderScale)))
			if p.original != nil && p.original.Transform.Turns%2 != 0 {
				rw, rh = rh, rw
			}
			picture, err = asset.Rasterize(rw, rh)
			if err == nil && p.original != nil {
				picture = p.original.Transform.Apply(picture)
			}
		}
		return func() {
			p.running = false
			if g.closed || g.textPlacement != p || g.document != p.doc {
				return
			}
			if err == nil && equalAnnotationProperties(data, p.data) {
				if !p.hasBaseline {
					p.baseline = rect.Y*data.PageHeight + metrics.Baseline
					p.hasBaseline = true
				}
				p.metrics, p.rect, p.ready = metrics, rect, true
				p.preview.Image = picture
				p.preview.Refresh()
			}
			p.layer.Refresh()
		}
	})
}

var textPlacementFonts sync.Map

func textPlacementFont(name string, style fyne.TextStyle) fyne.Resource {
	key := fmt.Sprintf("%s/%t/%t", name, style.Bold, style.Italic)
	if cached, ok := textPlacementFonts.Load(key); ok {
		return cached.(fyne.Resource)
	}
	data, err := annotations.FontData(name, style.Bold, style.Italic)
	if err != nil {
		return theme.Current().Font(style)
	}
	resource := fyne.NewStaticResource(key+".ttf", data)
	cached, _ := textPlacementFonts.LoadOrStore(key, resource)
	return cached.(fyne.Resource)
}

type textPlacementTheme struct {
	fyne.Theme
	size   float32
	face   fyne.Resource
	hidden bool
	ink    color.NRGBA
}

func (t textPlacementTheme) Font(fyne.TextStyle) fyne.Resource { return t.face }
func (t textPlacementTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameInputBackground, theme.ColorNameInputBorder, theme.ColorNameHover:
		return color.Transparent
	}
	if name == theme.ColorNameForeground {
		if t.hidden {
			return color.Transparent
		}
		return t.ink
	}
	return t.Theme.Color(name, variant)
}
func (t textPlacementTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return t.size
	case theme.SizeNameInputBorder:
		// Fyne also uses this dimension for the caret width. The border color
		// stays transparent; zero would make the insertion cursor invisible.
		return 1
	case theme.SizeNameInnerPadding, theme.SizeNameLineSpacing:
		return 0
	}
	return t.Theme.Size(name)
}
