package model3d

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const MaxFileBytes = 64 << 20
const MaxTriangles = 500_000

func Load(name string, reader io.Reader) (*Mesh, error) {
	if reader == nil {
		return nil, errors.New("STL: nil reader")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, errors.New("STL: file exceeds the 64 MB prototype limit")
	}
	m := &Mesh{Name: name, SourceBytes: int64(len(data))}
	if len(data) >= 84 && uint64(binary.LittleEndian.Uint32(data[80:84]))*50+84 == uint64(len(data)) {
		count := int(binary.LittleEndian.Uint32(data[80:84]))
		if count > MaxTriangles {
			return nil, errors.New("STL: too many triangles")
		}
		m.triangles = make([]Triangle, 0, count)
		for i := range count {
			record := data[84+i*50 : 84+(i+1)*50]
			var t Triangle
			for j := range 3 {
				p := record[12+j*12:]
				t.Vertices[j] = Vector{float64(math.Float32frombits(binary.LittleEndian.Uint32(p))), float64(math.Float32frombits(binary.LittleEndian.Uint32(p[4:]))), float64(math.Float32frombits(binary.LittleEndian.Uint32(p[8:])))}
			}
			if err = m.add(t); err != nil {
				return nil, err
			}
		}
	} else {
		if err = m.loadASCII(data); err != nil {
			return nil, err
		}
	}
	if len(m.triangles) == 0 {
		return nil, errors.New("STL: no non-degenerate triangles")
	}
	m.Min, m.Max = m.triangles[0].Vertices[0], m.triangles[0].Vertices[0]
	for _, t := range m.triangles {
		for _, v := range t.Vertices {
			m.Min = Vector{math.Min(m.Min.X, v.X), math.Min(m.Min.Y, v.Y), math.Min(m.Min.Z, v.Z)}
			m.Max = Vector{math.Max(m.Max.X, v.X), math.Max(m.Max.Y, v.Y), math.Max(m.Max.Z, v.Z)}
		}
	}
	size := m.Size()
	if !finite(size.X) || !finite(size.Y) || !finite(size.Z) {
		return nil, errors.New("STL: coordinate range is too large")
	}
	m.prepareNormals()
	return m, nil
}
func (m *Mesh) add(t Triangle) error {
	for _, v := range t.Vertices {
		if !finite(v.X) || !finite(v.Y) || !finite(v.Z) {
			return errors.New("STL: non-finite coordinate")
		}
	}
	a, b := t.Vertices[1].Sub(t.Vertices[0]), t.Vertices[2].Sub(t.Vertices[0])
	if !finite(a.X) || !finite(a.Y) || !finite(a.Z) || !finite(b.X) || !finite(b.Y) || !finite(b.Z) {
		return errors.New("STL: coordinate range is too large")
	}
	if a.Unit().Cross(b.Unit()).Dot(a.Unit().Cross(b.Unit())) < 1e-24 {
		return nil
	}
	if len(m.triangles) >= MaxTriangles {
		return errors.New("STL: too many triangles")
	}
	m.triangles = append(m.triangles, t)
	return nil
}
func parseVector(fields []string) (Vector, error) {
	if len(fields) != 3 {
		return Vector{}, errors.New("expected three coordinates")
	}
	var values [3]float64
	for i, s := range fields {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || !finite(v) {
			return Vector{}, errors.New("invalid number")
		}
		values[i] = v
	}
	return Vector{values[0], values[1], values[2]}, nil
}
func (m *Mesh) loadASCII(data []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 64*1024)
	state, line, facets := 0, 0, 0
	var t Triangle
	ended := false
	for scanner.Scan() {
		line++
		f := strings.Fields(scanner.Text())
		if len(f) == 0 {
			continue
		}
		fail := func() error { return fmt.Errorf("STL: invalid ASCII syntax at line %d", line) }
		switch state {
		case 0:
			if f[0] != "solid" {
				return fail()
			}
			state = 1
			ended = false
		case 1:
			if f[0] == "endsolid" {
				state = 0
				ended = true
				continue
			}
			if len(f) != 5 || f[0] != "facet" || f[1] != "normal" {
				return fail()
			}
			if _, err := parseVector(f[2:]); err != nil {
				return fail()
			}
			state = 2
		case 2:
			if len(f) != 2 || f[0] != "outer" || f[1] != "loop" {
				return fail()
			}
			state = 3
		case 3, 4, 5:
			if len(f) != 4 || f[0] != "vertex" {
				return fail()
			}
			v, err := parseVector(f[1:])
			if err != nil {
				return fail()
			}
			t.Vertices[state-3] = v
			state++
		case 6:
			if len(f) != 1 || f[0] != "endloop" {
				return fail()
			}
			state = 7
		case 7:
			if len(f) != 1 || f[0] != "endfacet" {
				return fail()
			}
			facets++
			if facets > MaxTriangles {
				return errors.New("STL: too many triangles")
			}
			if err := m.add(t); err != nil {
				return err
			}
			state = 1
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("STL: %w", err)
	}
	if state != 0 || !ended {
		return errors.New("STL: incomplete ASCII file or invalid binary length")
	}
	return nil
}
