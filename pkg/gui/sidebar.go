package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"image/color"
)

type Sidebar struct {
	container *container.Scroll
	panel     *fyne.Container
	viewport  *fyne.Container
	documents *container.Scroll
	g         *Gui
}

func NewSidebar(g *Gui) *Sidebar {

	if g.documentList == nil {
		g.documentList = container.NewVBox()
	}
	documents := container.NewVScroll(g.documentList)
	documents.SetMinSize(fyne.NewSize(0, 140))
	documents.Hide()
	scroll := container.NewVScroll(g.thumbList)
	scroll.SetMinSize(fyne.NewSize(0, 0))
	scroll.Hide()

	viewport := container.New(&thumbnailViewportLayout{g: g}, container.NewThemeOverride(scroll, thumbnailScrollTheme{Theme: fyne.CurrentApp().Settings().Theme()}))
	panel := container.NewBorder(documents, nil, nil, nil, viewport)
	panel.Hide()
	return &Sidebar{
		panel:     panel,
		viewport:  viewport,
		documents: documents,
		g:         g,
		container: scroll,
	}
}

// Visibility is known only after layout. A document may open while this
// viewport still has zero height, and window resizing exposes more thumbnails
// without producing a scroll event. Coalesce a retry after layout completes.
type thumbnailViewportLayout struct {
	g      *Gui
	size   fyne.Size
	queued bool
}

func (l *thumbnailViewportLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return objects[0].MinSize()
}

func (l *thumbnailViewportLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
	if size == l.size {
		return
	}
	l.size = size
	if l.queued || size.Height <= 0 || size.Width <= 0 {
		return
	}
	l.queued = true
	fyne.Do(func() {
		l.queued = false
		if !l.g.closed {
			l.g.loadVisiblePDFThumbnails()
		}
	})
}

// Hide both scrollbar states locally; wheel/trackpad scrolling and drag-edge
// autoscrolling still use the original scroll container and its full viewport.
type thumbnailScrollTheme struct{ fyne.Theme }

func (t thumbnailScrollTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameScrollBar || name == theme.SizeNameScrollBarSmall {
		return 0
	}
	return t.Theme.Size(name)
}
func (t thumbnailScrollTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameScrollBar || name == theme.ColorNameScrollBarBackground {
		return color.Transparent
	}
	return t.Theme.Color(name, variant)
}
