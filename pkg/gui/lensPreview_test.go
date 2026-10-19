package gui

import (
	"testing"

	"emanuelelongo/proview/pkg/annotations"
)

func TestLensPreviewReplacesObsoleteFrameWithoutAnotherDebounce(t *testing.T) {
	g, driver := newDropTestGui(t)
	g.addDocument(testDocument(t, 1000, 500), nil, nil)
	layer := g.imageSignatureLayer
	data := annotations.Default(annotations.Shape)
	data.Shape, data.PageWidth, data.PageHeight = annotations.Magnifier, 1000, 500
	data.Points = []annotations.Point{{X: 100, Y: 100}, {X: 200, Y: 200}}
	g.addPageAnnotation(layer, data, 0, 0, nil)
	for g.annotationPending > 0 {
		driver.runNext(t)
	}
	if layer.lensTimer != nil {
		layer.lensTimer.Stop()
		layer.lensTimer = nil
	}
	for layer.lensRunning {
		driver.runNext(t)
	}
	driver.drain()
	g.renderLensPreviews(layer)
	if !layer.lensRunning {
		t.Fatal("frame did not start")
	}
	for range 10 {
		g.scheduleLensPreviews(layer)
	}
	if layer.lensTimer != nil {
		t.Fatal("motion queued extra timers while a frame was running")
	}
	// The stale completion must start the latest frame immediately, leaving no
	// delay during which continuous motion can keep the content frozen.
	driver.runNext(t)
	if !layer.lensRunning || layer.lensTimer != nil {
		t.Fatal("obsolete frame added another debounce before replacement")
	}
	for layer.lensRunning {
		driver.runNext(t)
	}
	g.closeWindow()
}
