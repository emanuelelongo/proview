package model3d

import "math"

const (
	cornerSnapPixels = 12.0
	edgeSnapPixels   = 8.0
)

// PickSnapped selects a visible surface point, then attracts nearby clicks to
// corners and boundary or sharp edges of the hit triangle. Smooth tessellation
// seams do not become measurement targets.
func PickSnapped(m *Mesh, c Camera, w, h int, x, y, displayScale float64) (Vector, bool) {
	point, triangle, hit := pickSurface(m, c, w, h, x, y)
	if !hit {
		return Vector{}, false
	}
	if !finite(displayScale) || displayScale <= 0 {
		displayScale = 1
	}
	cornerRadius := cornerSnapPixels / displayScale
	edgeRadius := edgeSnapPixels / displayScale
	span := modelSpan(m)
	center := Vector{(m.Min.X + m.Max.X) / 2, (m.Min.Y + m.Max.Y) / 2, (m.Min.Z + m.Max.Z) / 2}
	vertices := meshTriangleVertices(m, triangle, center, span)
	projected := [3]Vector{}
	for i, vertex := range vertices {
		projected[i] = Project(m, c, vertex, w, h)
	}
	// Most clicks are well inside a face; avoid scanning neighboring triangles.
	nearFeature := false
	for edge := range projected {
		a, b := projected[edge], projected[(edge+1)%3]
		if pixelDistanceSquared(a.X, a.Y, x, y) <= cornerRadius*cornerRadius ||
			segmentDistanceSquared(a, b, x, y) <= edgeRadius*edgeRadius {
			nearFeature = true
			break
		}
	}
	if !nearFeature {
		return point, true
	}
	features := triangleFeatureEdges(m, triangle, vertices, center, span)
	bestCorner := cornerRadius * cornerRadius
	corner := -1
	for edge, feature := range features {
		if !feature {
			continue
		}
		for _, i := range [2]int{edge, (edge + 1) % 3} {
			dx, dy := projected[i].X-x, projected[i].Y-y
			if distance := dx*dx + dy*dy; distance <= bestCorner {
				bestCorner, corner = distance, i
			}
		}
	}
	if corner >= 0 {
		return vertices[corner], true
	}
	bestEdge := edgeRadius * edgeRadius
	var snapped Vector
	found := false
	for edge, feature := range features {
		if !feature {
			continue
		}
		a, b := projected[edge], projected[(edge+1)%3]
		dx, dy := b.X-a.X, b.Y-a.Y
		lengthSquared := dx*dx + dy*dy
		if lengthSquared <= 0 {
			continue
		}
		t := max(0., min(1., ((x-a.X)*dx+(y-a.Y)*dy)/lengthSquared))
		px, py := a.X+t*dx, a.Y+t*dy
		distance := (px-x)*(px-x) + (py-y)*(py-y)
		if distance <= bestEdge {
			bestEdge = distance
			v0, v1 := vertices[edge], vertices[(edge+1)%3]
			snapped = Vector{v0.X + t*(v1.X-v0.X), v0.Y + t*(v1.Y-v0.Y), v0.Z + t*(v1.Z-v0.Z)}
			found = true
		}
	}
	if found {
		return snapped, true
	}
	return point, true
}

func pixelDistanceSquared(px, py, x, y float64) float64 {
	return (px-x)*(px-x) + (py-y)*(py-y)
}

func segmentDistanceSquared(a, b Vector, x, y float64) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	lengthSquared := dx*dx + dy*dy
	if lengthSquared <= 0 {
		return pixelDistanceSquared(a.X, a.Y, x, y)
	}
	t := max(0., min(1., ((x-a.X)*dx+(y-a.Y)*dy)/lengthSquared))
	return pixelDistanceSquared(a.X+t*dx, a.Y+t*dy, x, y)
}

func meshTriangleVertices(m *Mesh, index int, center Vector, span float64) [3]Vector {
	if m.packed == nil {
		return m.triangles[index].Vertices
	}
	var vertices [3]Vector
	for corner := range vertices {
		offset := index*21 + corner*3
		vertices[corner] = Vector{
			center.X + float64(m.packed[offset])*span/2,
			center.Y + float64(m.packed[offset+1])*span/2,
			center.Z + float64(m.packed[offset+2])*span/2,
		}
	}
	return vertices
}

func triangleFeatureEdges(m *Mesh, selected int, vertices [3]Vector, center Vector, span float64) [3]bool {
	normal := vertices[1].Sub(vertices[0]).Cross(vertices[2].Sub(vertices[0])).Unit()
	var adjacent [3]bool
	var feature [3]bool
	for i := 0; i < m.TriangleCount(); i++ {
		if i == selected {
			continue
		}
		other := meshTriangleVertices(m, i, center, span)
		otherNormal := other[1].Sub(other[0]).Cross(other[2].Sub(other[0])).Unit()
		for edge := range feature {
			if !sharesEdge(vertices[edge], vertices[(edge+1)%3], other) {
				continue
			}
			adjacent[edge] = true
			if math.Abs(normal.Dot(otherNormal)) < math.Cos(math.Pi/6) {
				feature[edge] = true
			}
		}
	}
	for edge := range feature {
		feature[edge] = feature[edge] || !adjacent[edge]
	}
	return feature
}

func sharesEdge(a, b Vector, triangle [3]Vector) bool {
	for edge := range triangle {
		c, d := triangle[edge], triangle[(edge+1)%3]
		if a == c && b == d || a == d && b == c {
			return true
		}
	}
	return false
}
