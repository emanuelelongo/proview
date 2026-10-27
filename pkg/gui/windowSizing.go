package gui

import (
	"emanuelelongo/proview/pkg/document"
	"math"
	"os"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// Keep a fresh file window hidden until the opening worker has supplied its
// geometry. Center only before its first Show, never after loading into an
// already visible editor.
func (g *Gui) prepareWindowOpening(paths []string) {
	if len(paths) > 0 && !g.windowShown {
		g.showAfterOpening = true
		return
	}
	g.showWindow()
}

func (g *Gui) showWindow() {
	if g.closed {
		return
	}
	if !g.windowShown {
		g.window.CenterOnScreen()
		g.windowShown = true
	}
	g.showAfterOpening = false
	g.window.Show()
}

func (g *Gui) showPreparedWindow() {
	if g.showAfterOpening {
		g.showWindow()
	}
}

func windowWorkArea(window fyne.Window) fyne.Size {
	if native, ok := window.(driver.NativeWindow); ok {
		var area fyne.Size
		native.RunNative(func(context any) { area = nativeWindowWorkArea(context, window.Canvas().Scale()) })
		if area.Width > 0 && area.Height > 0 {
			return area
		}
	}
	return fyne.NewSize(1280, 960)
}

// Before native creation Canvas.Scale is still 1. Apply the selected monitor's
// scale and Fyne's user setting when converting its work area to logical units;
// otherwise the first requested frame can exceed the screen and the window
// manager resizes/repositions it after mapping. Fyne 2.7 rounds scale to tenths.
func initialWindowScale(system float32) float32 {
	user := float32(1)
	value := os.Getenv("FYNE_SCALE")
	if value != "auto" && fyne.CurrentApp().Settings().Scale() > 0 {
		user = fyne.CurrentApp().Settings().Scale()
	}
	if parsed, err := strconv.ParseFloat(value, 32); err == nil && parsed > 0 {
		user = float32(parsed)
	}
	return max(.1, float32(math.Round(float64(max(1, system)*user*10)))/10)
}

// Opening is separate from workspace activation: switching retained documents
// must not undo a manually chosen window size. Fullscreen/fixed windows keep
// their geometry. Markdown has a continuous layout, so use an A4 reading ratio.
func (g *Gui) sizeWindowForOpenedFile() {
	if g.window.FullScreen() || g.window.FixedSize() || g.showingModel() {
		return
	}
	var area fyne.Size
	if g.windowWorkArea != nil {
		area = g.windowWorkArea()
	} else {
		area = windowWorkArea(g.window)
	}
	chrome := fyne.NewSize(64, 88)
	canvasSize, bodySize := g.window.Canvas().Size(), g.mainSplit.Size()
	if bodySize.Width > 0 && bodySize.Height > 0 {
		chrome = fyne.NewSize(max(48, canvasSize.Width-bodySize.Width), max(72, canvasSize.Height-bodySize.Height))
	}
	if g.showingMarkdown() && g.rightPanel != nil {
		chrome.Width = max(chrome.Width, g.rightPanel.MinSize().Width+16)
	}
	if g.sidebar.panel.Visible() {
		chrome.Width += g.sidebarSplit.width + sidebarDividerWidth
	}
	page := fyne.Size{}
	if g.showingMarkdown() {
		page = fyne.NewSize(210, 297)
	} else if g.document != nil && g.document.Format() == document.FormatPDF {
		if size, err := g.document.PageSize(0); err == nil {
			page = fyne.NewSize(float32(size.X), float32(size.Y))
		}
	}
	size := openingWindowSize(page, chrome, area, g.window.Content().MinSize())
	g.openingFitDocument = g.document
	g.window.Resize(size)
	if g.document != nil {
		g.fitNewDocument()
		if size == canvasSize {
			g.openingFitDocument = nil
		}
	}
}

func openingWindowSize(page, chrome, area, minimum fyne.Size) fyne.Size {
	available := fyne.NewSize(max(1, area.Width-32), max(1, area.Height-48))
	size := fyne.NewSize(1000, 740)
	if page.Width > 0 && page.Height > 0 {
		size.Height = min(1120, available.Height)
		size.Width = max(1, size.Height-chrome.Height)*page.Width/page.Height + chrome.Width
		if size.Width > available.Width {
			size.Width = available.Width
			size.Height = max(1, size.Width-chrome.Width)*page.Height/page.Width + chrome.Height
		}
	}
	return fyne.NewSize(min(available.Width, max(minimum.Width, size.Width)), min(available.Height, max(minimum.Height, size.Height)))
}
