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
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// A draft is view state until confirmed. Typing and style changes never enter
// document history, and one confirmation creates one vector text object.
type textPlacement struct {
	doc       *document.Document
	layer     *signatureLayer
	point     annotations.Point
	data      annotations.Data
	entry     *annotationEntry
	field     *container.ThemeOverride
	size      float32
	frame     *canvas.Rectangle
	preview   *canvas.Image
	metrics   annotations.TextMetrics
	rect      document.NormalizedRect
	requested annotations.Data
	scale     float64
	running   bool
	ready     bool
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
	t := textPlacementTheme{Theme: theme.Current(), size: size, face: face, ink: p.data.Color, hidden: p.ready && equalAnnotationProperties(p.requested, p.data) && p.scale == scale}
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
	if p.ready && equalAnnotationProperties(p.requested, p.data) && p.scale == scale {
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
	p.requested, p.scale, p.ready = data, scale, false
	if strings.TrimSpace(data.Text) == "" {
		return
	}
	p.running = true
	g.queueAnnotation(func() func() {
		asset, bounds, metrics, err := data.TextArtwork()
		var rect document.NormalizedRect
		var picture image.Image
		if err == nil {
			rect, err = annotationPlacementRect(bounds.W(), bounds.H(), data.PageWidth, data.PageHeight, p.point.X-metrics.Left, p.point.Y-data.Padding)
		}
		if err == nil {
			renderScale := math.Min(scale*2, 2048/math.Max(bounds.W(), bounds.H()))
			picture, err = asset.Rasterize(max(1, int(math.Ceil(bounds.W()*renderScale))), max(1, int(math.Ceil(bounds.H()*renderScale))))
		}
		return func() {
			p.running = false
			if g.closed || g.textPlacement != p || g.document != p.doc {
				return
			}
			if err == nil && equalAnnotationProperties(data, p.data) {
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
	case theme.SizeNameInnerPadding, theme.SizeNameInputBorder, theme.SizeNameLineSpacing:
		return 0
	}
	return t.Theme.Size(name)
}
