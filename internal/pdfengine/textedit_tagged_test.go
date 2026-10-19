//go:build cgo && !nocgo && !extlib

package pdfengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Two independently tagged pages deliberately share the original content stream.
// ParentTree backlinks, an ID tree and structure-level alternative strings must
// survive movement, including the native writer's object renumbering.
func taggedMoveFixture(content, resources string, extra ...string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 7 0 R /Lang (en-US) >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /StructParents 0 /Resources << /Font << /F1 4 0 R >> " + resources + " >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fixtureStream(content, ""),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] /StructParents 1 /Resources << /Font << /F1 4 0 R >> " + resources + " >> /Contents 5 0 R >>",
		"<< /Type /StructTreeRoot /K [8 0 R 9 0 R] /ParentTree 10 0 R /ParentTreeNextKey 2 /IDTree 11 0 R >>",
		"<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K 0 /ID (first) /T (Title) /Alt (PUBLIC SECRET KEEP) >>",
		"<< /Type /StructElem /S /P /P 7 0 R /Pg 6 0 R /K 0 /ID (second) /ActualText (PUBLIC SECRET KEEP) >>",
		"<< /Nums [0 [8 0 R] 1 [9 0 R]] >>",
		"<< /Names [(first) 8 0 R (second) 9 0 R] >>",
	}
	return fixturePDF(append(objects, extra...), "")
}

// Canonicalize the complete structure graph, including cycles/backlinks, without
// depending on indirect object numbers. Page references use stable page numbers.
func taggedStructure(t *testing.T, data []byte) string {
	t.Helper()
	ctx, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(data), model.NewDefaultConfiguration(), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ctx.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string]int{}
	for page := 1; page <= ctx.PageCount; page++ {
		_, ref, _, err := ctx.PageDict(context.Background(), page, false)
		if err != nil {
			t.Fatal(err)
		}
		pages[ref.String()] = page
	}
	seen := map[string]int{}
	var visit func(types.Object) string
	visit = func(obj types.Object) string {
		switch obj := obj.(type) {
		case nil:
			return "null"
		case types.IndirectRef:
			key := obj.String()
			if page, ok := pages[key]; ok {
				return fmt.Sprintf("page:%d", page)
			}
			if id, ok := seen[key]; ok {
				return fmt.Sprintf("ref:%d", id)
			}
			id := len(seen)
			seen[key] = id
			value, err := ctx.Dereference(obj)
			if err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("object:%d:%s", id, visit(value))
		case types.Dict:
			keys := make([]string, 0, len(obj))
			for key, value := range obj {
				resolved, err := ctx.Dereference(value)
				if err != nil {
					t.Fatal(err)
				}
				// An explicit null dictionary value is equivalent to absence;
				// the native writer drops such entries during garbage collection.
				if resolved == nil {
					continue
				}
				keys = append(keys, key)
			}
			sort.Strings(keys)
			var b strings.Builder
			for _, key := range keys {
				fmt.Fprintf(&b, "%s={%s};", key, visit(obj[key]))
			}
			return b.String()
		case types.Array:
			var b strings.Builder
			for _, value := range obj {
				fmt.Fprintf(&b, "[%s]", visit(value))
			}
			return b.String()
		case types.StringLiteral, types.HexLiteral:
			value, err := ctx.DereferenceText(obj)
			if err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("text:%q", value)
		default:
			return obj.String()
		}
	}
	return visit(catalog["StructTreeRoot"]) + ";marked=" + visit(catalog["MarkInfo"]) + ";lang=" + visit(catalog["Lang"])
}

