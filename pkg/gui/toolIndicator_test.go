package gui

import (
	"testing"

	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
)

func TestSelectIndicatorFollowsSelectedObject(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.newToolPropertyPanel()
	g.setAnnotationTool(toolSelect)
	if g.toolIndicatorHeader.Visible() {
		t.Fatal("empty Select shows a heading")
	}
	for _, example := range []struct {
		kind  annotations.Kind
		shape annotations.ShapeKind
		icon  string
	}{
		{kind: annotations.Text, icon: "text-outline"},
		{kind: annotations.Drawing, icon: "sketching"},
		{kind: annotations.Shape, shape: annotations.Oval, icon: "oval"},
		{kind: annotations.Shape, shape: annotations.Star, icon: "star"},
	} {
		data := annotations.Default(example.kind)
		data.Text = "Sample"
		data.PageWidth, data.PageHeight = 100, 100
		data.Points = []annotations.Point{{X: 10, Y: 10}, {X: 90, Y: 50}}
		data.Shape = example.shape
		artwork, _, err := data.Artwork()
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := artwork.Prepare()
		if err != nil {
			t.Fatal(err)
		}
		object, err := g.document.AddAnnotation(0, document.NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, data, prepared)
		if err != nil {
			t.Fatal(err)
		}
		layer.reload()
		g.selectSignature(layer, object.ID)
		if !g.annotationControls.Visible() || !g.toolIndicatorHeader.Visible() || g.currentToolIndicator.icon.Resource.Name() != tablerIcon(example.icon).Name() {
			t.Fatalf("selected %s/%s has no matching heading above its properties", example.kind, example.shape)
		}
		g.clearSignatureSelection()
		if g.toolIndicatorHeader.Visible() || g.annotationPropertyScroll.Visible() || !g.toolPropertyPanel.Visible() {
			t.Fatal("clearing selection should empty the rail while preserving its space")
		}
	}
}
