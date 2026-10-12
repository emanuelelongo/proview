package document

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"sort"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func TestCommonImageFormatsLoadAndExport(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := range 2 {
		for x := range 3 {
			pixels.SetNRGBA(x, y, color.NRGBA{R: 180, G: 40, B: 90, A: 128})
		}
	}
	cases := []struct {
		name   string
		format Format
		encode func(*bytes.Buffer) error
	}{
		{"sample.GIF", FormatGIF, func(b *bytes.Buffer) error { return gif.Encode(b, pixels, nil) }},
		{"sample.tif", FormatTIFF, func(b *bytes.Buffer) error { return tiff.Encode(b, pixels, nil) }},
		{"sample.TIFF", FormatTIFF, func(b *bytes.Buffer) error { return tiff.Encode(b, pixels, &tiff.Options{Compression: tiff.Deflate}) }},
		{"sample.bmp", FormatBMP, func(b *bytes.Buffer) error { return bmp.Encode(b, pixels) }},
		{"sample.webp", FormatWebP, func(b *bytes.Buffer) error {
			data, err := os.ReadFile("testdata/small.webp")
			if err == nil {
				_, err = b.Write(data)
			}
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var encoded bytes.Buffer
			if err := tc.encode(&encoded); err != nil {
				t.Fatal(err)
			}
			source, err := Load(tc.name, bytes.NewReader(encoded.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := New(source)
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Close()
			preview, err := doc.Preview(0)
			if err != nil {
				t.Fatal(err)
			}
			if doc.Format() != tc.format || preview.Bounds().Size() != pixels.Bounds().Size() || !bytes.Equal(source.Original(), encoded.Bytes()) {
				t.Fatal("format, dimensions, or original bytes changed")
			}
			if tc.format == FormatTIFF || tc.format == FormatWebP {
				_, _, _, alpha := preview.At(0, 0).RGBA()
				if alpha != 128*257 {
					t.Fatalf("transparency lost: alpha=%d", alpha)
				}
			}
			output, err := doc.Export(ExportOptions{Format: FormatPNG})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := png.Decode(bytes.NewReader(output)); err != nil {
				t.Fatal(err)
			}
			if CanExport(tc.format) {
				t.Fatal("import-only image format treated as writable")
			}
		})
	}
}

func TestCorruptCommonImageFormatsFail(t *testing.T) {
	for _, name := range []string{"bad.gif", "bad.tif", "bad.bmp", "bad.webp"} {
		if _, err := Load(name, bytes.NewReader([]byte("invalid image"))); err == nil {
			t.Fatalf("accepted corrupt %s", name)
		}
	}
}

func TestTIFFOrientationIsNormalized(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	var encoded bytes.Buffer
	if err := tiff.Encode(&encoded, pixels, nil); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	order := binary.LittleEndian
	ifd := int(order.Uint32(data[4:8]))
	count := int(order.Uint16(data[ifd : ifd+2]))
	// Append a replacement first directory; existing pixel/strip offsets stay valid.
	directory := make([]byte, 2+(count+1)*12+4)
	order.PutUint16(directory[:2], uint16(count+1))
	copy(directory[2:], data[ifd+2:ifd+2+count*12])
	entry := directory[2+count*12:]
	order.PutUint16(entry[:2], 0x112)
	order.PutUint16(entry[2:4], 3)
	order.PutUint32(entry[4:8], 1)
	order.PutUint16(entry[8:10], 6)
	entries := make([][]byte, count+1)
	for i := range entries {
		entries[i] = bytes.Clone(directory[2+i*12 : 2+(i+1)*12])
	}
	sort.Slice(entries, func(i, j int) bool { return order.Uint16(entries[i][:2]) < order.Uint16(entries[j][:2]) })
	for i, e := range entries {
		copy(directory[2+i*12:], e)
	}
	order.PutUint32(data[4:8], uint32(len(data)))
	data = append(data, directory...)
	source, err := Load("rotated.tiff", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	preview, err := source.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Bounds().Size() != image.Pt(2, 3) {
		t.Fatal("TIFF orientation was not applied")
	}
}

func TestGIFShowsFirstFrame(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 3, 2), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	second.SetColorIndex(0, 0, 1)
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	source, err := Load("animated.gif", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	preview, err := source.Preview(0)
	if err != nil {
		t.Fatal(err)
	}
	if source.PageCount() != 1 || color.GrayModel.Convert(preview.At(0, 0)).(color.Gray).Y != 0 {
		t.Fatal("GIF did not use the first frame")
	}
}
