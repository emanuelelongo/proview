//go:build cgo && !nocgo && !extlib

package pdfengine

import (
	"bytes"
	"image"
	"strings"
	"testing"
)

func textEffectsFixture(content, form, group string) []byte {
	effects := `/ExtGState <<
 /Trans << /Type /ExtGState /ca .3 /CA .3 >>
 /Blend << /Type /ExtGState /BM /Multiply >>
 /Soft << /Type /ExtGState /SMask << /S /Luminosity /G 8 0 R >> >>
 /Opaque << /Type /ExtGState /ca 1 /CA 1 /BM /Normal /SMask /None >>
 /Stroke << /Type /ExtGState /CA .3 >> >>`
	resources := `/Resources << /Font << /F1 4 0 R >> /XObject << /Form 6 0 R /Nested 7 0 R >> ` + effects + ` >>`
	return fixturePDF([]string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 420 200] ` + resources + ` /Contents 5 0 R >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		fixtureStream(content, ""),
		fixtureStream(form, `/Type /XObject /Subtype /Form /BBox [0 0 420 200] `+resources+" "+group),
		fixtureStream(`BT /F1 16 Tf 30 140 Td (SECRET KEEP) Tj ET`, `/Type /XObject /Subtype /Form /BBox [0 0 420 200] `+resources),
		fixtureStream(`1 g 0 0 420 200 re f`, `/Type /XObject /Subtype /Form /BBox [0 0 420 200] /Group << /S /Transparency /CS /DeviceGray >> /Resources << >>`),
	}, "")
}

func TestTextEditingPreservesUnrelatedTransparency(t *testing.T) {
	const text = `BT /F1 16 Tf 30 140 Td (SECRET KEEP) Tj ET`
	const art = `q /Trans gs /Blend gs 1 0 0 rg 25 125 150 35 re f Q`
	for _, tc := range []struct{ name, content, form string }{
		{"art_before", art + " " + text, ""},
		{"art_after", text + " " + art, ""},
		{"transparent_text_elsewhere", text + ` q /Trans gs BT /F1 16 Tf 30 50 Td (PUBLIC) Tj ET Q`, ""},
		{"shared_art_form", `/Form Do ` + text + ` /Form Do`, art},
		{"inherited_state_reset_inside_form", `/Trans gs /Form Do`, `/Opaque gs ` + text},
		{"soft_mask_elsewhere", `q /Soft gs 1 0 0 rg 20 20 100 30 re f Q ` + text, ""},
		{"local_form_state", `/Form Do`, art + " " + text},
		{"nested_form_state", `/Form Do`, art + ` /Nested Do`},
		{"restored_opacity", `/Trans gs q /Opaque gs ` + text + ` Q`, ""},
		{"restored_mask", `/Soft gs q /Opaque gs ` + text + ` Q`, ""},
		{"restored_blend", `/Blend gs q /Opaque gs ` + text + ` Q`, ""},
		{"stroke_alpha_only", `/Stroke gs ` + text, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := textEffectsFixture(tc.content, tc.form, "")
			identical := probeCorrect(t, input, 0, "SECRET", "SECRET", 0, 0)
			checkTextEffectsPixels(t, probeImage(t, input, 0), probeImage(t, identical, 0), image.Rectangle{})

			output := probeCorrect(t, input, 0, "SECRET", "EDIT", 0, 0)
			probeSameWordPosition(t, input, output, 0, "KEEP")
			before, after := probeImage(t, input, 0), probeImage(t, output, 0)
			// Only the selected word's ink can change (144 DPI, top-left origin).
			checkTextEffectsPixels(t, before, after, image.Rect(58, 85, 195, 125))

			// The replacement Form remains searchable and can be edited again.
			probeCorrect(t, output, 0, "EDIT", "NEXT", 0, 0)
		})
	}
}

func TestTextEditingRejectsEffectsOnSelectedText(t *testing.T) {
	const text = `BT /F1 16 Tf 30 140 Td (SECRET KEEP) Tj ET`
	for _, tc := range []struct{ name, content, form, group string }{
		{"alpha", `/Trans gs ` + text, "", ""},
		{"blend", `/Blend gs ` + text, "", ""},
		{"mask", `/Soft gs ` + text, "", ""},
		{"partial_state", `/Trans gs /Stroke gs ` + text, "", ""},
		{"q_Q_restoration", `/Trans gs q /Opaque gs Q ` + text, "", ""},
		{"local_form", `/Form Do`, `/Trans gs ` + text, ""},
		{"inherited_form", `/Trans gs /Form Do`, text, ""},
		{"inherited_nested_form", `/Form Do`, `/Trans gs /Nested Do`, ""},
		{"transparency_group", `/Form Do`, text, `/Group << /S /Transparency /CS /DeviceRGB >>`},
		{"later_effect_in_same_text_object", `BT /F1 16 Tf 30 140 Td (SECRET ) Tj /Trans gs (KEEP) Tj ET`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := textEffectsFixture(tc.content, tc.form, tc.group)
			original := bytes.Clone(input)
			_, start, count := probeRange(t, input, 0, "SECRET")
			output, err := CorrectText(input, 0, start, count, "EDIT", 0, 0)
			if err == nil || output != nil || !strings.Contains(err.Error(), "Transparency") {
				t.Fatalf("expected transparency rejection without output, got %v", err)
			}
			if !bytes.Equal(input, original) {
				t.Fatal("rejected edit changed input")
			}
		})
	}
}

func checkTextEffectsPixels(t *testing.T, before, after image.Image, editZone image.Rectangle) {
	t.Helper()
	if before.Bounds() != after.Bounds() {
		t.Fatal("page bounds changed")
	}
	for y := before.Bounds().Min.Y; y < before.Bounds().Max.Y; y++ {
		for x := before.Bounds().Min.X; x < before.Bounds().Max.X; x++ {
			if image.Pt(x, y).In(editZone) {
				continue
			}
			ar, ag, ab, aa := before.At(x, y).RGBA()
			br, bg, bb, ba := after.At(x, y).RGBA()
			for _, pair := range [][2]uint32{{ar, br}, {ag, bg}, {ab, bb}, {aa, ba}} {
				a, b := pair[0], pair[1]
				if b > a {
					a, b = b, a
				}
				// Transparency compositing can round antialiasing by one 8-bit level
				// when resource cleanup changes an intermediate rendering group.
				if a-b > 257 {
					t.Fatalf("unrelated pixel changed at %d,%d: %v vs %v", x, y, before.At(x, y), after.At(x, y))
				}
			}
		}
	}
}
