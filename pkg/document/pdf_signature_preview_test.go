package document

import (
	"context"
	"emanuelelongo/proview/internal/pdfengine"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"testing"
)

func TestSignaturePreviewDoesNotInventSignBadge(t *testing.T) {
	for _, artwork := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty marker", true: "existing artwork"}[artwork], func(t *testing.T) {
			ctx, err := newAppearanceContext(100, 80)
			if err != nil {
				t.Fatal(err)
			}
			page, pageRef, _, err := ctx.PageDict(context.Background(), 1, false)
			if err != nil {
				t.Fatal(err)
			}
			content := "q Q"
			if artwork {
				content = "1 0 0 rg 0 0 60 30 re f"
			}
			ap, err := addPDFStream(ctx, []byte(content), types.Dict{"Type": types.Name("XObject"), "Subtype": types.Name("Form"), "BBox": pdfArray(0, 0, 60, 30)})
			if err != nil {
				t.Fatal(err)
			}
			widget := types.Dict{"Type": types.Name("Annot"), "Subtype": types.Name("Widget"), "FT": types.Name("Sig"), "F": types.Integer(4), "Ff": types.Integer(1), "T": types.StringLiteral("Signature"), "Rect": pdfArray(20, 20, 80, 50), "P": *pageRef, "AP": types.Dict{"N": *ap}}
			ref, err := ctx.IndRefForNewObject(widget)
			if err != nil {
				t.Fatal(err)
			}
			page["Annots"] = types.Array{*ref}
			root, err := ctx.Catalog()
			if err != nil {
				t.Fatal(err)
			}
			root["AcroForm"] = types.Dict{"Fields": types.Array{*ref}}
			raw, err := writePDFContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			img, err := pdfengine.RenderFormPage(raw, 0, 72)
			if err != nil {
				t.Fatal(err)
			}
			colored := 0
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					r, g, b, a := img.At(x, y).RGBA()
					if a > 0 && r > g+1000 && r > b+1000 {
						colored++
					}
				}
			}
			if !artwork && colored > 0 {
				t.Fatalf("empty signature gained colored badge (%d pixels)", colored)
			}
			if artwork && colored < 1000 {
				t.Fatal("real signature artwork disappeared")
			}
		})
	}
}
