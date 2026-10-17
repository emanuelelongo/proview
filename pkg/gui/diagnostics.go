package gui

import (
	"emanuelelongo/proview/internal/applog"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
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
	g.logEvent("operation_failed", "level=ERROR operation=%q error=%q", operation, err)
	dialog.ShowError(err, g.window)
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
	case strings.Contains(reason, "font lacks replacement"), strings.Contains(reason, "missing glyph"), strings.Contains(reason, "cannot encode"), strings.Contains(reason, "not available in"):
		return "The current font can't display some of the new characters. Try different text."
	case strings.Contains(reason, "not supported"), strings.Contains(reason, "unsupported"), strings.Contains(reason, "only "):
		return "This page uses text formatting that Proview can't edit yet. Your document has not been changed."
	default:
		return "Couldn't edit this text. Your document has not been changed. Details are available in Help → Open logs folder."
	}
}

func (g *Gui) showTextEditError(page int, err error) {
	g.logEvent("operation_failed", "level=ERROR operation=pdf_text_edit page=%d error=%q", page+1, err)
	dialog.ShowError(errors.New(textEditErrorMessage(err)), g.window)
}
