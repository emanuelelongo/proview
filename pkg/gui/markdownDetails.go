package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (v *markdownView) details(n *markdown.Details) fyne.CanvasObject {
	if v.detailsOpen == nil {
		v.detailsOpen = make(map[*markdown.Details]bool)
	}
	open, known := v.detailsOpen[n]
	if !known {
		open = n.Open
		v.detailsOpen[n] = open
	}
	icon := "chevron-right"
	if open {
		icon = "chevron-down"
	}
	toggle := func() {
		if v.editor.markdownView != v || v.editor.closed {
			return
		}
		v.detailsOpen[n] = !v.detailsOpen[n]
		v.rebuild()
		v.editor.captureDocument()
		if v.viewport != nil {
			v.viewport.Refresh()
		}
	}
	button := newIconButton(theme.NewColoredResource(tablerIcon(icon), theme.ColorNameHyperlink), "Expand section", toggle)
	button.Importance = widget.LowImportance
	if open {
		button.tooltip = "Collapse section"
	}
	title := v.selectable(&widget.TextSegment{Text: n.Summary, Style: widget.RichTextStyleStrong})
	label := v.styleText(&title.RichText, title)
	// Keep summary text in the ordinary tree for search and viewport geometry.
	header := container.NewBorder(nil, nil, button, nil, container.NewStack(label, newThumbnailTap(toggle)))
	content := []fyne.CanvasObject{header}
	if open {
		_, _, accent := calloutColors("details")
		line := canvas.NewRectangle(accent)
		line.SetMinSize(fyne.NewSize(0, 1))
		divider := container.New(layout.NewCustomPaddedLayout(10, 10, 0, 0), line)
		content = append(content, divider, container.NewVBox(v.blocks(n)...))
	}
	background := &markdownCalloutBackground{view: v, kind: "details"}
	background.ExtendBaseWidget(background)
	body := container.New(layout.NewCustomPaddedLayout(12, 16, 16, 16), container.NewVBox(content...))
	return container.NewStack(background, body)
}
