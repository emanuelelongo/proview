package pdfengine

import _ "embed"

//go:embed COPYING
var licenseText string

// LicenseText returns the license notice distributed with the native PDF engine.
func LicenseText() string { return licenseText }
