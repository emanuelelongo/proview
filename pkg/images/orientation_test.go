package images

import (
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func exifJPEG(order binary.ByteOrder, orientation uint16) []byte {
	tiff := make([]byte, 26)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 1)
	order.PutUint16(tiff[10:], 0x112)
	order.PutUint16(tiff[12:], 3)
	order.PutUint32(tiff[14:], 1)
	order.PutUint16(tiff[18:], orientation)
	result := []byte{0xff, 0xd8, 0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0}
	return append(append(result, tiff...), 0xff, 0xd9)
}
func TestJPEGOrientationAllCameraTransforms(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 3, 2))
	for i := 0; i < 6; i++ {
		src.SetGray(i%3, i/3, color.Gray{Y: uint8(i + 1)})
	}
	expected := [][]uint8{{1, 2, 3, 4, 5, 6}, {3, 2, 1, 6, 5, 4}, {6, 5, 4, 3, 2, 1}, {4, 5, 6, 1, 2, 3}, {1, 4, 2, 5, 3, 6}, {4, 1, 5, 2, 6, 3}, {6, 3, 5, 2, 4, 1}, {3, 6, 2, 5, 1, 4}}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for orientation := uint16(1); orientation <= 8; orientation++ {
			out := JPEGOrientation(exifJPEG(order, orientation)).Apply(src)
			w, h := 3, 2
			if orientation >= 5 {
				w, h = h, w
			}
			if out.Bounds().Size() != image.Pt(w, h) {
				t.Fatalf("orientation %d size %v", orientation, out.Bounds())
			}
			for i, want := range expected[orientation-1] {
				got := color.GrayModel.Convert(out.At(i%w, i/w)).(color.Gray).Y
				if got != want {
					t.Fatalf("orientation %d pixel %d: %d != %d", orientation, i, got, want)
				}
			}
		}
	}
}
func TestJPEGOrientationMalformedAndUnrelatedMetadata(t *testing.T) {
	valid := exifJPEG(binary.LittleEndian, 6)
	for n := 0; n < len(valid)-2; n++ {
		if got := JPEGOrientation(valid[:n]); got != (Transform{}) {
			t.Fatalf("accepted truncated segment at %d", n)
		}
	}
	for _, change := range []func([]byte){
		func(b []byte) { b[4] = 0xff; b[5] = 0xff },                               // segment length
		func(b []byte) { b[12] = '?' },                                            // TIFF byte order
		func(b []byte) { b[16] = 0xff; b[17] = 0xff; b[18] = 0xff; b[19] = 0xff }, // IFD offset
		func(b []byte) { b[20] = 0xff; b[21] = 0xff },                             // entry count
		func(b []byte) { b[24] = 4 },                                              // wrong orientation type
		func(b []byte) { b[26] = 2 },                                              // wrong value count
		func(b []byte) { b[30] = 9 },                                              // invalid orientation
	} {
		data := append([]byte(nil), valid...)
		change(data)
		if JPEGOrientation(data) != (Transform{}) {
			t.Fatal("accepted malformed orientation")
		}
	}
	// XMP APP1 before EXIF must not hide the main orientation.
	data := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, 6, 'X', 'M', 'P', 0}, valid[2:]...)
	if JPEGOrientation(data) != (Transform{Turns: 1}) {
		t.Fatal("unrelated APP1 hid EXIF")
	}
	// Never inspect markers found inside compressed scan bytes.
	data = append([]byte{0xff, 0xd8, 0xff, 0xda}, valid[2:]...)
	if JPEGOrientation(data) != (Transform{}) {
		t.Fatal("read orientation from scan data")
	}
}
func FuzzJPEGOrientation(f *testing.F) {
	f.Add(exifJPEG(binary.LittleEndian, 6))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		tr := JPEGOrientation(data)
		if tr.Turns < 0 || tr.Turns > 3 {
			t.Fatal(tr)
		}
	})
}
