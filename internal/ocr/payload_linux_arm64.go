//go:build proview_ocr_bundle && linux && arm64

package ocr

import _ "embed"

//go:embed payloads/linux-arm64.zip
var runtimePayload []byte
