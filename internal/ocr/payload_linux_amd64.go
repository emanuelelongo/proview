//go:build proview_ocr_bundle && linux && amd64

package ocr

import _ "embed"

//go:embed payloads/linux-amd64.zip
var runtimePayload []byte
