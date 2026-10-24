package gui

import (
	"context"
	"emanuelelongo/proview/internal/applog"
	"emanuelelongo/proview/pkg/annotations"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// Call only at operation boundaries on the UI thread, never from rendering or
// input-motion callbacks. Document text, image bytes and credentials are omitted.
func (g *Gui) logEvent(event, details string, args ...any) {
	context := fmt.Sprintf("window=%p document=%p", g, g.document)
	if g.document != nil {
		context += fmt.Sprintf(" format=%s pages=%d revision=%d dirty=%t redactions=%t", g.document.Format(), g.document.PageCount(), g.document.Revision(), g.document.Dirty(), g.document.HasRedactions())
	}
	log.Printf("event=%s %s %s", event, context, fmt.Sprintf(details, args...))
}

func (g *Gui) showOperationError(operation string, err error) {
	g.showErrorMessage(g.operationFailureMessage(operation, err))
}

func (g *Gui) showErrorMessage(message string) {
	if !g.closed {
		dialog.ShowError(errors.New(message), g.window)
	}
}

func (g *Gui) showAnnotationError(page int, data annotations.Data, err error) {
	op := "annotation_edit"
	if data.Kind == annotations.Drawing {
		op = "draw"
	} else if data.Kind == annotations.Text {
		op = "text_annotation"
	}
	g.logEvent("operation_failed", "level=ERROR operation=%q page=%d kind=%s style=%s points=%d quads=%d width=%g opacity=%g error=%q", op, page+1, data.Kind, data.StrokeStyle, len(data.Points), len(data.Quads), data.Width, data.Opacity, diagnosticError(err))
	g.showErrorMessage(operationErrorMessage(op, err))
}

func (g *Gui) operationFailureMessage(operation string, err error) string {
	g.logEvent("operation_failed", "level=ERROR operation=%q error=%q", operation, diagnosticError(err))
	return operationErrorMessage(operation, err)
}

// These messages are authored at the UI boundary (for example file-name
// validation), never supplied by an engine, operating system or remote service.
func (g *Gui) showInputError(err error) {
	if !g.closed {
		dialog.ShowError(err, g.window)
	}
}

func operationErrorMessage(operation string, err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return "Access was denied. Check the file or folder permissions and try again."
	case errors.Is(err, os.ErrNotExist):
		return "The file or folder is no longer available. Choose it again."
	case errors.Is(err, context.DeadlineExceeded):
		return "The operation took too long. Please try again."
	}
	op := strings.ToLower(operation)
	switch {
	case op == "text_annotation":
		reason := strings.ToLower(err.Error())
		if strings.Contains(reason, "does not contain character") {
			return "The selected font can't display some characters. Try another font or different text."
		}
		if strings.Contains(reason, "maximum 2000") {
			return "Use no more than 2000 characters in one text box."
		}
		return "Couldn't place this text. Try a different font or shorter text."
	case strings.Contains(op, "draw"):
		return "Couldn't complete the stroke. Please try drawing it again."
	case strings.Contains(op, "resize"):
		return "Couldn't resize this shape. Try a different size."
	case strings.Contains(op, "annotation"), strings.Contains(op, "object"), strings.Contains(op, "transform"):
		return "Couldn't update this object. Please try again."
	case strings.Contains(op, "redact"):
		return "Couldn't apply this redaction. Please try selecting the area again."
	case strings.Contains(op, "form"):
		return "Couldn't update the PDF form. Check the field value and try again."
	case strings.Contains(op, "clipboard_copy"):
		return "Couldn't copy the selection. Please try again."
	case strings.Contains(op, "clipboard"), strings.Contains(op, "paste"):
		return "Couldn't paste this content. Try copying it again."
	case strings.Contains(op, "preview"), strings.Contains(op, "history"):
		return "Couldn't display this page. Please try reopening the document."
	case strings.Contains(op, "signature_trace"):
		return "Couldn't prepare this signature. Try adjusting sensitivity or choosing another image."
	case strings.Contains(op, "cloud"), strings.Contains(op, "sync"):
		return "Couldn't sync signatures. Check your connection and cloud settings, then try again."
	case strings.Contains(op, "signature"):
		return "Couldn't complete the signature operation. Please try again."
	case op == "open_logs_folder":
		return "Couldn't open the logs folder. Please try again."
	case op == "open_url":
		return "Couldn't open this link. Copy it and open it in your browser."
	case op == "print":
		return "Couldn't print this document. Check your printer and the system printing service, then try again."
	case strings.Contains(op, "save"), strings.Contains(op, "export"):
		return "Couldn't save the document. Check the destination and available disk space, then try again."
	case strings.Contains(op, "open"), strings.Contains(op, "load"):
		return "Couldn't open this file. Check that it is readable and uses a supported format."
	case strings.Contains(op, "crop"):
		return "Couldn't crop this page. Please try selecting the area again."
	case strings.Contains(op, "page"), strings.Contains(op, "pdf_append"):
		return "Couldn't update the PDF pages. Please try again."
	default:
		return "Couldn't complete this operation. Please try again."
	}
}