func taggedPageContent(t *testing.T, data []byte, page int) string {
	t.Helper()
	ctx, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(data), model.NewDefaultConfiguration(), nil)
	if err != nil {
		t.Fatal(err)
	}
	dict, _, _, err := ctx.PageDict(context.Background(), page+1, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ctx.PageContent(dict, page+1)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestTextCorrectionTaggedMovePreservesStructure(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"one-object", "BT /F1 16 Tf 105 140 Td (SECRET) Tj ET"},
		{"glyph-objects", "BT /F1 16 Tf 105 140 Td (S) Tj ET BT /F1 16 Tf 116 140 Td (E) Tj ET BT /F1 16 Tf 127 140 Td (C) Tj ET BT /F1 16 Tf 139 140 Td (R) Tj ET BT /F1 16 Tf 151 140 Td (E) Tj ET BT /F1 16 Tf 162 140 Td (T) Tj ET"},
		{"spacing", "BT /F1 16 Tf .375 Tc -.125 Tw 105 140 Td [(SE) 35 (CRET)] TJ ET"},
		{"transformed", "q .5 0 0 .5 10 5 cm BT /F1 32 Tf 190 270 Td (SECRET) Tj ET Q"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "q .85 .9 .7 rg 0 0 420 200 re f Q /P << /MCID 0 >> BDC BT /F1 16 Tf 30 140 Td (PUBLIC) Tj ET " + tc.text + " BT /F1 16 Tf 240 140 Td (KEEP) Tj ET EMC 0 0 m 20 20 l S"
			input := taggedMoveFixture(content, "")
			beforeStructure := taggedStructure(t, input)
			beforeLayout, start, count := probeRange(t, input, 0, "SECRET")
			for _, delta := range []Point{{}, {X: 5, Y: 25}} {
				out := probeCorrect(t, input, 0, "SECRET", "SECRET", delta.X, delta.Y)
				if taggedStructure(t, out) != beforeStructure {
					t.Fatal("structure tree, parent/ID tree or alternative strings changed")
				}
				if !reflect.DeepEqual(probeImage(t, input, 1), probeImage(t, out, 1)) {
					t.Fatal("shared content changed on the other page")
				}
				probeSameWordPosition(t, input, out, 0, "PUBLIC")
				probeSameWordPosition(t, input, out, 0, "KEEP")
				afterLayout, afterStart, _ := probeRange(t, out, 0, "SECRET")
				for i := 0; i < count; i++ {
					a, b := beforeLayout.Glyphs[start+i], afterLayout.Glyphs[afterStart+i]
					if math.Abs((b.Origin.X-a.Origin.X)*beforeLayout.Width-delta.X) > .002 || math.Abs((b.Origin.Y-a.Origin.Y)*beforeLayout.Height-delta.Y) > .002 {
						t.Fatal("original glyph position/spacing was not translated exactly")
					}
				}
				// Text-showing strings and marked-content operators retain their
				// original logical order, including a word split across BT/ETs.
				ops := regexp.MustCompile(`\([^)]*\)|/MCID\s+\d+|\b(?:BDC|EMC)\b`)
				if !reflect.DeepEqual(ops.FindAllString(taggedPageContent(t, input, 0), -1), ops.FindAllString(taggedPageContent(t, out, 0), -1)) {
					t.Fatal("marked content or original text-showing order changed")
				}
				before, after := probeImage(t, input, 0), probeImage(t, out, 0)
				if delta == (Point{}) {
					if !reflect.DeepEqual(before, after) {
						t.Fatal("zero move changed rendered pixels")
					}
				} else {
					changed := false
					for y := 0; y < before.Bounds().Dy(); y++ {
						for x := 0; x < before.Bounds().Dx(); x++ {
							if before.At(x, y) != after.At(x, y) {
								changed = true
								if x < 200 || x > 365 || y < 85 || y > 175 {
									t.Fatalf("unrelated pixel changed at %d,%d", x, y)
								}
							}
						}
					}
					if !changed {
						t.Fatal("move did not change pixels")
					}
					second := probeCorrect(t, out, 0, "SECRET", "SECRET", 0, 10)
					if taggedStructure(t, second) != beforeStructure {
						t.Fatal("repeated move changed structure")
					}
				}
			}
		})
	}
}

