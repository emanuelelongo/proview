//go:build proview_ocr_bundle && darwin && arm64

package ocr

import _ "embed"

//go:embed payloads/darwin-arm64.zip
var runtimePayload []byte
