package gui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"emanuelelongo/proview/pkg/document"
	"emanuelelongo/proview/pkg/signatures"
	"fmt"
	"fyne.io/fyne/v2/dialog"
	"golang.design/x/clipboard"
	"image/png"
	"math"
	"time"
)

type objectClipboard interface {
	ReadObject(context.Context) ([]byte, error)
	WriteObject(context.Context, []byte, []byte) error
}

var copiedObjectFormat = clipboard.Register("application/x-proview-object+json")

func (systemImageClipboard) ReadObject(ctx context.Context) ([]byte, error) {
	if e := initImageClipboard(); e != nil {
		return nil, e
	}
	return clipboard.Read(ctx, copiedObjectFormat)
}
func (systemImageClipboard) WriteObject(ctx context.Context, pngData, objectData []byte) error {
	if e := initImageClipboard(); e != nil {
		return e
	}
	_, e := clipboard.WriteAll(ctx, clipboard.Item{Format: clipboard.FmtImage, Bytes: pngData}, clipboard.Item{Format: copiedObjectFormat, Bytes: objectData})
	return e
}

func (g *Gui) copySelectedObject(o document.Overlay) {
	doc := g.document
	snapshot := doc.RenderingSnapshot()
	// The native private format must never contain an object hidden by redaction.
	if snapshot.ObjectIntersectsRedaction(o.ID) {
		g.copyPageArea(o.Page, o.Rect)
		return
	}
	g.queueAnnotation(func() func() {
		c, e := snapshot.CopyObject(o.ID)
		var payload, pngData []byte
		if e == nil {
			payload, e = c.Encode()
		}
		if e == nil {
			p, err := snapshot.ObjectImage(o.ID)
			e = err
			if e == nil {
				var b bytes.Buffer
				e = png.Encode(&b, p)
				pngData = b.Bytes()
			}
		}
		if e == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if backend, ok := desktopImageClipboard.(objectClipboard); ok {
				e = backend.WriteObject(ctx, pngData, payload)
			} else {
				e = fmt.Errorf("The clipboard backend does not support editable objects")
			}
		}
		return func() {
			if g.document != doc || g.closed {
				return
			}
			if e != nil {
				dialog.ShowError(fmt.Errorf("Could not copy object: %w", e), g.window)
			}
		}
	})
}

func (g *Gui) pastedObjectRect(c document.CopiedObject, pageSizeWidth, pageSizeHeight float64, count int) document.NormalizedRect {
	scale := math.Min(1, math.Min(pageSizeWidth/c.Width, pageSizeHeight/c.Height))
	w, h := c.Width*scale/pageSizeWidth, c.Height*scale/pageSizeHeight
	dx, dy := 12*float64(count)/pageSizeWidth, 12*float64(count)/pageSizeHeight
	x, y := c.Rect.X+dx, c.Rect.Y+dy
	if x+w > 1 {
		x = c.Rect.X - dx
	}
	if y+h > 1 {
		y = c.Rect.Y - dy
	}
	return document.NormalizedRect{X: math.Min(math.Max(x, 0), 1-w), Y: math.Min(math.Max(y, 0), 1-h), Width: w, Height: h}
}
func (g *Gui) insertCopiedObject(doc *document.Document, layer *signatureLayer, page int, c document.CopiedObject, prepared signatures.Prepared, payload []byte) {
	digest := sha256.Sum256(payload)
	count := 1
	if g.objectPasteDigest == digest && g.objectPasteDocument == doc && g.objectPastePage == page {
		count = g.objectPasteCount + 1
	}
	w, h := g.annotationPageSize(layer)
	rect := g.pastedObjectRect(c, w, h, count)
	o, e := doc.PasteObject(page, rect, c, prepared)
	if e != nil {
		dialog.ShowError(e, g.window)
		return
	}
	g.objectPasteDigest = digest
	g.objectPasteDocument = doc
	g.objectPastePage = page
	g.objectPasteCount = count
	g.setAnnotationTool(toolSelect)
	g.clearAreaSelection()
	layer.reload()
	g.selectSignature(layer, o.ID)
	g.documentChanged(page)
}
