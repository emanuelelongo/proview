package qrscan

import _ "embed"

//go:embed LICENSE
var licenseText string

func LicenseText() string { return licenseText }
