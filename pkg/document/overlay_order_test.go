package document

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestReorderOverlayIsPageLocalAndUndoable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		object int
		order  OverlayOrder
		want   []int
	}{
		{"front", 2, BringToFront, []int{0, 1, 4, 3, 5, 2}},
		{"forward", 2, BringForward, []int{0, 1, 4, 3, 2, 5}},
		{"backward", 2, SendBackward, []int{2, 1, 0, 3, 4, 5}},
		{"back", 4, SendToBack, []int{4, 1, 0, 3, 2, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := Load("pages.pdf", bytes.NewReader(minimalPDF(t, 2)))
			if err != nil {
				t.Fatal(err)
			}
			doc, _ := New(source)
			defer doc.Close()
			var ids []OverlayID
			for _, page := range []int{0, 1, 0, 1, 0, 0} {
				o, err := doc.AddSignature(page, NormalizedRect{X: .1, Y: .1, Width: .5, Height: .5}, opaqueSignaturePNG(t))
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, o.ID)
			}
			doc.MarkSaved()
			before := doc.RenderingSnapshot()
			if err := doc.ReorderOverlay(ids[tc.object], tc.order); err != nil {
				t.Fatal(err)
			}
			var got, want []OverlayID
			for _, o := range doc.Overlays() {
				got = append(got, o.ID)
			}
			for _, i := range tc.want {
				want = append(want, ids[i])
			}
			if !slices.Equal(got, want) || !doc.Dirty() {
				t.Fatalf("order = %v, want %v", got, want)
			}
			if before.Overlays()[tc.object].ID != ids[tc.object] {
				t.Fatal("rendering snapshot changed")
			}
			if !doc.Undo() || doc.Dirty() {
				t.Fatal("Undo did not restore saved state")
			}
			for i, o := range doc.Overlays() {
				if o.ID != ids[i] {
					t.Fatal("Undo lost original order")
				}
			}
			if !doc.Redo() {
				t.Fatal("Redo failed")
			}
			for i, o := range doc.Overlays() {
				if o.ID != want[i] {
					t.Fatal("Redo lost reordered objects")
				}
			}
			revision := doc.Revision()
			if err := doc.ReorderOverlay(999, BringToFront); !errors.Is(err, ErrSignatureNotFound) {
				t.Fatal("missing object accepted")
			}
			if err := doc.ReorderOverlay(ids[0], OverlayOrder(999)); err == nil {
				t.Fatal("invalid order accepted")
			}
			if err := doc.BeginOverlayEdit(ids[0]); err != nil {
				t.Fatal(err)
			}
			if err := doc.ReorderOverlay(ids[0], BringToFront); !errors.Is(err, ErrEditInProgress) {
				t.Fatal("active gesture was reordered")
			}
			if err := doc.CancelOverlayEdit(ids[0]); err != nil {
				t.Fatal(err)
			}
			if doc.Revision() != revision {
				t.Fatal("rejected operations changed document revision")
			}
		})
	}
}

func TestReorderOverlayPreviewAndExportAgree(t *testing.T) {
	for _, format := range []Format{FormatPNG, FormatPDF} {
		t.Run(string(format), func(t *testing.T) {
			var source *Source
			if format == FormatPDF {
				var err error
				source, err = Load("page.pdf", bytes.NewReader(textPDF(t, "Preserved")))
				if err != nil {
					t.Fatal(err)
				}
			} else {
				source, _ = testImageSource(t)
			}
			doc, _ := New(source)
			defer doc.Close()
			var blue bytes.Buffer
			img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
			for y := range 2 {
				for x := range 2 {
					img.SetNRGBA(x, y, color.NRGBA{B: 255, A: 255})
				}
			}
			if err := png.Encode(&blue, img); err != nil {
				t.Fatal(err)
			}
			rect := NormalizedRect{X: .1, Y: .1, Width: .8, Height: .8}
			red, err := doc.AddSignature(0, rect, opaqueSignaturePNG(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := doc.AddImage(0, rect, blue.Bytes()); err != nil {
				t.Fatal(err)
			}
			assertColor := func(img image.Image, wantRed bool) {
				t.Helper()
				p := img.Bounds().Size().Div(2)
				r, _, b, _ := img.At(p.X, p.Y).RGBA()
				if wantRed && (r < 60000 || b > 5000) || !wantRed && (b < 60000 || r > 5000) {
					t.Fatalf("wrong front object: r=%d b=%d", r, b)
				}
			}
			check := func(wantRed bool) {
				t.Helper()
				preview, err := doc.RenderPage(0)
				if err != nil {
					t.Fatal(err)
				}
				assertColor(preview, wantRed)
				output, err := doc.Export(ExportOptions{Format: format})
				if err != nil {
					t.Fatal(err)
				}
				exported, err := Load("output."+string(format), bytes.NewReader(output))
				if err != nil {
					t.Fatal(err)
				}
				defer exported.Close()
				exportedPreview, err := exported.Preview(0)
				if err != nil {
					t.Fatal(err)
				}
				assertColor(exportedPreview, wantRed)
				if format == FormatPDF {
					var content strings.Builder
					if err := api.ExtractContent(context.Background(), bytes.NewReader(output), nil, func(r io.Reader, _ int) error { _, err := io.Copy(&content, r); return err }, nil); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(content.String(), "Preserved") || !strings.Contains(content.String(), "0 0 m 72 72 l S") {
						t.Fatal("original PDF text/vector content was lost")
					}
				}
			}
			check(false)
			if err := doc.ReorderOverlay(red.ID, BringToFront); err != nil {
				t.Fatal(err)
			}
			check(true)
			doc.MarkSaved()
			revision := doc.Revision()
			if err := doc.ReorderOverlay(red.ID, BringForward); err != nil {
				t.Fatal(err)
			}
			if doc.Dirty() || doc.Revision() != revision {
				t.Fatal("no-op created an edit")
			}
		})
	}
}
