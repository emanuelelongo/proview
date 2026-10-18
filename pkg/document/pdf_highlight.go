package document

import (
	"context"
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Set the standard PDF Multiply graphics state inside the appearance, so readers
// using either the native Highlight dictionary or its artwork retain dark text.
func applyMultiply(ctx *model.Context, form *types.StreamDict) error {
	if err := form.Decode(); err != nil {
		return err
	}
	res, err := ctx.DereferenceDict(form.Dict["Resources"])
	if err != nil {
		return err
	}
	if res == nil {
		res = types.Dict{}
	}
	states, err := ctx.DereferenceDict(res["ExtGState"])
	if err != nil {
		return err
	}
	if states == nil {
		states = types.Dict{}
	}
	name := "HighlightMultiply"
	for i := 0; states[name] != nil; i++ {
		state, _ := ctx.DereferenceDict(states[name])
		if state["BM"] == types.Name("Multiply") && strings.HasPrefix(string(form.Content), "/"+name+" gs\n") {
			return nil
		}
		name = fmt.Sprintf("HighlightMultiply%d", i)
	}
	states[name] = types.Dict{"Type": types.Name("ExtGState"), "BM": types.Name("Multiply")}
	res["ExtGState"] = states
	form.Dict["Resources"] = res
	form.Content = append([]byte("/"+name+" gs\n"), form.Content...)
	return form.Encode()
}

// Flattened PDF/redaction exports use the same blend operation as annotations.
func multiplyArtworkPDF(raw []byte) ([]byte, error) {
	ctx, err := readPDFObjects(raw)
	if err != nil {
		return nil, err
	}
	p, _, attrs, err := ctx.PageDict(context.Background(), 1, false)
	if err != nil {
		return nil, err
	}
	content, err := ctx.PageContent(p, 1)
	if err != nil {
		return nil, err
	}
	form := types.StreamDict{Dict: types.Dict{"Resources": attrs.Resources}, Content: content}
	if err = applyMultiply(ctx, &form); err != nil {
		return nil, err
	}
	ref, err := addPDFStream(ctx, form.Content, nil)
	if err != nil {
		return nil, err
	}
	p["Contents"] = *ref
	p["Resources"] = form.Dict["Resources"]
	return writePDFContext(ctx)
}
