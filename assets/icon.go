package assets

import _ "embed"

// AppIcon is embedded so packaged applications never depend on the working directory.
//
//go:embed app-icon.png
var AppIcon []byte
