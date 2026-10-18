package document

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"emanuelelongo/proview/pkg/annotations"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Embed the actual bundled face for other editors' FreeText regeneration. CID
// values use Unicode BMP code points; the standard CIDToGIDMap maps them to the
// font's glyph indices. This avoids depending on fonts installed on the reader.
func annotationFont(ctx *model.Context, d annotations.Data) (*types.IndirectRef, error) {
	data, e := annotations.FontData(d.Font, d.Bold, d.Italic)
	if e != nil {
		return nil, e
	}
	face, e := sfnt.Parse(data)
	if e != nil {
		return nil, e
	}
	var buf sfnt.Buffer
	name, e := face.Name(&buf, sfnt.NameIDPostScript)
	if e != nil {
		return nil, e
	}
	cff := bytes.HasPrefix(data, []byte("OTTO"))
	fileInfo := types.Dict{"Length1": types.Integer(len(data))}
	if cff {
		fileInfo["Subtype"] = types.Name("OpenType")
	}
	fontFile, e := addPDFStream(ctx, data, fileInfo)
	if e != nil {
		return nil, e
	}
	metrics, e := face.Metrics(&buf, fixed.I(1000), font.HintingNone)
	if e != nil {
		return nil, e
	}
	bounds, e := face.Bounds(&buf, fixed.I(1000), font.HintingNone)
	if e != nil {
		return nil, e
	}
	flags := 32
	if d.Italic {
		flags |= 64
	}
	if d.Font == "Go Mono" {
		flags |= 1
	}
	if d.Font == "Latin Modern Roman" {
		flags |= 2
	}
	angle := 0.
	if d.Italic {
		angle = -12
	}
	descriptorDict := types.Dict{"Type": types.Name("FontDescriptor"), "FontName": types.Name(name), "FontFamily": pdfText(d.Font), "Flags": types.Integer(flags), "FontBBox": pdfArray(float64(bounds.Min.X)/64, -float64(bounds.Max.Y)/64, float64(bounds.Max.X)/64, -float64(bounds.Min.Y)/64), "Ascent": types.Float(float64(metrics.Ascent) / 64), "Descent": types.Float(-float64(metrics.Descent) / 64), "CapHeight": types.Float(float64(metrics.CapHeight) / 64), "ItalicAngle": types.Float(angle), "StemV": types.Integer(80), "FontFile2": *fontFile}
	if cff {
		delete(descriptorDict, "FontFile2")
		descriptorDict["FontFile3"] = *fontFile
	}
	descriptor, e := ctx.IndRefForNewObject(descriptorDict)
	if e != nil {
		return nil, e
	}
	glyphMap := make([]byte, 65536*2)
	var widths types.Array
	var cidMappings []string
	seenGlyphs := map[uint16]bool{}
	for cid := 0; cid < 65536; cid++ {
		if cid >= 0xd800 && cid <= 0xdfff {
			continue
		}
		glyph, e := face.GlyphIndex(&buf, rune(cid))
		if e != nil {
			return nil, e
		}
		if glyph == 0 {
			continue
		}
		binary.BigEndian.PutUint16(glyphMap[cid*2:], uint16(glyph))
		advance, e := face.GlyphAdvance(&buf, glyph, fixed.I(1000), font.HintingNone)
		if e != nil {
			return nil, e
		}
		if cff {
			cidMappings = append(cidMappings, fmt.Sprintf("<%04X> %d", cid, glyph))
			if !seenGlyphs[uint16(glyph)] {
				widths = append(widths, types.Integer(glyph), pdfArray(float64(advance)/64))
				seenGlyphs[uint16(glyph)] = true
			}
		} else {
			widths = append(widths, types.Integer(cid), pdfArray(float64(advance)/64))
		}
	}
	mapping, e := addPDFStream(ctx, glyphMap, nil)
	if e != nil {
		return nil, e
	}
	cidDict := types.Dict{"Type": types.Name("Font"), "Subtype": types.Name("CIDFontType2"), "BaseFont": types.Name(name), "CIDSystemInfo": types.Dict{"Registry": types.StringLiteral("Adobe"), "Ordering": types.StringLiteral("Identity"), "Supplement": types.Integer(0)}, "FontDescriptor": *descriptor, "CIDToGIDMap": *mapping, "DW": types.Integer(1000), "W": widths}
	if cff {
		cidDict["Subtype"] = types.Name("CIDFontType0")
		delete(cidDict, "CIDToGIDMap")
	}
	cid, e := ctx.IndRefForNewObject(cidDict)
	if e != nil {
		return nil, e
	}
	cmap := []byte("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /UnicodeBMP def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n2 beginbfrange\n<0000> <D7FF> <0000>\n<E000> <FFFF> <E000>\nendbfrange\nendcmap\nCMapName currentdict /CMap defineresource pop\nend\nend")
	unicode, e := addPDFStream(ctx, cmap, nil)
	if e != nil {
		return nil, e
	}
	var encoding types.Object = types.Name("Identity-H")
	if cff {
		var body strings.Builder
		body.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> def\n/CMapName /UnicodeToGlyph def\n/CMapType 1 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
		for start := 0; start < len(cidMappings); start += 100 {
			end := start + 100
			if end > len(cidMappings) {
				end = len(cidMappings)
			}
			fmt.Fprintf(&body, "%d begincidchar\n%s\nendcidchar\n", end-start, strings.Join(cidMappings[start:end], "\n"))
		}
		body.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend")
		ref, e := addPDFStream(ctx, []byte(body.String()), types.Dict{"Type": types.Name("CMap"), "CMapName": types.Name("UnicodeToGlyph"), "CIDSystemInfo": types.Dict{"Registry": types.StringLiteral("Adobe"), "Ordering": types.StringLiteral("Identity"), "Supplement": types.Integer(0)}})
		if e != nil {
			return nil, e
		}
		encoding = *ref
	}
	return ctx.IndRefForNewObject(types.Dict{"Type": types.Name("Font"), "Subtype": types.Name("Type0"), "BaseFont": types.Name(name), "Encoding": encoding, "DescendantFonts": types.Array{*cid}, "ToUnicode": *unicode})
}
func fontKey(d annotations.Data) string { return fmt.Sprintf("%s/%t/%t", d.Font, d.Bold, d.Italic) }

func existingAnnotationFont(ctx *model.Context, resources types.Dict, d annotations.Data) (string, *types.IndirectRef) {
	for name, obj := range resources {
		ref, ok := obj.(types.IndirectRef)
		if !ok {
			continue
		}
		font, _ := ctx.DereferenceDict(obj)
		if font == nil {
			continue
		}
		if children, _ := ctx.DereferenceArray(font["DescendantFonts"]); len(children) > 0 {
			font, _ = ctx.DereferenceDict(children[0])
		}
		if font == nil {
			continue
		}
		descriptor, _ := ctx.DereferenceDict(font["FontDescriptor"])
		if descriptor == nil {
			continue
		}
		flags, _ := number(descriptor["Flags"])
		if annotationString(ctx, descriptor, "FontFamily") == d.Font && strings.Contains(strings.ToLower(annotationName(descriptor, "FontName")), "bold") == d.Bold && (int(flags)&64 != 0) == d.Italic {
			return name, &ref
		}
	}
	return "", nil
}
