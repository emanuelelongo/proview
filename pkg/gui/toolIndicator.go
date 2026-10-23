package gui

import (
	"emanuelelongo/proview/pkg/annotations"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A passive circular badge distinguishes the current tool from property buttons.
type toolIndicator struct {
	widget.BaseWidget
	icon *widget.Icon
}

func newToolIndicator() *toolIndicator {
	indicator := &toolIndicator{icon: widget.NewIcon(tablerIcon("pointer"))}
	indicator.ExtendBaseWidget(indicator)
	return indicator
}

func (i *toolIndicator) CreateRenderer() fyne.WidgetRenderer {
	circle := canvas.NewCircle(theme.SelectionColor())
	circle.StrokeColor = theme.PrimaryColor()
	circle.StrokeWidth = 1
	content := container.NewGridWrap(fyne.NewSize(36, 36), container.NewStack(circle, container.NewCenter(i.icon)))
	return &toolIndicatorRenderer{WidgetRenderer: widget.NewSimpleRenderer(content), circle: circle}
}

type toolIndicatorRenderer struct {
	fyne.WidgetRenderer
	circle *canvas.Circle
}

func (r *toolIndicatorRenderer) Refresh() {
	r.circle.FillColor = theme.SelectionColor()
	r.circle.StrokeColor = theme.PrimaryColor()
	r.WidgetRenderer.Refresh()
}

func (g *Gui) refreshToolIndicator() {
	if g.currentToolIndicator == nil {
		return
	}
	icon := map[annotationTool]string{toolSelect: "pointer", toolText: "text-outline", toolDraw: "sketching", toolHighlight: "highlight", toolShapes: "circle-square", toolRedact: "xxx"}[g.effectiveAnnotationTool()]
	switch {
	case g.cropAction != nil && g.cropAction.button.Importance == widget.HighImportance:
		icon = "crop"
	case g.formsActive:
		icon = "forms"
	case g.textEditing:
		icon = "file-type-pdf"
	}
	if icon == "pointer" {
		if g.selectedRedaction != 0 {
			icon = "xxx"
		} else if selected, ok := g.selectedOverlay(); ok {
			switch selected.ObjectKind {
			case "image":
				icon = "photo"
			case "pdf-stamp":
				icon = "file-type-pdf"
			default:
				icon = "signature"
			}
			if data, annotation := selected.Annotation(); annotation {
				switch data.Kind {
				case annotations.Text:
					icon = "text-outline"
				case annotations.Drawing:
					icon = "sketching"
				case annotations.Highlight:
					icon = "highlight"
				case annotations.Shape:
					icon = shapeIconName(data.Shape)
				}
			}
		} else if !g.temporarySelection {
			g.toolIndicatorHeader.Hide()
			return
		}
	}
	g.toolIndicatorHeader.Show()
	g.currentToolIndicator.icon.SetResource(tablerIcon(icon))
}
