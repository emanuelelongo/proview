package document

import (
	"bytes"
	"emanuelelongo/proview/pkg/annotations"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

func TestNewNormalizedRect(t *testing.T) {
	tests := []struct {
		name        string
		x, y, w, h  float64
		wantInvalid bool
	}{
		{name: "full page", w: 1, h: 1},
		{name: "inside page", x: .1, y: .2, w: .3, h: .4},
		{name: "negative origin", x: -.1, w: .5, h: .5, wantInvalid: true},
		{name: "zero width", w: 0, h: .5, wantInvalid: true},
		{name: "outside right", x: .8, w: .3, h: .5, wantInvalid: true},
		{name: "outside bottom", y: .8, w: .5, h: .3, wantInvalid: true},
		{name: "NaN", x: math.NaN(), w: .5, h: .5, wantInvalid: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewNormalizedRect(test.x, test.y, test.w, test.h)
			if test.wantInvalid && !errors.Is(err, ErrInvalidRect) {
				t.Fatalf("error = %v, want ErrInvalidRect", err)
			}
			if !test.wantInvalid && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDocumentCropIsNonDestructiveAndComposable(t *testing.T) {
	source, original := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Dirty() {
		t.Fatal("new document is dirty")
	}

	if err := doc.ApplyCrop(image.Rect(10, 20, 90, 80), image.Rect(0, 0, 100, 100)); err != nil {
		t.Fatal(err)
	}
	assertRect(t, doc, NormalizedRect{X: .1, Y: .2, Width: .8, Height: .6})

	if err := doc.ApplyCrop(image.Rect(20, 15, 60, 45), image.Rect(0, 0, 80, 60)); err != nil {
		t.Fatal(err)
	}
	assertRect(t, doc, NormalizedRect{X: .3, Y: .35, Width: .4, Height: .3})
	if !doc.Dirty() {
		t.Fatal("cropped document is not dirty")
	}
	if !bytes.Equal(doc.Original(), original) {
		t.Fatal("crop changed the original bytes")
	}

	doc.MarkSaved()
	if doc.Dirty() {
		t.Fatal("MarkSaved did not clear dirty state")
	}
	if !doc.ClearCrop() || !doc.Dirty() {
		t.Fatal("ClearCrop did not change state")
	}
	if _, ok := doc.Crop(); ok {
		t.Fatal("crop still present after ClearCrop")
	}
}

func TestDocumentCropWithExtremeAspectRatiosAndSizes(t *testing.T) {
	tests := []struct {
		name      string
		bounds    image.Rectangle
		selection image.Rectangle
		want      NormalizedRect
	}{
		{
			name:      "very wide",
			bounds:    image.Rect(0, 0, 100000, 10),
			selection: image.Rect(10000, 1, 90000, 9),
			want:      NormalizedRect{X: .1, Y: .1, Width: .8, Height: .8},
		},
		{
			name:      "very tall",
			bounds:    image.Rect(0, 0, 10, 100000),
			selection: image.Rect(1, 10000, 9, 90000),
			want:      NormalizedRect{X: .1, Y: .1, Width: .8, Height: .8},
		},
		{
			name:      "tiny",
			bounds:    image.Rect(0, 0, 2, 2),
			selection: image.Rect(0, 0, 1, 1),
			want:      NormalizedRect{Width: .5, Height: .5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, _ := testImageSource(t)
			doc, err := New(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := doc.ApplyCrop(test.selection, test.bounds); err != nil {
				t.Fatal(err)
			}
			assertRect(t, doc, test.want)
		})
	}
}

func TestPDFCropIsRejected(t *testing.T) {
	source, err := Load("document.pdf", bytes.NewReader(minimalPDF(t, 1)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.ApplyCrop(image.Rect(0, 0, 10, 10), image.Rect(0, 0, 20, 20)); !errors.Is(err, ErrCropUnsupported) {
		t.Fatalf("error = %v, want ErrCropUnsupported", err)
	}
	if doc.Dirty() {
		t.Fatal("rejected crop changed the document")
	}
}

func TestDocumentSignatures(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	signaturePNG := testSignaturePNG(t)
	rect, err := NewNormalizedRect(.2, .3, .4, .2)
	if err != nil {
		t.Fatal(err)
	}

	first, err := doc.AddSignature(0, rect, signaturePNG)
	if err != nil {
		t.Fatal(err)
	}
	second, err := doc.AddSignature(0, NormalizedRect{X: .5, Y: .5, Width: .2, Height: .2}, signaturePNG)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.ID == 0 {
		t.Fatalf("signature IDs are not unique: %d, %d", first.ID, second.ID)
	}
	if first.Preview() == nil || !bytes.Equal(first.PNG(), signaturePNG) {
		t.Fatal("signature assets were not preserved")
	}

	signaturePNG[0] = 0
	if bytes.Equal(first.PNG(), signaturePNG) {
		t.Fatal("signature retained the caller's PNG slice")
	}

	updatedRect := NormalizedRect{X: .1, Y: .1, Width: .3, Height: .3}
	if err := doc.UpdateSignature(first.ID, 0, updatedRect); err != nil {
		t.Fatal(err)
	}
	signatures := doc.Signatures()
	if len(signatures) != 2 || signatures[0].Rect != updatedRect {
		t.Fatalf("signatures after update = %#v", signatures)
	}

	if err := doc.RemoveSignature(first.ID); err != nil {
		t.Fatal(err)
	}
	signatures, err = doc.SignaturesForPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(signatures) != 1 || signatures[0].ID != second.ID {
		t.Fatalf("signatures after remove = %#v", signatures)
	}
	if err := doc.RemoveSignature(first.ID); !errors.Is(err, ErrSignatureNotFound) {
		t.Fatalf("second remove error = %v, want ErrSignatureNotFound", err)
	}
}

func TestSignatureStrokeIsUndoable(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := doc.AddSignature(0, NormalizedRect{X: .2, Y: .2, Width: .4, Height: .2}, testSignaturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	doc.MarkSaved()
	originalPixels := visiblePixels(signature.Preview())
	if err := doc.SetSignatureStroke(signature.ID, 4); err != nil {
		t.Fatal(err)
	}
	updated := doc.Signatures()[0]
	if updated.StrokeWidth != 4 || visiblePixels(updated.Preview()) <= originalPixels || !doc.Dirty() {
		t.Fatalf("stroke update was not applied: %#v", updated)
	}
	if !doc.Undo() || doc.Signatures()[0].StrokeWidth != 0 || doc.Dirty() {
		t.Fatal("undo did not restore the original stroke")
	}
	if !doc.Redo() || doc.Signatures()[0].StrokeWidth != 4 {
		t.Fatal("redo did not restore the thicker stroke")
	}
	if err := doc.SetSignatureStroke(signature.ID, 11); !errors.Is(err, ErrInvalidStroke) {
		t.Fatalf("invalid stroke error = %v, want ErrInvalidStroke", err)
	}
}

func visiblePixels(input image.Image) int {
	count := 0
	for y := input.Bounds().Min.Y; y < input.Bounds().Max.Y; y++ {
		for x := input.Bounds().Min.X; x < input.Bounds().Max.X; x++ {
			_, _, _, alpha := input.At(x, y).RGBA()
			if alpha != 0 {
				count++
			}
		}
	}
	return count
}

func TestDocumentSignaturesOnDifferentPDFPages(t *testing.T) {
	source, err := Load("document.pdf", bytes.NewReader(minimalPDF(t, 2)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	rect := NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}
	for page := 0; page < 2; page++ {
		if _, err := doc.AddSignature(page, rect, testSignaturePNG(t)); err != nil {
			t.Fatal(err)
		}
	}
	for page := 0; page < 2; page++ {
		signatures, err := doc.SignaturesForPage(page)
		if err != nil {
			t.Fatal(err)
		}
		if len(signatures) != 1 || signatures[0].Page != page {
			t.Fatalf("page %d signatures = %#v", page, signatures)
		}
	}
}

func TestRenderPageComposesCropAndSignature(t *testing.T) {
	source, original := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.ApplyCrop(image.Rect(10, 20, 90, 80), image.Rect(0, 0, 100, 100)); err != nil {
		t.Fatal(err)
	}
	signature := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	signature.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	signature.SetNRGBA(1, 0, color.NRGBA{R: 255, A: 255})
	var signatureData bytes.Buffer
	if err := png.Encode(&signatureData, signature); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddSignature(0, NormalizedRect{X: .25, Y: .25, Width: .5, Height: .25}, signatureData.Bytes()); err != nil {
		t.Fatal(err)
	}

	rendered, err := doc.RenderPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := rendered.Bounds(); got.Dx() != 80 || got.Dy() != 60 {
		t.Fatalf("rendered bounds = %v, want 80x60", got)
	}
	foundRed := false
	for y := rendered.Bounds().Min.Y; y < rendered.Bounds().Max.Y && !foundRed; y++ {
		for x := rendered.Bounds().Min.X; x < rendered.Bounds().Max.X; x++ {
			r, g, b, a := rendered.At(x, y).RGBA()
			if r > 0 && g == 0 && b == 0 && a > 0 {
				foundRed = true
				break
			}
		}
	}
	if !foundRed {
		t.Fatal("rendered page does not contain the signature")
	}
	if !bytes.Equal(doc.Original(), original) {
		t.Fatal("rendering changed the original bytes")
	}
}

func TestCropKeepsExistingSignatureAspectRatio(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := doc.AddSignature(0, NormalizedRect{X: .2, Y: .2, Width: .4, Height: .2}, testSignaturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.ApplyCrop(image.Rect(0, 0, 100, 50), image.Rect(0, 0, 100, 100)); err != nil {
		t.Fatal(err)
	}
	rect := doc.Signatures()[0].Rect
	displayAspect := rect.Width * 100 / (rect.Height * 50)
	wantAspect := float64(signature.preview.Bounds().Dx()) / float64(signature.preview.Bounds().Dy())
	if math.Abs(displayAspect-wantAspect) > 1e-12 {
		t.Fatalf("signature aspect after crop = %f, want %f", displayAspect, wantAspect)
	}
	if !doc.Undo() || doc.Signatures()[0].Rect != signature.Rect {
		t.Fatal("undo crop did not restore the original signature rectangle")
	}
}

func TestDocumentSignatureValidation(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	validRect := NormalizedRect{Width: .5, Height: .5}

	tests := []struct {
		name string
		page int
		rect NormalizedRect
		png  []byte
		want error
	}{
		{name: "invalid page", page: 1, rect: validRect, png: testSignaturePNG(t), want: ErrInvalidPage},
		{name: "invalid rect", page: 0, rect: NormalizedRect{}, png: testSignaturePNG(t), want: ErrInvalidRect},
		{name: "invalid PNG", page: 0, rect: validRect, png: []byte("invalid")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := doc.AddSignature(test.page, test.rect, test.png)
			if err == nil {
				t.Fatal("AddSignature() succeeded, want error")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	if doc.Dirty() || len(doc.Signatures()) != 0 {
		t.Fatal("rejected signatures changed the document")
	}
}

func TestDocumentUndoRedoHistory(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}

	if err := doc.ApplyCrop(image.Rect(10, 10, 90, 90), image.Rect(0, 0, 100, 100)); err != nil {
		t.Fatal(err)
	}
	originalRect := NormalizedRect{X: .2, Y: .2, Width: .3, Height: .2}
	signature, err := doc.AddSignature(0, originalRect, testSignaturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	movedRect := NormalizedRect{X: .5, Y: .4, Width: .3, Height: .2}
	if err := doc.UpdateSignature(signature.ID, 0, movedRect); err != nil {
		t.Fatal(err)
	}
	if err := doc.RemoveSignature(signature.ID); err != nil {
		t.Fatal(err)
	}

	if !doc.CanUndo() || doc.CanRedo() {
		t.Fatal("unexpected history state after edits")
	}
	if !doc.Undo() || len(doc.Signatures()) != 1 {
		t.Fatal("undo did not restore the removed signature")
	}
	if !doc.Undo() || doc.Signatures()[0].Rect != originalRect {
		t.Fatal("undo did not restore the signature position")
	}
	if !doc.Undo() || len(doc.Signatures()) != 0 {
		t.Fatal("undo did not remove the added signature")
	}
	if !doc.Undo() {
		t.Fatal("undo did not restore the uncropped image")
	}
	if _, cropped := doc.Crop(); cropped {
		t.Fatal("crop still present after undo")
	}
	if doc.Dirty() || doc.CanUndo() || !doc.CanRedo() {
		t.Fatal("initial state was not fully restored")
	}

	for range 4 {
		if !doc.Redo() {
			t.Fatal("redo history ended too early")
		}
	}
	if len(doc.Signatures()) != 0 {
		t.Fatal("redo did not restore the final removal")
	}
	if _, cropped := doc.Crop(); !cropped {
		t.Fatal("redo did not restore the crop")
	}
	doc.MarkSaved()
	if doc.Dirty() {
		t.Fatal("saved final state is dirty")
	}
	if !doc.Undo() || !doc.Dirty() {
		t.Fatal("undo away from the saved state did not mark the document dirty")
	}
	if !doc.Redo() || doc.Dirty() {
		t.Fatal("redo to the saved state did not clear the dirty flag")
	}
}

func TestSignatureDragIsOneHistoryEntryAndBranchClearsRedo(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	originalRect := NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}
	signature, err := doc.AddSignature(0, originalRect, testSignaturePNG(t))
	if err != nil {
		t.Fatal(err)
	}

	if err := doc.BeginSignatureEdit(signature.ID); err != nil {
		t.Fatal(err)
	}
	if doc.CanUndo() || doc.CanRedo() {
		t.Fatal("history actions are available during an active edit")
	}
	for _, rect := range []NormalizedRect{
		{X: .2, Y: .2, Width: .2, Height: .2},
		{X: .3, Y: .25, Width: .2, Height: .2},
		{X: .4, Y: .3, Width: .2, Height: .2},
	} {
		if err := doc.UpdateSignature(signature.ID, 0, rect); err != nil {
			t.Fatal(err)
		}
	}
	if err := doc.CommitSignatureEdit(signature.ID); err != nil {
		t.Fatal(err)
	}
	finalRect := doc.Signatures()[0].Rect
	if !doc.Undo() || doc.Signatures()[0].Rect != originalRect {
		t.Fatal("one undo did not revert the complete drag")
	}
	if !doc.Redo() || doc.Signatures()[0].Rect != finalRect {
		t.Fatal("redo did not restore the complete drag")
	}

	if !doc.Undo() {
		t.Fatal("cannot prepare history branch")
	}
	branchedRect := NormalizedRect{X: .6, Y: .6, Width: .2, Height: .2}
	if err := doc.UpdateSignature(signature.ID, 0, branchedRect); err != nil {
		t.Fatal(err)
	}
	if doc.CanRedo() {
		t.Fatal("new edit after undo did not clear redo history")
	}
}

func TestDocumentHistoryIsBounded(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := doc.AddSignature(0, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, testSignaturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxHistoryEntries+25; index++ {
		x := .1
		if index%2 == 0 {
			x = .2
		}
		if err := doc.UpdateSignature(signature.ID, 0, NormalizedRect{X: x, Y: .1, Width: .2, Height: .2}); err != nil {
			t.Fatal(err)
		}
	}
	undos := 0
	for doc.Undo() {
		undos++
	}
	if undos != maxHistoryEntries {
		t.Fatalf("undo count = %d, want bounded history of %d", undos, maxHistoryEntries)
	}
}

func assertRect(t *testing.T, doc *Document, want NormalizedRect) {
	t.Helper()
	got, ok := doc.Crop()
	if !ok {
		t.Fatal("document has no crop")
	}
	if math.Abs(got.X-want.X) > 1e-12 || math.Abs(got.Y-want.Y) > 1e-12 ||
		math.Abs(got.Width-want.Width) > 1e-12 || math.Abs(got.Height-want.Height) > 1e-12 {
		t.Fatalf("crop = %#v, want %#v", got, want)
	}
}

func testImageSource(t *testing.T) (*Source, []byte) {
	t.Helper()
	input := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, input); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), encoded.Bytes()...)
	source, err := Load("image.png", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	return source, original
}

func testSignaturePNG(t testing.TB) []byte {
	t.Helper()
	signature := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	signature.SetNRGBA(1, 1, color.NRGBA{A: 128})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, signature); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestSnapshotSharesImmutableBuffersAndPreservesIsolation(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	pngData := testSignaturePNG(t)
	sig, err := doc.AddSignature(0, NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, pngData)
	if err != nil {
		t.Fatal(err)
	}
	annData := annotations.Default(annotations.Drawing)
	annData.PageWidth, annData.PageHeight = 100, 100
	annData.Points = []annotations.Point{{X: 10, Y: 10}, {X: 50, Y: 50}}
	asset, _, err := annData.Artwork()
	if err != nil {
		t.Fatal(err)
	}
	prep, err := asset.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddAnnotation(0, NormalizedRect{X: .2, Y: .2, Width: .3, Height: .3}, annData, prep); err != nil {
		t.Fatal(err)
	}
	doc.MarkSaved()

	// Dirty() should not allocate when comparing unchanged state.
	allocs := testing.AllocsPerRun(50, func() {
		if doc.Dirty() {
			t.Fatal("expected clean document")
		}
	})
	if allocs > 0 {
		t.Fatalf("Dirty() allocated %v objects per run, want 0", allocs)
	}

	// Internal snapshots share immutable PNG, annotation, and asset payloads and preserve revision.
	snap := doc.RenderingSnapshot()
	if snap.Revision() != doc.Revision() {
		t.Fatalf("RenderingSnapshot revision = %d, want %d", snap.Revision(), doc.Revision())
	}
	if &snap.signatures[0].png[0] != &doc.signatures[0].png[0] {
		t.Fatal("RenderingSnapshot cloned immutable PNG slice")
	}
	if snap.signatures[1].annotation != doc.signatures[1].annotation || snap.signatures[1].asset != doc.signatures[1].asset {
		t.Fatal("RenderingSnapshot cloned immutable annotation or asset pointer")
	}

	// Public PNG(), Annotation(), and Asset() accessors still return defensive copies.
	exported := doc.Signatures()[0].PNG()
	exported[0] ^= 0xff
	if bytes.Equal(exported, doc.Signatures()[0].PNG()) {
		t.Fatal("mutating exported PNG slice affected document state")
	}
	exportedAnn, ok := doc.Signatures()[1].Annotation()
	if !ok {
		t.Fatal("expected annotation")
	}
	exportedAnn.Points[0].X = 999
	currentAnn, _ := doc.Signatures()[1].Annotation()
	if currentAnn.Points[0].X == 999 {
		t.Fatal("mutating exported annotation points affected document state")
	}
	exportedAsset := doc.Signatures()[1].Asset()
	exportedAsset.Data[0] ^= 0xff
	if bytes.Equal(exportedAsset.Data, doc.Signatures()[1].Asset().Data) {
		t.Fatal("mutating exported asset data affected document state")
	}
	if err := doc.UpdateSignature(sig.ID, 0, NormalizedRect{X: .2, Y: .2, Width: .2, Height: .2}); err != nil {
		t.Fatal(err)
	}
	if !doc.Dirty() {
		t.Fatal("expected dirty document after signature move")
	}
}

