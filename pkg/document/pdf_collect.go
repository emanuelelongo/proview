package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// pdfcpu does not yet accept the C2PA file relationship. Page collection builds
// a new catalog, so document-level Content Credentials are not copied anyway.
// Accept that one extension during source validation, keeping all other checks.
func collectPDFPages(c context.Context, original []byte, output *bytes.Buffer, selection []string) error {
	err := api.Collect(c, bytes.NewReader(original), output, selection, nil)
	if err == nil || !isC2PARelationshipError(err) {
		return err
	}
	conf := model.NewDefaultConfiguration()
	conf.Cmd = model.COLLECT
	ctx, err := api.ReadContext(c, bytes.NewReader(original), conf)
	if err != nil {
		return err
	}
	var relationships []types.Dict
	defer func() {
		for _, dict := range relationships {
			dict["AFRelationship"] = types.Name("C2PA_Manifest")
		}
	}()
	for {
		err = api.ValidateContext(c, ctx)
		if err == nil {
			break
		}
		var validationErr *model.ValidationError
		if !isC2PARelationshipError(err) || !errors.As(err, &validationErr) {
			return err
		}
		entry := ctx.Table[validationErr.ObjectNumber()]
		if entry == nil || entry.Free {
			return err
		}
		dict, ok := entry.Object.(types.Dict)
		if !ok || dict.NameEntry("AFRelationship") == nil || *dict.NameEntry("AFRelationship") != "C2PA_Manifest" {
			return err
		}
		relationships = append(relationships, dict)
		delete(dict, "AFRelationship")
	}
	for _, dict := range relationships {
		dict["AFRelationship"] = types.Name("C2PA_Manifest")
	}
	if err := api.OptimizeContext(c, ctx); err != nil {
		return err
	}
	pages, err := api.PagesForPageCollection(ctx.PageCount, selection)
	if err != nil {
		return err
	}
	selected, err := pdfcpu.ExtractPages(c, ctx, pages, false)
	if err != nil {
		return err
	}
	var result bytes.Buffer
	if err := api.Write(c, selected, &result, conf); err != nil {
		return err
	}
	if err := api.Validate(c, bytes.NewReader(result.Bytes()), nil, nil); err != nil {
		return fmt.Errorf("document: validate collected PDF pages: %w", err)
	}
	output.Reset()
	_, err = output.Write(result.Bytes())
	return err
}

func isC2PARelationshipError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "entry=AFRelationship invalid dict entry: C2PA_Manifest")
}
