//go:build proview_ocr_bundle && darwin && amd64

package ocr

import _ "embed"

//go:embed payloads/darwin-amd64.zip
var runtimePayload []byte
