package model3d

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

const asciiTriangle = "solid test\nfacet normal 0 0 1\nouter loop\nvertex 0 0 0\nvertex 2 0 0\nvertex 0 1 0\nendloop\nendfacet\nendsolid test\n"

func binaryMesh(vertices [][3]Vector) []byte {
	data := make([]byte, 84+50*len(vertices))
	copy(data, "solid binary header")
	binary.LittleEndian.PutUint32(data[80:], uint32(len(vertices)))
	for i, t := range vertices {
		for j, v := range t {
			for k, n := range []float64{v.X, v.Y, v.Z} {
				binary.LittleEndian.PutUint32(data[84+i*50+12+j*12+k*4:], math.Float32bits(float32(n)))
			}
		}
	}
	return data
}
func TestASCIIAndBinarySTL(t *testing.T) {
	triangle := [3]Vector{{0, 0, 0}, {2, 0, 0}, {0, 1, 0}}
	for _, data := range [][]byte{[]byte(asciiTriangle), binaryMesh([][3]Vector{triangle})} {
		m, err := Load("shape.stl", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if m.TriangleCount() != 1 || m.Size() != (Vector{2, 1, 0}) || m.SourceBytes != int64(len(data)) {
			t.Fatalf("unexpected mesh: %+v", m)
		}
		if len(m.triangles) != 0 || len(m.packed) != 21 {
			t.Fatal("full-precision source triangles were retained after packing")
		}
		for i, value := range []float32{1, -.5, 0} {
			if m.packed[3+i] != value {
				t.Fatal("normalized packed coordinates changed")
			}
		}
	}
}
func TestRejectMalformedSTL(t *testing.T) {
	valid := binaryMesh([][3]Vector{{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}})
	badCount := bytes.Clone(valid)
	binary.LittleEndian.PutUint32(badCount[80:], math.MaxUint32)
	cases := [][]byte{nil, []byte("solid test\nvertex 0 0 0\nendsolid test"), []byte(strings.Replace(asciiTriangle, "vertex 2 0 0", "vertex NaN 0 0", 1)), []byte(strings.Replace(asciiTriangle, "vertex 2 0 0", "vertex nope 0 0", 1)), []byte(strings.Replace(asciiTriangle, "endfacet", "", 1)), valid[:len(valid)-1], badCount, binaryMesh([][3]Vector{{{0, 0, 0}, {math.Inf(1), 0, 0}, {0, 1, 0}}})}
	for i, data := range cases {
		if _, err := Load("bad.stl", bytes.NewReader(data)); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	if _, err := Load("nil.stl", nil); err == nil {
		t.Fatal("nil reader accepted")
	}
}
func TestDegenerateTrianglesAndTinyCoordinates(t *testing.T) {
	data := strings.Replace(asciiTriangle, "vertex 2 0 0", "vertex 0 0 0", 1)
	if _, err := Load("empty.stl", strings.NewReader(data)); err == nil {
		t.Fatal("empty geometry accepted")
	}
	tiny := strings.ReplaceAll(strings.ReplaceAll(asciiTriangle, "vertex 2 0 0", "vertex 2e-300 0 0"), "vertex 0 1 0", "vertex 0 1e-300 0")
	m, err := Load("tiny.stl", strings.NewReader(tiny))
	if err != nil {
		t.Fatal(err)
	}
	v := Vector{float64(m.packed[3]), float64(m.packed[4]), float64(m.packed[5])}
	if !finite(v.X) || !finite(v.Y) || !finite(v.Z) {
		t.Fatal("tiny geometry normalization is invalid")
	}
}
func FuzzSTLLoad(f *testing.F) {
	f.Add([]byte(asciiTriangle))
	f.Add(binaryMesh([][3]Vector{{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}}))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = Load("fuzz.stl", bytes.NewReader(data))
	})
}
