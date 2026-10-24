package markdown

import (
	"embed"
	"strings"
)

//go:embed licenses/*.txt
var licenseFiles embed.FS

func Licenses() string {
	var b strings.Builder
	for _, name := range []string{"Goldmark-MIT.txt", "Chroma-MIT.txt", "Regexp2-MIT.txt", "Sketch-Pele-MIT.txt", "Sketch-Fonts.txt", "Fpdf-MIT.txt"} {
		data, err := licenseFiles.ReadFile("licenses/" + name)
		if err != nil {
			continue
		}
		b.WriteString(name)
		b.WriteString("\n\n")
		b.Write(data)
		b.WriteString("\n\n")
	}
	return b.String()
}
