package cloud

import _ "embed"

//go:embed LICENSES.txt
var dependencyLicenses string

func LicenseText() string { return dependencyLicenses }
