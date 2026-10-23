package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/color"
	"math"
	"testing"

	"emanuelelongo/proview/internal/textselect"
	"emanuelelongo/proview/pkg/annotations"
	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
)

func TestHighlightFollowsTextAcrossLinesAsOneUndo(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolHighlight)
	p := layer.highlightInput
	geometry := textselect.Layout{Width: 1000, Height: 500, Words: []textselect.Word{
		{Text: "one", Line: 0, Quad: textselect.Rect(.1, .2, .2, .25)},
		{Text: "two", Line: 0, Quad: textselect.Rect(.22, .2, .32, .25)},
		{Text: "three", Line: 1, Quad: textselect.Rect(.1, .4, .2, .45)},
	}}
	p.geometry = &geometry
	p.key = g.document.TextSelectionKey(0)
	pos := func(x, y float32) *fyne.PointEvent {
		origin, size := layer.displayRect()
		return &fyne.PointEvent{Position: origin.Add(fyne.NewPos(x*size.Width, y*size.Height))}
	}
	down := mouseAt(0, 0)
	down.Position = pos(.15, .22).Position
	p.MouseDown(down)
	event := mouseAt(0, 0)
	event.Position = pos(.15, .42).Position
	p.MouseMoved(event)
	if !p.active || len(p.draft) != 2 || p.preview == nil || g.document.Dirty() {
		t.Fatal("selection did not follow text transiently")
	}
	if p.draft[0][0].X != 100 || p.draft[0][1].X != 320 {
		t.Fatalf("same-line words were not joined: %v", p.draft)
	}
	p.MouseUp(event)
	finishAnnotationJobs(t, g)
	if len(g.document.Overlays()) != 1 {
		t.Fatal("multiline highlight did not create one object")
	}
	data, _ := g.document.Overlays()[0].Annotation()
	if data.Kind != annotations.Highlight || data.Opacity != 1 || len(data.Quads) != 2 || len(data.Points) != 0 {
		t.Fatalf("highlight=%+v", data)
	}
	if !g.document.Undo() || g.document.Dirty() || g.document.CanUndo() {
		t.Fatal("highlight is not one Undo operation")
	}
	if !g.document.Redo() || len(g.document.Overlays()) != 1 {
		t.Fatal("highlight Redo failed")
	}
	// A background drag never becomes a freehand mark.
	p.MouseDown(mouseAt(800, 600))
	p.MouseUp(mouseAt(900, 650))
	if g.annotationPending != 0 {
		t.Fatal("background drag created a highlight")
	}
}

func TestHighlightCancellationAndPropertyControls(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolHighlight)
	p := layer.highlightInput
	geometry := textselect.Layout{Width: 1000, Height: 500, Words: []textselect.Word{{Text: "one", Quad: textselect.Rect(.1, .2, .3, .3)}}}
	p.geometry = &geometry
	p.key = g.document.TextSelectionKey(0)
	origin, size := layer.displayRect()
	event := mouseAt(origin.X+.15*size.Width, origin.Y+.25*size.Height)
	p.MouseDown(event)
	if !p.active || !g.cancelCurrentOperation() || p.active || g.document.Dirty() {
		t.Fatal("Escape did not cancel highlight")
	}
	for _, object := range fyneTest.LaidOutObjects(g.annotationControls) {
		if _, ok := object.(*compactSelect); ok {
			t.Fatal("Highlight still has thickness/opacity selectors")
		}
	}
	if layer.input.Visible() || !p.Visible() {
		t.Fatal("Highlight is still using the freehand input")
	}
	g.setAnnotationTool(toolDraw)
	if p.Visible() || !layer.input.Visible() {
		t.Fatal("Draw did not restore freehand input")
	}
	selects := 0
	for _, object := range fyneTest.LaidOutObjects(g.annotationControls) {
		if _, ok := object.(*compactSelect); ok {
			selects++
		}
	}
	if selects < 2 {
		t.Fatal("Draw lacks width or opacity control")
	}
}

func TestDrawRoundAndLegacySolidPreviewMatchOpacity(t *testing.T) {
	for _, style := range []annotations.StrokeStyle{annotations.StrokeRound, annotations.StrokeSolid} {
		t.Run(string(style), func(t *testing.T) {
			g, layer := annotationTestGui(t)
			g.setAnnotationTool(toolDraw)
			data := g.annotationDefaults[annotations.Drawing]
			data.Width = 20
			data.Opacity = .3
			data.StrokeStyle = style
			g.annotationDefaults[annotations.Drawing] = data
			p := layer.input
			p.MouseDown(mouseAt(100, 300))
			p.MouseMoved(mouseAt(250, 300))
			p.MouseMoved(mouseAt(100, 300))
			if !p.active || p.frame == nil {
				t.Fatal("missing live stroke")
			}
			maxAlpha := uint8(0)
			for i := 3; i < len(p.frame.Pix); i += 4 {
				if p.frame.Pix[i] > maxAlpha {
					maxAlpha = p.frame.Pix[i]
				}
			}
			if math.Abs(float64(maxAlpha)-76) > 1 {
				t.Fatalf("self-intersection opacity=%d", maxAlpha)
			}
			p.MouseUp(mouseAt(250, 300))
			finishAnnotationJobs(t, g)
			actual, _ := g.document.Overlays()[0].Annotation()
			if actual.StrokeStyle != style || actual.Opacity != .3 || len(actual.Points) < 3 {
				t.Fatal("stroke mode/opacity/freehand points were not preserved")
			}
		})
	}
}

