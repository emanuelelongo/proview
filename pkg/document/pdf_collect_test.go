package document

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func contentCredentialsPDF(t *testing.T, relationship string) []byte {
	t.Helper()
	c := context.Background()
	ctx, err := api.ReadContext(c, bytes.NewReader(textPDF(t, "Preserved")), nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := ctx.NewEmbeddedStreamDict(strings.NewReader("synthetic manifest"), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	file, err := ctx.IndRefForNewObject(types.Dict{
		"Type":           types.Name("Filespec"),
		"F":              types.StringLiteral("manifest.c2pa"),
		"UF":             types.StringLiteral("manifest.c2pa"),
		"AFRelationship": types.Name(relationship),
		"EF":             types.Dict{"F": *stream, "UF": *stream},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ctx.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	catalog["AF"] = types.Array{*file}
	catalog["Names"] = types.Dict{"EmbeddedFiles": types.Dict{"Names": types.Array{types.StringLiteral("Content Credentials"), *file}}}
	var output bytes.Buffer
	if err := api.WriteContext(c, ctx, &output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestBlankPDFPageWithContentCredentials(t *testing.T) {
	original := contentCredentialsPDF(t, "C2PA_Manifest")
	if err := api.Validate(context.Background(), bytes.NewReader(original), nil, nil); !isC2PARelationshipError(err) {
		t.Fatalf("fixture did not reproduce the unsupported C2PA relationship: %v", err)
	}
	source, err := Load("credentials.pdf", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(source)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	defer doc.Close()
	ids := doc.PageIDs()
	blank, err := doc.RenderingSnapshot().BlankPageAfter(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.InsertBlankPage(0, blank); err != nil {
		blank.Close()
		t.Fatal(err)
	}
	output, err := doc.Export(ExportOptions{Format: FormatPDF})
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Validate(context.Background(), bytes.NewReader(output), nil, nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := Load("result.pdf", bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.PageCount() != 2 {
		t.Fatal("blank page missing from export")
	}
	var content strings.Builder
	if err := api.ExtractContent(context.Background(), bytes.NewReader(output), []string{"1"}, func(r io.Reader, _ int) error {
		_, err := io.Copy(&content, r)
		return err
	}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content.String(), "Preserved") || !strings.Contains(content.String(), "0 0 m 72 72 l S") {
		t.Fatal("original text/vector content was lost")
	}
	if !bytes.Equal(source.Original(), original) {
		t.Fatal("source bytes changed")
	}
	if !doc.Undo() || !slices.Equal(doc.PageIDs(), ids) || doc.Dirty() {
		t.Fatal("Undo did not restore the original document")
	}
	if !doc.Redo() || doc.PageCount() != 2 {
		t.Fatal("Redo did not restore the blank page")
	}
}

func TestCollectPDFPagesRejectsOtherInvalidRelationships(t *testing.T) {
	var output bytes.Buffer
	if err := collectPDFPages(context.Background(), contentCredentialsPDF(t, "InvalidRelationship"), &output, []string{"1"}); err == nil {
		t.Fatal("unrelated invalid relationship was accepted")
	}
}

func TestCollectPDFPagesStillValidatesAfterC2PAError(t *testing.T) {
	c := context.Background()
	ctx, err := api.ReadContext(c, bytes.NewReader(contentCredentialsPDF(t, "C2PA_Manifest")), nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ctx.IndRefForNewObject(types.Dict{
		"Type": types.Name("Filespec"), "F": types.StringLiteral("other.txt"),
		"AFRelationship": types.Name("InvalidRelationship"),
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ctx.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	names, err := ctx.DereferenceDict(catalog["Names"])
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := ctx.DereferenceDict(names["EmbeddedFiles"])
	if err != nil {
		t.Fatal(err)
	}
	embedded["Names"] = append(embedded["Names"].(types.Array), types.StringLiteral("Other"), *other)
	var original, output bytes.Buffer
	if err := api.WriteContext(c, ctx, &original); err != nil {
		t.Fatal(err)
	}
	if err := api.Collect(c, bytes.NewReader(original.Bytes()), &output, []string{"1"}, nil); !isC2PARelationshipError(err) {
		t.Fatalf("expected C2PA to fail first: %v", err)
	}
	if err := collectPDFPages(c, original.Bytes(), &output, []string{"1"}); err == nil || !strings.Contains(err.Error(), "InvalidRelationship") {
		t.Fatalf("compatibility path skipped another validation failure: %v", err)
	}
}
