package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/document"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

type preparedExports struct {
	data     map[document.Format][]byte
	revision uint64
}

const lastSaveDirectoryKey = "lastSaveDirectory"

func (g *Gui) save() { g.saveWithCompletion(nil) }

func (g *Gui) saveWithCompletion(saved func()) {
	if g.waitAnnotationChanges(func() { g.saveWithCompletion(saved) }) {
		return
	}
	if g.document == nil || g.working {
		return
	}
	if g.saveTarget == nil {
		g.saveAsWithCompletion(saved)
		return
	}
	format, err := document.FormatFromName(g.saveTarget.Name())
	if err != nil || !document.CanExport(format) {
		g.saveAsWithCompletion(saved)
		return
	}
	g.confirmRedactionExport(g.saveTarget, func() { g.saveConfirmed(saved) })
}

func (g *Gui) saveConfirmed(saved func()) {
	target := g.saveTarget
	g.logEvent("save_started", "mode=save")
	revision := g.document.Revision()
	g.working = true
	progress := dialog.NewCustomWithoutButtons("Saving", widget.NewProgressBarInfinite(), g.window)
	progress.Show()
	go func() {
		data, exportErr := g.exportForName(target.Name())
		fyne.Do(func() {
			progress.Hide()
			g.working = false
			defer g.processPendingFiles()
			if exportErr != nil {
				g.showOperationError("save", exportErr)
				return
			}
			if target.Scheme() == "file" {
				g.finishFileSave(data, target, revision, saved)
				return
			}
			writer, err := storage.Writer(target)
			if err != nil {
				g.showOperationError("save", err)
				return
			}
			g.writeExport(writer, data, target, revision, saved)
		})
	}()
}

func (g *Gui) saveAs() { g.saveAsWithCompletion(nil) }

func (g *Gui) saveAsWithCompletion(saved func()) {
	if g.waitAnnotationChanges(func() { g.saveAsWithCompletion(saved) }) {
		return
	}
	if g.document == nil || g.working {
		return
	}
	g.confirmRedactionExport(nil, func() { g.saveAsConfirmed(saved) })
}

func (g *Gui) saveAsConfirmed(saved func()) {
	g.logEvent("save_started", "mode=save_as")
	g.working = true
	progress := dialog.NewCustomWithoutButtons("Preparing export", widget.NewProgressBarInfinite(), g.window)
	progress.Show()
	go func() {
		exports, err := g.prepareExports()
		fyne.Do(func() {
			progress.Hide()
			g.working = false
			defer g.processPendingFiles()
			if err != nil {
				g.showOperationError("save", err)
				return
			}
			g.showSaveAsDialog(exports, saved)
		})
	}()
}

func (g *Gui) showSaveAsDialog(exports preparedExports, saved func()) {
	g.showLocalSaveAsDialog(exports, saved)
}

func (g *Gui) prepareExports() (preparedExports, error) {
	revision := g.document.Revision()
	formats := []document.Format{document.FormatPNG, document.FormatJPEG}
	if g.document.Format() == document.FormatPDF {
		formats = []document.Format{document.FormatPDF}
	}
	result := make(map[document.Format][]byte, len(formats))
	for _, format := range formats {
		data, err := g.document.Export(document.ExportOptions{Format: format})
		if err != nil {
			return preparedExports{}, err
		}
		result[format] = data
	}
	return preparedExports{data: result, revision: revision}, nil
}

func (g *Gui) exportForName(name string) ([]byte, error) {
	format, err := document.FormatFromName(name)
	if err != nil {
		return nil, fmt.Errorf("unsupported export format: %w", err)
	}
	if g.document.Format() == document.FormatPDF && format != document.FormatPDF {
		return nil, errors.New("a PDF document must be saved with the .pdf extension")
	}
	if g.document.Format() != document.FormatPDF && format == document.FormatPDF {
		return nil, errors.New("an image must be saved as .png, .jpg, or .jpeg")
	}
	return g.document.Export(document.ExportOptions{Format: format})
}

func (g *Gui) writeExport(writer fyne.URIWriteCloser, data []byte, uri fyne.URI, revision uint64, saved func()) {
	_, writeErr := io.Copy(writer, bytes.NewReader(data))
	closeErr := writer.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		g.showOperationError("save_write", fmt.Errorf("could not save %q: %w", uri.Name(), err))
		return
	}
	g.fileSizeBytes = int64(len(data))
	g.finishSave(uri, revision, saved)
}

func (g *Gui) finishFileSave(data []byte, uri fyne.URI, revision uint64, saved func()) {
	if g.document == nil || g.document.Revision() != revision {
		g.showInputError(errors.New("the document changed; save again to export the latest changes"))
		return
	}
	if err := document.WriteFileAtomic(uri.Path(), data); err != nil {
		g.showOperationError("save_write", fmt.Errorf("could not save %q: %w", uri.Name(), err))
		return
	}
	g.fileSizeBytes = int64(len(data))
	g.finishSave(uri, revision, saved)
}

func (g *Gui) finishSave(uri fyne.URI, revision uint64, saved func()) {
	g.logEvent("save_completed", "bytes=%d exported_revision=%d scheme=%q", g.fileSizeBytes, revision, uri.Scheme())
	g.saveTarget = uri
	if uri.Scheme() == "file" && g.workDir == "" {
		if directory, err := filepath.Abs(filepath.Dir(uri.Path())); err == nil {
			g.app.Preferences().SetString(lastSaveDirectoryKey, directory)
		}
	}
	if g.document.Revision() == revision {
		if g.redactedTargets == nil {
			g.redactedTargets = make(map[string][]string)
		}
		g.redactedTargets[uri.String()] = g.document.RedactionKeys()
		g.document.MarkSaved()
	}
	g.updateHistoryActions()
	if saved != nil {
		saved()
	}
}