func TestHighlightNativePDFQueuesReleasedGestureAndKeepsTextSearchable(t *testing.T) {
	g, driver := newDropTestGui(t)
	source, err := document.Load("highlight.pdf", bytes.NewReader(redactTestPDF()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	g.showDocument(doc)
	g.setAnnotationTool(toolHighlight)
	layer := g.signatureLayers[0]
	p := layer.highlightInput
	down := mouseAt(0, 0)
	down.Position = wordPosition(t, layer, "PUBLIC")
	up := mouseAt(0, 0)
	up.Position = wordPosition(t, layer, "KEEP")
	p.MouseDown(down)
	p.MouseUp(up)
	if !p.pending || doc.Dirty() {
		t.Fatal("pending native geometry mutated document")
	}
	for len(doc.Overlays()) == 0 {
		driver.runNext(t)
	}
	data, _ := doc.Overlays()[0].Annotation()
	if len(data.Quads) != 1 || data.Opacity != 1 || g.annotationPending != 0 {
		t.Fatal("released gesture did not commit once")
	}
	exported, err := doc.Export(document.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := document.Load("highlighted.pdf", bytes.NewReader(exported))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	finalDoc, err := document.New(reopened)
	if err != nil {
		t.Fatal(err)
	}
	geometry, err := finalDoc.TextGeometry(0)
	if err != nil || len(geometry.Glyphs) == 0 {
		t.Fatal("highlight destroyed searchable PDF text", err)
	}
	g.closeWindow()
}

func TestHighlightDraftSamplesOnlySelectedAreaWithoutShiftingBackdrop(t *testing.T) {
	g, layer := annotationTestGui(t)
	g.setAnnotationTool(toolHighlight)
	p := layer.highlightInput
	layout := textselect.Layout{Width: 1000, Height: 500, Words: []textselect.Word{{Text: "word", Quad: textselect.Rect(.4, .3, .6, .4)}}}
	p.geometry, p.key = &layout, g.document.TextSelectionKey(0)
	origin, size := layer.displayRect()
	event := mouseAt(origin.X+.5*size.Width, origin.Y+.35*size.Height)
	backdrop := image.NewNRGBA(image.Rect(0, 0, 1000, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 1000; x++ {
			backdrop.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}
	p.MouseDown(event)
	p.backdrop = backdrop
	p.end = -1
	p.update(event.Position)
	if p.preview == nil {
		t.Fatal("draft missing")
	}
	const width, height = 1000, 500
	rect := p.previewRect
	tile := image.Rect(0, 0, int(math.Round(rect.Width*width)), int(math.Round(rect.Height*height)))
	offset := image.Pt(int(math.Round(rect.X*width)), int(math.Round(rect.Y*height)))
	actual := p.preview.Generator(tile.Dx(), tile.Dy())
	if tile.Dx() > 205 || tile.Dy() > 55 {
		t.Fatal("draft retained a page-sized texture")
	}
	scaled := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.BiLinear.Scale(scaled, scaled.Bounds(), backdrop, backdrop.Bounds(), xdraw.Src, nil)
	mask := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	mask.SetNRGBA(0, 0, p.data.Color)
	for _, point := range []image.Point{image.Pt(420, 160), image.Pt(498, 175), image.Pt(580, 190)} {
		expected := document.MultiplyHighlight(scaled, mask, image.Rectangle{Min: point, Max: point.Add(image.Pt(1, 1))})
		got := color.NRGBAModel.Convert(actual.At(point.X-offset.X, point.Y-offset.Y)).(color.NRGBA)
		want := color.NRGBAModel.Convert(expected.At(0, 0)).(color.NRGBA)
		if got != want {
			t.Fatalf("shifted backdrop at %v: got %v want %v", point, got, want)
		}
	}
	for _, point := range []image.Point{image.Pt(0, 0), image.Pt(offset.X, 170), image.Pt(500, offset.Y)} {
		if _, _, _, alpha := actual.At(point.X-offset.X, point.Y-offset.Y).RGBA(); alpha != 0 {
			t.Fatalf("highlight leaked at %v", point)
		}
	}
	p.cancel()
}