func TestTextCorrectionTaggedMoveRejectsUnsupported(t *testing.T) {
	for _, tc := range []struct{ name, content, replacement string }{
		{"replacement", "BT /F1 16 Tf 30 140 Td (SECRET) Tj ET", "EDIT"},
		{"delete", "BT /F1 16 Tf 30 140 Td (SECRET) Tj ET", ""},
		{"partial-object", "BT /F1 16 Tf 30 140 Td (SECRET KEEP) Tj ET", "SECRET"},
		{"state-in-text", "BT /F1 16 Tf 30 140 Td q (SECRET) Tj Q ET", "SECRET"},
		{"ctm-in-text", "BT /F1 16 Tf 1 0 0 1 0 0 cm 30 140 Td (SECRET) Tj ET", "SECRET"},
		{"alternative-text", "/Span << /ActualText (SECRET) >> BDC BT /F1 16 Tf 30 140 Td (SECRET) Tj ET EMC", "SECRET"},
		{"ambiguous", "BT /F1 16 Tf 30 140 Td (SECRET) Tj ET BT /F1 16 Tf 30 140 Td (SECRET) Tj ET", "SECRET"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := taggedMoveFixture("/P << /MCID 0 >> BDC "+tc.content+" EMC", "")
			_, start, count := probeRange(t, input, 0, "SECRET")
			if out, err := CorrectText(input, 0, start, count, tc.replacement, 0, 25); err == nil || out != nil {
				t.Fatal("unsupported tagged edit produced output")
			} else {
				t.Log(err)
			}
		})
	}
	input := taggedMoveFixture("/P << /MCID 0 >> BDC BT /F1 16 Tf 30 140 Td (SECRET) Tj ET BT /F1 16 Tf 160 140 Td (KEEP) Tj ET EMC", "")
	layout, start, count := probeRange(t, input, 0, "SECRET")
	_, keep, _ := probeRange(t, input, 0, "KEEP")
	dx := (layout.Glyphs[keep].Origin.X - layout.Glyphs[start].Origin.X) * layout.Width
	if out, err := CorrectText(input, 0, start, count, "SECRET", dx, 0); out != nil || !errors.Is(err, ErrTextOverlap) {
		t.Fatalf("tagged overlap accepted: %v", err)
	}
	if out, err := DuplicateText(input, 0, start, count, "SECRET", 0, 25); out != nil || err == nil {
		t.Fatal("tagged duplication accepted")
	}
	form := fixtureStream("BT /F1 16 Tf 30 140 Td (SECRET) Tj ET", "/Type /XObject /Subtype /Form /BBox [0 0 420 200] /Resources << /Font << /F1 4 0 R >> >>")
	input = taggedMoveFixture("/Form Do", "/XObject << /Form 12 0 R >>", form)
	_, start, count = probeRange(t, input, 0, "SECRET")
	if out, err := CorrectText(input, 0, start, count, "SECRET", 0, 25); out != nil || err == nil || !strings.Contains(err.Error(), "Source Forms") {
		t.Fatalf("tagged source Form accepted: %v", err)
	}
	for _, attribute := range []string{"A", "C", "Stm"} {
		input = taggedMoveFixture("/P << /MCID 0 >> BDC BT /F1 16 Tf 30 140 Td (SECRET) Tj ET EMC", "")
		// Rebuild the fixture to retain valid xref offsets after adding attributes.
		ctx, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(input), model.NewDefaultConfiguration(), nil)
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := ctx.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		root, err := ctx.DereferenceDict(catalog["StructTreeRoot"])
		if err != nil {
			t.Fatal(err)
		}
		kids, err := ctx.DereferenceArray(root["K"])
		if err != nil {
			t.Fatal(err)
		}
		element, err := ctx.DereferenceDict(kids[0])
		if err != nil {
			t.Fatal(err)
		}
		reason := "Structure layout attributes"
		switch attribute {
		case "A":
			element["A"] = types.Dict{"O": types.Name("Layout"), "BBox": types.Array{types.Integer(30), types.Integer(130), types.Integer(100), types.Integer(160)}}
		case "C":
			element["C"] = types.Name("Paragraph")
		case "Stm":
			page, ref, _, err := ctx.PageDict(context.Background(), 1, false)
			if err != nil {
				t.Fatal(err)
			}
			element["K"] = types.Dict{"Type": types.Name("MCR"), "Pg": *ref, "Stm": page["Contents"], "MCID": types.Integer(0)}
			reason = "Explicit structure stream references"
		}
		var b bytes.Buffer
		if err := api.WriteContext(context.Background(), ctx, &b); err != nil {
			t.Fatal(err)
		}
		_, start, count = probeRange(t, b.Bytes(), 0, "SECRET")
		if out, err := CorrectText(b.Bytes(), 0, start, count, "SECRET", 0, 25); out != nil || err == nil || !strings.Contains(err.Error(), reason) {
			t.Fatalf("stale structure layout attributes accepted: %v", err)
		}
	}
}

func TestTextCorrectionTaggedMoveNamedPropertiesAndContentsArray(t *testing.T) {
	input := taggedMoveFixture("/P /Paragraph BDC BT /F1 16 Tf 30 140 Td (SECRET) Tj ET EMC", "/Properties << /Paragraph << /MCID 0 >> >>")
	ctx, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(input), model.NewDefaultConfiguration(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for page := 1; page <= 2; page++ {
		dict, _, _, err := ctx.PageDict(context.Background(), page, false)
		if err != nil {
			t.Fatal(err)
		}
		dict["Contents"] = types.Array{dict["Contents"]}
	}
	var b bytes.Buffer
	if err := api.WriteContext(context.Background(), ctx, &b); err != nil {
		t.Fatal(err)
	}
	input = b.Bytes()
	out := probeCorrect(t, input, 0, "SECRET", "SECRET", 10, 25)
	if taggedStructure(t, input) != taggedStructure(t, out) {
		t.Fatal("named properties/array contents lost structure")
	}
	if !regexp.MustCompile(`/Paragraph\s+BDC`).MatchString(taggedPageContent(t, out, 0)) {
		t.Fatal("named marked-content property was not preserved")
	}
	if !reflect.DeepEqual(probeImage(t, input, 1), probeImage(t, out, 1)) {
		t.Fatal("array-valued shared contents changed on the other page")
	}
}
