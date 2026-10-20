package ocr

import (
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"

	"emanuelelongo/proview/internal/textselect"
)

// ParseHOCR reads Tesseract's XHTML without resolving external entities. Invalid
// character geometry falls back to the word; invalid word geometry rejects it.
func ParseHOCR(r io.Reader, width, height int) (textselect.Layout, error) {
	layout := textselect.Layout{Width: float64(width), Height: float64(height)}
	if width <= 0 || height <= 0 {
		return layout, errors.New("invalid OCR image dimensions")
	}
	decoder := xml.NewDecoder(r)
	block, line, wordDepth, depth := 0, 0, 0, 0
	page := false
	var word textselect.Word
	var raw, text strings.Builder
	validCharacters := true
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return layout, err
		}
		switch node := token.(type) {
		case xml.StartElement:
			depth++
			class, title := "", ""
			for _, a := range node.Attr {
				if a.Name.Local == "class" {
					class = a.Value
				}
				if a.Name.Local == "title" {
					title = a.Value
				}
			}
			switch class {
			case "ocr_page":
				page = true
			case "ocr_carea":
				block++
			case "ocr_line", "ocr_header", "ocr_textfloat", "ocr_caption":
				line++
			case "ocrx_word":
				q, ok := hocrBox(title, "bbox", width, height)
				if !ok || wordDepth != 0 {
					return layout, errors.New("invalid hOCR word bounds")
				}
				word = textselect.Word{Quad: q, Block: block, Line: line}
				for _, property := range strings.Split(title, ";") {
					fields := strings.Fields(property)
					if len(fields) == 2 && fields[0] == "x_wconf" {
						word.Confidence, err = strconv.ParseFloat(fields[1], 64)
						if err != nil {
							return layout, err
						}
					}
				}
				wordDepth = depth
				raw.Reset()
				text.Reset()
				validCharacters = true
			case "ocrx_cinfo":
				if wordDepth == 0 {
					break
				}
				var symbol string
				if err := decoder.DecodeElement(&symbol, &node); err != nil {
					return layout, err
				}
				depth--
				start := text.Len()
				text.WriteString(symbol)
				q, ok := hocrBox(title, "x_bboxes", width, height)
				if !ok || symbol == "" {
					validCharacters = false
					break
				}
				word.Characters = append(word.Characters, textselect.Character{Start: start, End: text.Len(), Quad: q})
			}
		case xml.CharData:
			if wordDepth > 0 {
				raw.Write(node)
			}
		case xml.EndElement:
			if wordDepth == depth {
				word.Text = text.String()
				if word.Text == "" {
					word.Text = strings.TrimSpace(raw.String())
					validCharacters = false
				}
				if !validCharacters {
					word.Characters = nil
				}
				if word.Text != "" {
					layout.Words = append(layout.Words, word)
				}
				wordDepth = 0
			}
			depth--
		}
	}
	if !page || wordDepth != 0 {
		return layout, errors.New("invalid hOCR document")
	}
	return layout, nil
}

func hocrBox(title, key string, width, height int) (textselect.Quad, bool) {
	for _, property := range strings.Split(title, ";") {
		fields := strings.Fields(property)
		if len(fields) != 5 || fields[0] != key {
			continue
		}
		var box [4]int
		for i := range box {
			v, err := strconv.Atoi(fields[i+1])
			if err != nil {
				return textselect.Quad{}, false
			}
			box[i] = v
		}
		if box[0] < 0 || box[1] < 0 || box[2] <= box[0] || box[3] <= box[1] || box[2] > width || box[3] > height {
			return textselect.Quad{}, false
		}
		return textselect.Rect(float64(box[0])/float64(width), float64(box[1])/float64(height), float64(box[2])/float64(width), float64(box[3])/float64(height)), true
	}
	return textselect.Quad{}, false
}
