//go:build ignore

// Regenerate with: go run packaging/windows/icon.go assets/app-icon.png packaging/windows/proview.ico
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	input, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	source, err := png.Decode(input)
	input.Close()
	if err != nil {
		panic(err)
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	var directory, images bytes.Buffer
	binary.Write(&directory, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	for _, size := range sizes {
		icon := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(icon, icon.Bounds(), source, source.Bounds(), draw.Src, nil)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, icon); err != nil {
			panic(err)
		}
		directory.Write([]byte{byte(size % 256), byte(size % 256), 0, 0})
		binary.Write(&directory, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&directory, binary.LittleEndian, [2]uint32{uint32(encoded.Len()), uint32(6 + 16*len(sizes) + images.Len())})
		images.Write(encoded.Bytes())
	}
	if err := os.WriteFile(os.Args[2], append(directory.Bytes(), images.Bytes()...), 0644); err != nil {
		panic(err)
	}
}
