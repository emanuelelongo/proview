package gui

import (
	"net"
	"net/url"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// Use the existing canvas text so links retain the status bar's compact type
// size and do not change the window's minimum size or the document viewport.
type statusTextSurface struct {
	widget.BaseWidget
	editor  *Gui
	text    *canvas.Text
	tooltip *tooltipSurface
}

func newStatusTextSurface(g *Gui) *statusTextSurface {
	s := &statusTextSurface{editor: g, text: g.tipText}
	s.ExtendBaseWidget(s)
	g.tipSurface = s
	return s
}
func (s *statusTextSurface) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(s.text)
}
func (s *statusTextSurface) Cursor() desktop.Cursor {
	if s.editor.selectedTextURL() != nil {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}
func (s *statusTextSurface) Tapped(*fyne.PointEvent) {
	g := s.editor
	if target := g.selectedTextURL(); target != nil && g.app != nil {
		if err := g.app.OpenURL(target); err != nil {
			g.showOperationError("open_url", err)
		}
	}
}
func (s *statusTextSurface) TappedSecondary(event *fyne.PointEvent) {
	g := s.editor
	if g.closed {
		return
	}
	if g.debugEnabled && !g.selectionStatusActive {
		menu := widget.NewPopUpMenu(fyne.NewMenu("", fyne.NewMenuItem("Save memory report…", g.saveMemoryReport)), g.window.Canvas())
		menu.ShowAtPosition(event.AbsolutePosition)
		return
	}
	g.showSelectionCopyMenu(event.AbsolutePosition)
}

func (s *statusTextSurface) MouseIn(*desktop.MouseEvent) {
	g := s.editor
	if g.closed || !g.debugEnabled || g.selectionStatusActive {
		return
	}
	c := g.window.Canvas()
	if c.Overlays().Top() != nil {
		return
	}
	surface, ok := c.Content().(*tooltipSurface)
	if !ok {
		return
	}
	pos := g.app.Driver().AbsolutePositionForObject(s)
	root := g.app.Driver().AbsolutePositionForObject(surface)
	s.tooltip = surface
	surface.show(s, debugInfoTooltip, fyne.NewPos(pos.X-root.X, pos.Y-root.Y))
	// Place the bubble above the bottom bar.
	surface.position.Y -= surface.bubble.Size().Height + 4
	surface.positionBubble(surface.Size())
}
func (s *statusTextSurface) MouseMoved(*desktop.MouseEvent) {}
func (s *statusTextSurface) MouseOut() {
	if s.tooltip != nil {
		s.tooltip.hide(s)
		s.tooltip = nil
	}
}

func selectedURL(text string) *url.URL {
	candidate := strings.TrimSpace(text)
	if candidate == "" || strings.IndexFunc(candidate, unicode.IsSpace) >= 0 {
		return nil
	}
	// Sentence punctuation may be part of the OCR word, not of the link target.
	candidate = strings.TrimRight(candidate, ".,;")
	explicit := strings.Contains(candidate, "://")
	if !explicit {
		candidate = "https://" + candidate
	}
	target, err := url.Parse(candidate)
	if err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Hostname() == "" || target.User != nil {
		return nil
	}
	host := target.Hostname()
	if !explicit && net.ParseIP(host) == nil {
		labels := strings.Split(host, ".")
		if len(labels) < 2 {
			return nil
		}
		for _, label := range labels {
			if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return nil
			}
			for _, r := range label {
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
					return nil
				}
			}
		}
		// Prevent decimal numbers from turning into links.
		for _, r := range labels[len(labels)-1] {
			if !unicode.IsLetter(r) {
				return nil
			}
		}
	}
	return target
}
func (g *Gui) selectedTextURL() *url.URL {
	if g.closed || !g.validTextSelection() {
		return nil
	}
	s := g.textSelection
	text := s.layout.Text(s.start, s.end)
	if s.qrCode && strings.Contains(text, "://") {
		target, err := url.Parse(strings.TrimSpace(text))
		if err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Hostname() == "" || target.User != nil || strings.IndexFunc(strings.TrimSpace(text), unicode.IsSpace) >= 0 {
			return nil
		}
		return target
	}
	return selectedURL(text)
}

func (g *Gui) hideSelectionCopyMenu() {
	if g.selectionCopyMenu != nil {
		g.selectionCopyMenu.Hide()
		g.selectionCopyMenu = nil
	}
}
func (g *Gui) showSelectionCopyMenu(position fyne.Position) {
	if g.closed || g.window == nil || !g.validTextSelection() || g.textSelection.active {
		return
	}
	g.hideSelectionCopyMenu()
	selection := g.textSelection
	start, end := selection.start, selection.end
	copyItem := fyne.NewMenuItem("Copy", func() {
		// A menu can outlive its selection or source. Never copy an obsolete range.
		if !g.closed && g.validTextSelection() && g.textSelection == selection && selection.start == start && selection.end == end {
			g.window.Clipboard().SetContent(selection.layout.Text(start, end))
		}
		g.hideSelectionCopyMenu()
	})
	g.selectionCopyMenu = widget.NewPopUpMenu(fyne.NewMenu("", copyItem), g.window.Canvas())
	g.selectionCopyMenu.ShowAtPosition(position)
}
func (l *signatureLayer) TappedSecondary(event *fyne.PointEvent) {
	if l.showObjectOrderMenuAt(event) {
		return
	}
	g := l.editor
	if g == nil || !g.validTextSelection() || g.textSelection.layer != l {
		return
	}
	s := g.textSelection
	p := l.textPoint(event.Position)
	for _, word := range s.layout.Range(s.start, s.end) {
		if word.Quad.Contains(p) {
			g.showSelectionCopyMenu(event.AbsolutePosition)
			return
		}
	}
}
