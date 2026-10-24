package gui

import (
	"emanuelelongo/proview/internal/markdown"
	"emanuelelongo/proview/internal/pdfengine"
	"emanuelelongo/proview/internal/qrscan"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/signatures/cloud"
	"fmt"
)

func thirdPartyLicensesText() string {
	tabler, err := tablerAssets.ReadFile("icons/tabler/LICENSE")
	if err != nil {
		tabler = []byte("Tabler Icons license text is unavailable.")
	}
	return fmt.Sprintf(`THIRD-PARTY COMPONENTS

Fyne — BSD 3-Clause — fyne.io/fyne/v2
Goldmark / Chroma / regexp2 — MIT — Native Markdown and syntax highlighting
Sketch / Pele — MIT; bundled fonts — OFL / BSD — Native Mermaid rendering
Go clipboard — MIT — golang.design/x/clipboard
Zenity — MIT — github.com/ncruces/zenity
go-fitz / MuPDF — GNU AGPL v3 — github.com/gen2brain/go-fitz
gozxing — MIT / Apache License 2.0 — github.com/makiuchi-d/gozxing
pdfcpu — Apache License 2.0 — github.com/pdfcpu/pdfcpu
Canvas — MIT — github.com/tdewolff/canvas
Go image and text packages — BSD 3-Clause — golang.org/x/image, golang.org/x/text
Tabler Icons — MIT — tabler.io/icons
go-keyring / wincred / shellescape — MIT — github.com/zalando/go-keyring
Go OAuth2 — BSD 3-Clause — golang.org/x/oauth2

The project source and dependency modules contain the corresponding notices and
license texts. The complete notices bundled directly with Proview follow.

FONT LICENSES

%s

TABLER ICONS

%s

GO-FITZ / MUPDF

%s

CLOUD SYNC DEPENDENCIES

%s

GOZXING

%s

MARKDOWN

%s`, annotations.FontLicenses(), string(tabler), pdfengine.LicenseText(), cloud.LicenseText(), qrscan.LicenseText(), markdown.Licenses())
}
