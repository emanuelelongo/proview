//go:build proview_ocr_bundle && windows && amd64

package ocr

import _ "embed"

//go:embed payloads/windows-amd64.zip
var runtimePayload []byte
