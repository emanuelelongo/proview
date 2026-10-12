package document

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestExportPNGComposesAtOriginalResolution(t *testing.T) {
	source, _ := testImageSource(t)
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.ApplyCrop(image.Rect(10, 20, 90, 80), image.Rect(0, 0, 100, 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddSignature(0, NormalizedRect{X: .25, Y: .25, Width: .5, Height: .25}, opaqueSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}

	output, err := doc.Export(ExportOptions{Format: FormatPNG})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if got := exported.Bounds().Size(); got != image.Pt(80, 60) {
		t.Fatalf("exported PNG size = %v, want 80x60", got)
	}
	if !containsRed(exported) {
		t.Fatal("exported PNG does not contain the signature")
	}
	_, _, _, alpha := exported.At(exported.Bounds().Min.X, exported.Bounds().Min.Y).RGBA()
	if alpha != 0 {
		t.Fatalf("exported PNG corner alpha = %d, want transparent", alpha)
	}
}

func TestExportJPEGFlattensTransparency(t *testing.T) {
	transparent := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	var input bytes.Buffer
	if err := png.Encode(&input, transparent); err != nil {
		t.Fatal(err)
	}
	source, err := Load("transparent.png", bytes.NewReader(input.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}

	output, err := doc.Export(ExportOptions{Format: FormatJPEG, JPEGQuality: 100})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := jpeg.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := exported.At(0, 0).RGBA()
	if r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Fatalf("transparent JPEG background = (%d,%d,%d), want white", r, g, b)
	}
}

func TestExportPDFPreservesTextAndPlacesSignature(t *testing.T) {
	original := textPDF(t, "Hello vector")
	source, err := Load("text.pdf", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	rect := NormalizedRect{X: .1, Y: .2, Width: .2, Height: .1}
	if _, err := doc.AddSignature(0, rect, opaqueSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}

	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(output, original) {
		t.Fatal("signed PDF is byte-identical to the original")
	}
	pageCount, err := api.PageCount(context.Background(), bytes.NewReader(output), nil)
	if err != nil || pageCount != 1 {
		t.Fatalf("exported page count = %d, error = %v", pageCount, err)
	}
	var extracted strings.Builder
	err = api.ExtractContent(context.Background(), bytes.NewReader(output), nil, func(reader io.Reader, _ int) error {
		_, err := io.Copy(&extracted, reader)
		return err
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extracted.String(), "Hello vector") {
		t.Fatalf("exported PDF lost selectable text; content = %q", extracted.String())
	}
	if !strings.Contains(extracted.String(), "0 0 m 72 72 l S") {
		t.Fatalf("exported PDF lost vector drawing commands; content = %q", extracted.String())
	}

	exportedSource, err := Load("signed.pdf", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := exportedSource.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	redBounds, ok := colorBounds(preview, func(r, g, b, a uint32) bool {
		return a > 0 && r > 0xc000 && g < 0x3000 && b < 0x3000
	})
	if !ok {
		t.Fatal("exported PDF preview does not contain the signature")
	}
	w, h := preview.Bounds().Dx(), preview.Bounds().Dy()
	want := image.Rect(int(.1*float64(w)), int(.2*float64(h)), int(.3*float64(w)), int(.3*float64(h)))
	if absInt(redBounds.Min.X-want.Min.X) > 3 || absInt(redBounds.Min.Y-want.Min.Y) > 3 ||
		absInt(redBounds.Max.X-want.Max.X) > 3 || absInt(redBounds.Max.Y-want.Max.Y) > 3 {
		t.Fatalf("signature bounds = %v, want approximately %v", redBounds, want)
	}
}

func TestExportUnmodifiedPDFReturnsOriginalBytes(t *testing.T) {
	original := minimalPDF(t, 1)
	source, err := Load("original.pdf", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, original) {
		t.Fatal("unmodified PDF export changed the original bytes")
	}
}

func TestExportPDFUsesVisualCropBoxCoordinatesOnRotatedPage(t *testing.T) {
	original := boxedPDF(t, "[0 0 100 200]", "[10 20 90 180]", 90)
	source, err := Load("rotated.pdf", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	rect := NormalizedRect{X: .1, Y: .2, Width: .2, Height: .2}
	if _, err := doc.AddSignature(0, rect, opaqueSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := Load("rotated-signed.pdf", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := exported.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	redBounds, ok := colorBounds(preview, func(r, g, b, a uint32) bool {
		return a > 0 && r > 0xc000 && g < 0x3000 && b < 0x3000
	})
	if !ok {
		t.Fatal("rotated PDF does not contain the signature")
	}
	w, h := preview.Bounds().Dx(), preview.Bounds().Dy()
	want := image.Rect(int(.1*float64(w)), int(.2*float64(h)), int(.3*float64(w)), int(.4*float64(h)))
	if absInt(redBounds.Min.X-want.Min.X) > 4 || absInt(redBounds.Min.Y-want.Min.Y) > 4 ||
		absInt(redBounds.Max.X-want.Max.X) > 4 || absInt(redBounds.Max.Y-want.Max.Y) > 4 {
		t.Fatalf("rotated signature bounds = %v, want approximately %v on %v", redBounds, want, preview.Bounds())
	}
}

func TestExportPDFPreservesSignatureTransparency(t *testing.T) {
	original := boxedPDF(t, "[0 0 72 72]", "", 0)
	source, err := Load("transparent-signature.pdf", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddSignature(0, NormalizedRect{X: .1, Y: .1, Width: .4, Height: .2}, halfTransparentSignaturePNG(t)); err != nil {
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := Load("transparent-signature-output.pdf", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := exported.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	transparentSide := color.NRGBAModel.Convert(preview.At(20, 28)).(color.NRGBA)
	opaqueSide := color.NRGBAModel.Convert(preview.At(60, 28)).(color.NRGBA)
	if transparentSide.R < 245 || transparentSide.G < 245 || transparentSide.B < 245 {
		t.Fatalf("transparent half changed the white page: %#v", transparentSide)
	}
	if opaqueSide.R < 220 || opaqueSide.G > 30 || opaqueSide.B > 30 {
		t.Fatalf("opaque half was not rendered red: %#v", opaqueSide)
	}
}

func TestExportPDFAppliesMultipleSignaturesToTheirPages(t *testing.T) {
	original := minimalPDF(t, 2)
	source, err := Load("multiple.pdf", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	placements := []struct {
		page int
		rect NormalizedRect
	}{
		{page: 0, rect: NormalizedRect{X: .1, Y: .1, Width: .2, Height: .1}},
		{page: 0, rect: NormalizedRect{X: .6, Y: .7, Width: .2, Height: .1}},
		{page: 1, rect: NormalizedRect{X: .3, Y: .4, Width: .2, Height: .1}},
	}
	for _, placement := range placements {
		if _, err := doc.AddSignature(placement.page, placement.rect, opaqueSignaturePNG(t)); err != nil {
			t.Fatal(err)
		}
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := Load("multiple-output.pdf", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if exported.PageCount() != 2 {
		t.Fatalf("page count = %d, want 2", exported.PageCount())
	}
	for _, placement := range placements {
		preview, err := exported.Preview(placement.page)
		if err != nil {
			t.Fatal(err)
		}
		x := int((placement.rect.X + placement.rect.Width/2) * float64(preview.Bounds().Dx()))
		y := int((placement.rect.Y + placement.rect.Height/2) * float64(preview.Bounds().Dy()))
		pixel := color.NRGBAModel.Convert(preview.At(x, y)).(color.NRGBA)
		if pixel.R < 220 || pixel.G > 30 || pixel.B > 30 {
			t.Fatalf("page %d signature at (%d,%d) is %#v", placement.page+1, x, y, pixel)
		}
	}
}

func TestEncryptedPDFReturnsClearLoadError(t *testing.T) {
	original := textPDF(t, "secret")
	var encrypted bytes.Buffer
	configuration := model.NewAESConfiguration("user-password", "owner-password", 256)
	if err := api.Encrypt(context.Background(), bytes.NewReader(original), &encrypted, configuration); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("encrypted.pdf", bytes.NewReader(encrypted.Bytes())); err == nil {
		t.Fatal("encrypted PDF unexpectedly loaded without a password")
	} else if !strings.Contains(strings.ToLower(err.Error()), "encrypted") {
		t.Fatalf("encrypted PDF error is unclear: %v", err)
	}
}

func opaqueSignaturePNG(t *testing.T) []byte {
	t.Helper()
	signature := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	for y := range 10 {
		for x := range 20 {
			signature.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, signature); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func halfTransparentSignaturePNG(t *testing.T) []byte {
	t.Helper()
	signature := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	for y := range 10 {
		for x := 10; x < 20; x++ {
			signature.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, signature); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func boxedPDF(t *testing.T, mediaBox, cropBox string, rotation int) []byte {
	t.Helper()
	pageOptions := "/MediaBox " + mediaBox
	if cropBox != "" {
		pageOptions += " /CropBox " + cropBox
	}
	if rotation != 0 {
		pageOptions += fmt.Sprintf(" /Rotate %d", rotation)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R %s /Contents 4 0 R >>", pageOptions),
		"<< /Length 0 >>\nstream\n\nendstream",
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n", len(offsets))
	pdf.WriteString("0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xrefOffset)
	return pdf.Bytes()
}

func textPDF(t *testing.T, text string) []byte {
	t.Helper()
	content := fmt.Sprintf("q 0 0 m 72 72 l S Q BT /F1 10 Tf 10 36 Td (%s) Tj ET", text)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n", len(offsets))
	pdf.WriteString("0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xrefOffset)
	return pdf.Bytes()
}

func containsRed(input image.Image) bool {
	_, ok := colorBounds(input, func(r, g, b, a uint32) bool {
		return a > 0 && r > 0xc000 && g < 0x3000 && b < 0x3000
	})
	return ok
}

func colorBounds(input image.Image, matches func(r, g, b, a uint32) bool) (image.Rectangle, bool) {
	bounds := input.Bounds()
	result := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if !matches(input.At(x, y).RGBA()) {
				continue
			}
			found = true
			result.Min.X = min(result.Min.X, x)
			result.Min.Y = min(result.Min.Y, y)
			result.Max.X = max(result.Max.X, x+1)
			result.Max.Y = max(result.Max.Y, y+1)
		}
	}
	return result, found
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
