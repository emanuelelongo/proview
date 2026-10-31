// Package model3d provides a read-only STL mesh and a software viewport renderer.
package model3d

import (
	"context"
	"math"
)

type Vector struct{ X, Y, Z float64 }

func (a Vector) Sub(b Vector) Vector { return Vector{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vector) Cross(b Vector) Vector {
	return Vector{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func (a Vector) Dot(b Vector) float64   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vector) Scale(s float64) Vector { return Vector{a.X * s, a.Y * s, a.Z * s} }
func (a Vector) Unit() Vector {
	m := math.Max(math.Abs(a.X), math.Max(math.Abs(a.Y), math.Abs(a.Z)))
	if m == 0 {
		return Vector{}
	}
	a = Vector{a.X / m, a.Y / m, a.Z / m}
	return a.Scale(1 / math.Sqrt(a.Dot(a)))
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

type Triangle struct{ Vertices [3]Vector }
type Mesh struct {
	Name        string
	triangles   []Triangle
	Min, Max    Vector
	SourceBytes int64
	packed      []float32
}

func (m *Mesh) Size() Vector { return m.Max.Sub(m.Min) }
func (m *Mesh) TriangleCount() int {
	if m.packed != nil {
		return len(m.packed) / 21
	}
	return len(m.triangles)
}
func (m *Mesh) normalized(v Vector) Vector {
	size := m.Size()
	span := math.Max(size.X, math.Max(size.Y, size.Z))
	return Vector{2 * ((v.X-m.Min.X)/span - size.X/span/2), 2 * ((v.Y-m.Min.Y)/span - size.Y/span/2), 2 * ((v.Z-m.Min.Z)/span - size.Z/span/2)}
}

// Pack normalized positions and crease-aware corner normals once. The temporary
// float64 triangles and shared-vertex map are discarded after this conversion.
func (m *Mesh) prepareNormals() {
	normals := make(map[Vector]Vector, len(m.triangles))
	for _, t := range m.triangles {
		n := m.normalized(t.Vertices[1]).Sub(m.normalized(t.Vertices[0])).Cross(m.normalized(t.Vertices[2]).Sub(m.normalized(t.Vertices[0]))).Unit()
		for _, v := range t.Vertices {
			sum := normals[v]
			aligned := n
			if sum.Dot(n) < 0 {
				aligned = n.Scale(-1)
			}
			normals[v] = Vector{sum.X + aligned.X, sum.Y + aligned.Y, sum.Z + aligned.Z}
		}
	}
	for v, n := range normals {
		normals[v] = n.Unit()
	}
	m.packed = make([]float32, len(m.triangles)*21)
	for i, t := range m.triangles {
		offset := i * 21
		face := m.normalized(t.Vertices[1]).Sub(m.normalized(t.Vertices[0])).Cross(m.normalized(t.Vertices[2]).Sub(m.normalized(t.Vertices[0]))).Unit()
		for corner, v := range t.Vertices {
			position := m.normalized(v)
			normal := face
			if smooth, ok := normals[v]; ok && math.Abs(smooth.Dot(face)) >= .8 {
				normal = smooth
			}
			p := offset + corner*3
			n := offset + 9 + corner*3
			m.packed[p], m.packed[p+1], m.packed[p+2] = float32(position.X), float32(position.Y), float32(position.Z)
			m.packed[n], m.packed[n+1], m.packed[n+2] = float32(normal.X), float32(normal.Y), float32(normal.Z)
		}
		m.packed[offset+18], m.packed[offset+19], m.packed[offset+20] = float32(face.X), float32(face.Y), float32(face.Z)
	}
	m.triangles = nil
}

// GPUVertices packs normalized positions and crease-aware normals for one upload.
func (m *Mesh) GPUVertices(ctx context.Context) ([]float32, error) {
	data := make([]float32, 0, m.TriangleCount()*18)
	for i := 0; i < m.TriangleCount(); i++ {
		if i%128 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		offset := i * 21
		for corner := range 3 {
			p, n := offset+corner*3, offset+9+corner*3
			data = append(data, m.packed[p], m.packed[p+1], m.packed[p+2], m.packed[n], m.packed[n+1], m.packed[n+2])
		}
	}
	return data, nil
}
