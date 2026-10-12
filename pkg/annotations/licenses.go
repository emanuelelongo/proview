package annotations

import _ "embed"

//go:embed licenses/Go-fonts-BSD.txt
var goFontsLicense string

//go:embed licenses/Latin-Modern-Go-BSD.txt
var latinModernGoLicense string

//go:embed licenses/Latin-Modern-GUST.txt
var latinModernLicense string

func FontLicenses() string {
	return "Go Sans / Go Mono\n\n" + goFontsLicense + "\n\nLatin Modern — Go font resources\n\n" + latinModernGoLicense + "\n\nLatin Modern — GUST Font License\n\n" + latinModernLicense
}