func suggestedExportName(name string) string {
	extension := filepath.Ext(name)
	format, err := document.FormatFromName(name)
	if err != nil || !document.CanExport(format) {
		extension = ".png"
	}
	return "Untitled" + strings.ToLower(extension)
}

// Remember removal per destination outside Undo. A later overwrite must warn
// before any writer is opened if Undo or deletion could restore content.
func (g *Gui) restoresRedactions(target fyne.URI) bool {
	if target == nil || g.document == nil {
		return false
	}
	current := make(map[string]bool)
	for _, key := range g.document.RedactionKeys() {
		current[key] = true
	}
	for _, key := range g.redactedTargets[target.String()] {
		if !current[key] {
			return true
		}
	}
	return false
}

func (g *Gui) confirmRedactionExport(target fyne.URI, action func()) {
	if target != nil && target.Scheme() != "file" && (g.document.HasRedactions() || g.restoresRedactions(target)) {
		g.showInputError(errors.New("redacted PDFs require a local destination for atomic saving; use Save As"))
		return
	}
	message := ""
	if g.document.HasRedactions() {
		message = "Selected text and overlapping content will be permanently removed from the exported PDF. Metadata, attachments, comments, actions and interactive fields will be removed; visible appearances will be flattened. Affected image areas will also be blacked out in every reuse of that image.\n\nThe original content and Undo remain available in this editing session. Other source files are not changed."
	}
	title := "Apply redactions?"
	if g.restoresRedactions(target) {
		title = "Restore content?"
		message += "\n\nSome previously saved redactions have been removed or changed. Overwriting this file may restore confidential content."
	}
	if message == "" {
		action()
		return
	}
	doc, revision := g.document, g.document.Revision()
	d := dialog.NewConfirm(title, message, func(ok bool) {
		if ok && g.document == doc && doc.Revision() == revision && !g.working {
			action()
		}
	}, g.window)
	d.SetConfirmText("Save")
	d.SetDismissText("Cancel")
	d.Resize(fyne.NewSize(600, 360))
	d.Show()
}

// Kept as the PDF-specific entry point used by redaction tests and callers.
func (g *Gui) showPDFSaveAsDialog(exports preparedExports, saved func()) {
	g.showLocalSaveAsDialog(exports, saved)
}

// Fyne's FileSave opens/truncates the destination before its callback. Select
// a path first for every format so Save As can confirm replacement safely.
func (g *Gui) showLocalSaveAsDialog(exports preparedExports, saved func()) {
	folder := g.initialSaveDirectory()
	name := widget.NewEntry()
	name.SetText(suggestedExportName(g.document.Name()))
	location := widget.NewLabel(folder)
	location.Wrapping = fyne.TextWrapBreak
	choose := widget.NewButton("Choose folder", func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				g.showOperationError("save", err)
				return
			}
			if uri != nil && uri.Scheme() == "file" {
				folder = uri.Path()
				location.SetText(folder)
			}
		}, g.window)
		if uri, err := storage.ListerForURI(storage.NewFileURI(folder)); err == nil {
			d.SetLocation(uri)
		}
		d.Show()
	})
	doc := g.document
	var chooser *dialog.CustomDialog
	save := widget.NewButton("Save", func() {
		filename := strings.TrimSpace(name.Text)
		format, formatErr := document.FormatFromName(filename)
		data, supported := exports.data[format]
		if filename == "" || filepath.Base(filename) != filename || strings.ContainsAny(filename, "/\\") || formatErr != nil || !supported {
			expected := ".png, .jpg, or .jpeg"
			if g.document.Format() == document.FormatPDF {
				expected = ".pdf"
			}
			g.showInputError(fmt.Errorf("enter a file name with a supported extension (%s)", expected))
			return
		}
		if g.document != doc || doc.Revision() != exports.revision {
			g.showInputError(errors.New("the document changed; save again"))
			return
		}
		uri := storage.NewFileURI(filepath.Join(folder, filename))
		proceed := func() {
			if g.document != doc || doc.Revision() != exports.revision {
				return
			}
			chooser.Hide()
			g.finishFileSave(data, uri, exports.revision, saved)
		}
		_, err := os.Stat(uri.Path())
		if err != nil && !os.IsNotExist(err) {
			g.showOperationError("save", err)
			return
		}
		if err == nil || g.restoresRedactions(uri) {
			message := "Replace the existing file?"
			if g.restoresRedactions(uri) {
				message += "\n\nSome previously saved redactions have been removed or changed. This may restore confidential content."
			}
			d := dialog.NewConfirm("Replace file?", message, func(ok bool) {
				if ok {
					proceed()
				}
			}, g.window)
			d.SetConfirmText("Replace")
			d.SetDismissText("Cancel")
			d.Show()
		} else {
			proceed()
		}
	})
	save.Importance = widget.HighImportance
	cancel := widget.NewButton("Cancel", func() { chooser.Hide() })
	content := container.NewVBox(widget.NewLabel("File name"), name, widget.NewLabel("Folder"), location, choose, container.NewHBox(cancel, save))
	chooser = dialog.NewCustomWithoutButtons("Save As", content, g.window)
	g.pauseExternalOpening(chooser)
	chooser.Resize(fyne.NewSize(560, 320))
	chooser.Show()
}

func (g *Gui) initialSaveDirectory() string {
	directories := []string{g.workDir, g.app.Preferences().String(lastSaveDirectoryKey)}
	if g.saveTarget != nil && g.saveTarget.Scheme() == "file" {
		directories = append(directories, filepath.Dir(g.saveTarget.Path()))
	}
	for _, directory := range directories {
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			return directory
		}
	}
	home, _ := os.UserHomeDir()
	return home
}