var diagnosticQuotedSecrets = regexp.MustCompile(`(?i)("(?:access_token|refresh_token|client_secret|password|secret_access_key|secretkey|accesskey|authorization)"\s*:\s*")[^"]*(")`)
var diagnosticSecrets = regexp.MustCompile(`(?i)(\b(?:access_token|refresh_token|client_secret|password|secret_access_key|secretkey|accesskey|x-amz-credential|x-amz-signature|token|authorization)\b["']?\s*[:=]\s*["']?)([^"'&,\s}]+)`)
var diagnosticBearer = regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/-]+=*`)
var diagnosticURLs = regexp.MustCompile(`https?://[^\s"'<>]+`)

func diagnosticError(err error) string {
	if err == nil {
		return ""
	}
	message := diagnosticURLs.ReplaceAllStringFunc(err.Error(), func(value string) string {
		u, parseErr := url.Parse(value)
		if parseErr != nil {
			return "[invalid URL]"
		}
		u.User = nil
		query := u.RawQuery != ""
		u.RawQuery, u.Fragment = "", ""
		clean := u.String()
		if query {
			clean += "?[redacted]"
		}
		return clean
	})
	message = diagnosticQuotedSecrets.ReplaceAllString(message, "${1}[redacted]${2}")
	message = diagnosticBearer.ReplaceAllString(message, "${1}[redacted]")
	return diagnosticSecrets.ReplaceAllString(message, "${1}[redacted]")
}

func (g *Gui) openLogsFolder() {
	go func() {
		directory, err := applog.Directory()
		if err == nil {
			err = os.MkdirAll(directory, 0700)
		}
		if err == nil {
			err = applog.Flush()
		}
		fyne.Do(func() {
			if g.closed {
				return
			}
			if err != nil {
				g.showOperationError("open_logs_folder", err)
				return
			}
			path := filepath.ToSlash(directory)
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			if err := g.app.OpenURL(&url.URL{Scheme: "file", Path: path}); err != nil {
				g.showOperationError("open_logs_folder", err)
			}
		})
	}()
}

// Keep engine terminology and structural PDF details in the logs.
func textEditErrorMessage(err error) string {
	reason := strings.ToLower(err.Error())
	switch {
	case strings.Contains(reason, "overlap existing text"):
		return "This text would overlap other text. Move it to a free area or use shorter text."
	case strings.Contains(reason, "document changed"):
		return "The document changed. Select the text again."
	case strings.Contains(reason, "unchanged-text movement"), strings.Contains(reason, "duplication is not yet supported for tagged"):
		return "This PDF currently supports moving text, but not changing or duplicating it. Your document has not been changed."
	case strings.Contains(reason, "tagged text movement requires complete source text objects"):
		return "This word is part of a larger text block that Proview can't move independently yet. Your document has not been changed."
	case strings.Contains(reason, "font lacks replacement"), strings.Contains(reason, "missing glyph"), strings.Contains(reason, "cannot encode"), strings.Contains(reason, "not available in"):
		return "The current font can't display some of the new characters. Try different text."
	case strings.Contains(reason, "not supported"), strings.Contains(reason, "unsupported"), strings.Contains(reason, "only "):
		return "This page uses text formatting that Proview can't edit yet. Your document has not been changed."
	default:
		return "Couldn't edit this text. Your document has not been changed. Details are available in Help → Open logs folder."
	}
}

func (g *Gui) showTextEditError(page int, err error) {
	g.logEvent("operation_failed", "level=ERROR operation=pdf_text_edit page=%d error=%q", page+1, diagnosticError(err))
	g.showErrorMessage(textEditErrorMessage(err))
}
