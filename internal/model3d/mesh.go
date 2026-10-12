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
	Triangles   []Triangle
	Min, Max    Vector
	SourceBytes int64
	normals     map[Vector]Vector
}

func (m *Mesh) Size() Vector { return m.Max.Sub(m.Min) }
func (m *Mesh) normalized(v Vector) Vector {
	size := m.Size()
	span := math.Max(size.X, math.Max(size.Y, size.Z))
	return Vector{2 * ((v.X-m.Min.X)/span - size.X/span/2), 2 * ((v.Y-m.Min.Y)/span - size.Y/span/2), 2 * ((v.Z-m.Min.Z)/span - size.Z/span/2)}
}

// Shared-vertex normals smooth shallow facets; the renderer retains face normals
// across sharp corners. Precompute once so camera updates only read mesh data.
func (m *Mesh) prepareNormals() {
	m.normals = make(map[Vector]Vector)
	for _, t := range m.Triangles {
		n := m.normalized(t.Vertices[1]).Sub(m.normalized(t.Vertices[0])).Cross(m.normalized(t.Vertices[2]).Sub(m.normalized(t.Vertices[0]))).Unit()
		for _, v := range t.Vertices {
			sum := m.normals[v]
			aligned := n
			if sum.Dot(n) < 0 {
				aligned = n.Scale(-1)
			}
			m.normals[v] = Vector{sum.X + aligned.X, sum.Y + aligned.Y, sum.Z + aligned.Z}
		}
	}
	for v, n := range m.normals {
		m.normals[v] = n.Unit()
	}
}

// GPUVertices packs normalized positions and crease-aware normals for one upload.
func (m *Mesh) GPUVertices(ctx context.Context) ([]float32, error) {
	data := make([]float32, 0, len(m.Triangles)*18)
	for i, t := range m.Triangles {
		if i%128 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		face := m.normalized(t.Vertices[1]).Sub(m.normalized(t.Vertices[0])).Cross(m.normalized(t.Vertices[2]).Sub(m.normalized(t.Vertices[0]))).Unit()
		for _, v := range t.Vertices {
			normal := face
			if n, ok := m.normals[v]; ok && math.Abs(n.Dot(face)) >= .8 {
				normal = n
			}
			p := m.normalized(v)
			data = append(data, float32(p.X), float32(p.Y), float32(p.Z), float32(normal.X), float32(normal.Y), float32(normal.Z))
		}
	}
	return data, nil
}
