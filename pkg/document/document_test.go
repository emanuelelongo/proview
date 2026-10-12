package document

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

func TestLoadPNGPreservesOriginal(t *testing.T) {
	inputImage := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	inputImage.SetNRGBA(1, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 40})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, inputImage); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), encoded.Bytes()...)

	source, err := LoadFrom("signature.PNG", "file:///tmp/signature.PNG", bytes.NewReader(original))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if source.Format() != FormatPNG {
		t.Fatalf("Format = %q, want %q", source.Format(), FormatPNG)
	}
	if source.Name() != "signature.PNG" || source.Location() != "file:///tmp/signature.PNG" {
		t.Fatalf("source identity = %q, %q", source.Name(), source.Location())
	}
	preview, err := source.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if source.PageCount() != 1 || preview.Bounds() != inputImage.Bounds() {
		t.Fatalf("PageCount = %d, bounds = %v, want one 3x2 page", source.PageCount(), preview.Bounds())
	}

	mutable, ok := preview.(interface {
		Set(int, int, color.Color)
	})
	if !ok {
		t.Fatal("decoded PNG is not mutable")
	}
	mutable.Set(1, 1, color.Black)
	if !bytes.Equal(source.Original(), original) {
		t.Fatal("editing the preview changed the original bytes")
	}
	returnedOriginal := source.Original()
	returnedOriginal[0] = 0
	if bytes.Equal(source.Original(), returnedOriginal) {
		t.Fatal("Original returned the source's mutable byte slice")
	}
}

func TestLoadJPEG(t *testing.T) {
	inputImage := image.NewRGBA(image.Rect(0, 0, 4, 3))
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, inputImage, nil); err != nil {
		t.Fatal(err)
	}

	source, err := Load("photo.JpEg", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if source.Format() != FormatJPEG {
		t.Fatalf("Format = %q, want %q", source.Format(), FormatJPEG)
	}
	preview, err := source.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := preview.Bounds(); got.Dx() != 4 || got.Dy() != 3 {
		t.Fatalf("preview bounds = %v, want 4x3", got)
	}
}

func TestLoadPDF(t *testing.T) {
	original := minimalPDF(t, 2)

	source, err := Load("two-pages.pdf", bytes.NewReader(original))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if source.Format() != FormatPDF {
		t.Fatalf("Format = %q, want %q", source.Format(), FormatPDF)
	}
	if source.PageCount() != 2 {
		t.Fatalf("page count = %d, want 2", source.PageCount())
	}
	if source.pages[0].preview != nil || source.pages[1].preview != nil {
		t.Fatal("PDF previews were rendered eagerly")
	}
	for i := 0; i < source.PageCount(); i++ {
		preview, err := source.Preview(i)
		if err != nil {
			t.Fatal(err)
		}
		if preview == nil || preview.Bounds().Empty() {
			t.Fatalf("page %d has no preview", i+1)
		}
	}
	if source.pages[0].preview == nil || source.pages[1].preview == nil {
		t.Fatal("PDF previews were not cached after rendering")
	}
	if source.pdf == nil {
		t.Fatal("PDF renderer was not retained for subsequent pages")
	}
	if !bytes.Equal(source.Original(), original) {
		t.Fatal("original PDF bytes were not preserved")
	}
}

func TestPDFRendererReopensAfterClose(t *testing.T) {
	source, err := Load("two-pages.pdf", bytes.NewReader(minimalPDF(t, 2)))
	if err != nil {
		t.Fatal(err)
	}
	source.Close()
	if source.pdf != nil {
		t.Fatal("PDF renderer remained open")
	}
	if _, err := source.Preview(1); err != nil {
		t.Fatalf("render page after close: %v", err)
	}
	if source.pdf == nil {
		t.Fatal("PDF renderer was not reopened")
	}
	source.Close()
}

func TestPDFThumbnailDoesNotRenderFullPage(t *testing.T) {
	source, err := Load("two-pages.pdf", bytes.NewReader(minimalPDF(t, 2)))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	thumbnail, err := source.Thumbnail(1)
	if err != nil {
		t.Fatal(err)
	}
	if source.pages[1].preview != nil {
		t.Fatal("rendering a thumbnail also rendered the full page")
	}
	if size, _ := source.PageSize(1); thumbnail.Bounds().Dx() >= size.X {
		t.Fatalf("thumbnail width = %d, full page width = %d", thumbnail.Bounds().Dx(), size.X)
	}
	again, err := source.Thumbnail(1)
	if err != nil || again != thumbnail {
		t.Fatalf("thumbnail was not cached: image = %v, error = %v", again, err)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		reader   io.Reader
	}{
		{name: "nil reader", fileName: "image.png"},
		{name: "read failure", fileName: "image.png", reader: failingReader{}},
		{name: "unsupported extension", fileName: "image.xyz", reader: bytes.NewReader(nil)},
		{name: "invalid PNG", fileName: "image.png", reader: bytes.NewReader([]byte("not a PNG"))},
		{name: "invalid PDF", fileName: "document.pdf", reader: bytes.NewReader([]byte("not a PDF"))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := Load(test.fileName, test.reader)
			if err == nil {
				t.Fatalf("Load() = %#v, want error", source)
			}
			if source != nil {
				t.Fatalf("Load() returned partial source: %#v", source)
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func minimalPDF(t *testing.T, pageCount int) []byte {
	t.Helper()

	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
	}

	pageObjects := make([]int, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pageObjects = append(pageObjects, 3+page*2)
	}
	var kids bytes.Buffer
	for _, objectNumber := range pageObjects {
		fmt.Fprintf(&kids, "%d 0 R ", objectNumber)
	}
	objects = append(objects, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), pageCount))
	for page := 0; page < pageCount; page++ {
		contentObject := 4 + page*2
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Contents %d 0 R >>", contentObject),
			"<< /Length 0 >>\nstream\n\nendstream",
		)
	}

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
