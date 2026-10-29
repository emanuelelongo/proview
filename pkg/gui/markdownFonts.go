package gui

import (
	"sync"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"golang.org/x/image/font/sfnt"
)

var markdownFonts sync.Map // fyne.Resource -> *sfnt.Font; font bytes remain shared

// Explicit, shared font resources avoid Fyne's separate parsed font copies for
// each theme scope. Keep native font fallback for runs the chosen font cannot
// cover (including emoji and scripts missing from the application font).
func markdownFontCovers(resource fyne.Resource, text string) bool {
	if resource == nil {
		return false
	}
	cached, ok := markdownFonts.Load(resource)
	if !ok {
		parsed, err := sfnt.Parse(resource.Content())
		if err != nil {
			return false
		}
		cached, _ = markdownFonts.LoadOrStore(resource, parsed)
	}
	font := cached.(*sfnt.Font)
	var buffer sfnt.Buffer
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		glyph, err := font.GlyphIndex(&buffer, r)
		if err != nil || glyph == 0 {
			return false
		}
	}
	return true
}

func (r *markdownSelectionRenderer) shareFonts() {
	reading := r.block.view.readingTheme()
	for _, object := range r.WidgetRenderer.Objects() {
		if text, ok := object.(*canvas.Text); ok {
			resource := reading.Font(text.TextStyle)
			text.FontSource = nil
			if markdownFontCovers(resource, text.Text) {
				text.FontSource = resource
			}
		}
	}
}
