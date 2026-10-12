package gui

import (
	"emanuelelongo/proview/pkg/document"
	"emanuelelongo/proview/pkg/signatures"
	"emanuelelongo/proview/pkg/signatures/cloud"
	"image"
	"image/color"
	"io"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) signatureArchive() (*signatures.Store, bool) {
	if g.signatureStore == nil {
		service, err := signatureService(g.app)
		if err != nil {
			dialog.ShowError(err, g.window)
			return nil, false
		}
		g.signatureStore = service.store
		g.signatureCloud = service
	}
	return g.signatureStore, true
}

func (g *Gui) signatureViewport() fyne.Size {
	size := g.window.Canvas().Size()
	if size.Width <= 0 || size.Height <= 0 {
		size = fyne.NewSize(1000, 700)
	}
	return fyne.NewSize(max(240, min(620, size.Width-80)), max(180, min(520, size.Height-180)))
}

func (g *Gui) showSignatureLibrary() {
	// This dialog manages the archive; insertion belongs to the dropdown.
	store, ok := g.signatureArchive()
	if !ok {
		return
	}
	content := container.NewVBox(widget.NewLabel("Loading signatures"))
	scroll := container.NewVScroll(content)
	size := g.signatureViewport()
	size.Height = min(size.Height, 350)
	scroll.SetMinSize(size)
	var library *dialog.CustomDialog
	newButton := widget.NewButton("New signature", func() {
		library.Hide()
		g.acquireSignature(func(entry signatures.Entry) {
			g.showSignatureLibrary()
		})
	})
	cloudStatus := widget.NewLabel("")
	cloudStatus.Wrapping = fyne.TextWrapWord
	if g.signatureCloud == nil {
		cloudStatus.Hide()
	} else {
		cloudStatus.SetText(g.signatureCloud.message(g.signatureCloud.coordinator.Status()))
	}
	library = dialog.NewCustomWithoutButtons("Saved signatures", container.NewBorder(cloudStatus, nil, nil, nil, scroll), g.window)
	library.SetButtons([]fyne.CanvasObject{newButton, widget.NewButton("Cloud sync", func() { library.Hide(); g.showSignatureCloud() }), widget.NewButton("Close", library.Hide)})
	closed := false
	var unsubscribe func()
	g.showSignatureDialog(library, func() {
		closed = true
		if unsubscribe != nil {
			unsubscribe()
		}
	})
	loadGeneration := uint64(0)
	load := func() {
		loadGeneration++
		generation := loadGeneration
		go func() {
			entries, listErr := store.List()
			previews := make([]image.Image, len(entries))
			for i, entry := range entries {
				previews[i], _ = entry.Asset.Render(180)
			}
			fyne.Do(func() {
				if closed || generation != loadGeneration {
					return
				}
				content.Objects = nil
				if listErr != nil {
					content.Add(widget.NewLabel("Some signatures could not be read: " + listErr.Error()))
				}
				if len(entries) == 0 {
					content.Add(widget.NewLabel("No saved signatures. Create your first signature."))
				}
				for i, entry := range entries {
					entry := entry
					preview := canvas.NewImageFromImage(previews[i])
					preview.FillMode = canvas.ImageFillContain
					preview.SetMinSize(fyne.NewSize(150, 65))
					picture := newSignatureChoice(container.NewStack(canvas.NewRectangle(color.White), preview), nil)
					if entry.Asset.Drawing != nil {
						picture.tapped = func() { library.Hide(); g.editArchiveDrawing(entry, func() { g.showSignatureLibrary() }) }
					}
					remove := widget.NewButtonWithIcon("", tablerIcon("trash"), func() {
						dialog.ShowConfirm("Delete signature", "Delete this signature from the library? Copies in documents will remain available.", func(confirm bool) {
							if !confirm {
								return
							}
							go func() {
								err := store.Delete(entry.ID)
								if err == nil && g.signatureCloud != nil {
									g.signatureCloud.push()
								}
								fyne.Do(func() {
									if err != nil {
										dialog.ShowError(err, g.window)
										return
									}
									library.Hide()
									g.showSignatureLibrary()
								})
							}()
						}, g.window)
					})
					remove.Importance = widget.DangerImportance
					content.Add(container.New(signatureLibraryRowLayout{}, picture, remove))
				}
				content.Refresh()
			})
		}()
	}
	load()
	if g.signatureCloud != nil {
		unsubscribe = g.signatureCloud.coordinator.Subscribe(func(status cloud.Status) {
			fyne.Do(func() {
				if !closed {
					cloudStatus.SetText(g.signatureCloud.message(status))
					if !status.Busy {
						load()
					}
				}
			})
		})
		g.signatureCloud.open()
	}
}

func (g *Gui) acquireSignature(done func(signatures.Entry)) {
	if _, ok := g.signatureArchive(); !ok {
		return
	}
	method := widget.NewRadioGroup([]string{"Import from file", "Draw with mouse"}, nil)
	method.Required = true
	method.SetSelected("Import from file")
	choice := dialog.NewCustomWithoutButtons("Acquire signature", container.NewPadded(method), g.window)
	ok := widget.NewButton("OK", func() {
		choice.Hide()
		switch method.Selected {
		case "Import from file":
			g.importArchiveSignature(done)
		case "Draw with mouse":
			g.editDrawing(signatures.Drawing{Version: 1, Width: 4, Smooth: 25}, true, func(asset signatures.Asset, closeEditor func()) {
				g.saveArchiveEntry(signatures.Entry{Asset: asset}, func(entry signatures.Entry) {
					closeEditor()
					if done != nil {
						done(entry)
					}
				})
			})
		}
	})
	ok.Importance = widget.HighImportance
	choice.SetButtons([]fyne.CanvasObject{ok, widget.NewButton("Cancel", choice.Hide)})
	g.showSignatureDialog(choice, nil)
}

func (g *Gui) importArchiveSignature(done func(signatures.Entry)) {
	g.chooseFile("Import signature", []string{".svg", ".png", ".jpg", ".jpeg"}, func(uri fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, g.window)
			return
		}
		if uri == nil {
			return
		}
		filename := uri.URI().Name()
		progress := dialog.NewCustomWithoutButtons("Importing signature", widget.NewProgressBarInfinite(), g.window)
		progress.Show()
		go func() {
			data, readErr := io.ReadAll(io.LimitReader(uri, signatures.MaxFileSize+1))
			closeErr := uri.Close()
			if readErr == nil {
				readErr = closeErr
			}
			var asset signatures.Asset
			var preview image.Image
			var scan signatures.Scan
			if readErr == nil {
				asset, scan, readErr = signatureImport(filename, data)
			}
			if readErr == nil && scan.Image == nil {
				preview, readErr = asset.Render(600)
			}
			fyne.Do(func() {
				progress.Hide()
				if g.closed {
					return
				}
				if readErr != nil {
					dialog.ShowError(readErr, g.window)
					return
				}
				if scan.Image != nil {
					g.editSignatureScan(scan, done)
					return
				}
				picture := canvas.NewImageFromImage(preview)
				picture.FillMode = canvas.ImageFillContain
				picture.SetMinSize(fyne.NewSize(400, 180))
				var confirmation *dialog.CustomDialog
				confirmation = dialog.NewCustomWithoutButtons("Import signature", container.NewStack(canvas.NewRectangle(color.White), picture), g.window)
				save := widget.NewButton("Save", func() {
					g.saveArchiveEntry(signatures.Entry{Asset: asset}, func(entry signatures.Entry) {
						confirmation.Hide()
						if done != nil {
							done(entry)
						}
					})
				})
				save.Importance = widget.HighImportance
				confirmation.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", confirmation.Hide), save})
				g.showSignatureDialog(confirmation, nil)
			})
		}()
	})
}

// SVG data stays vector; raster sources always enter the import preview.
func signatureImport(filename string, data []byte) (signatures.Asset, signatures.Scan, error) {
	if strings.EqualFold(filepath.Ext(filename), ".svg") {
		asset, err := signatures.Import(filename, data)
		return asset, signatures.Scan{}, err
	}
	scan, err := signatures.DecodeScan(data)
	return signatures.Asset{}, scan, err
}

func (g *Gui) saveArchiveEntry(entry signatures.Entry, done func(signatures.Entry)) {
	store, ok := g.signatureArchive()
	if !ok {
		return
	}
	progress := dialog.NewCustomWithoutButtons("Saving signature", widget.NewProgressBarInfinite(), g.window)
	progress.Show()
	go func() {
		saved, err := store.Save(entry)
		if err == nil && g.signatureCloud != nil {
			g.signatureCloud.push()
		}
		fyne.Do(func() {
			progress.Hide()
			if err != nil {
				dialog.ShowError(err, g.window)
				return
			}
			if done != nil {
				done(saved)
			}
		})
	}()
}

func (g *Gui) insertSignatureAsset(asset signatures.Asset) {
	if g.document == nil || g.working {
		return
	}
	doc := g.document
	page := 0
	if doc.Format() == document.FormatPDF {
		page = g.activePDFPage
	}
	g.working = true
	progress := dialog.NewCustomWithoutButtons("Preparing signature", widget.NewProgressBarInfinite(), g.window)
	progress.Show()
	go func() {
		prepared, err := asset.Prepare()
		pageSize, sizeErr := doc.PageSize(page)
		if err == nil {
			err = sizeErr
		}
		fyne.Do(func() {
			progress.Hide()
			g.working = false
			if err != nil {
				dialog.ShowError(err, g.window)
				return
			}
			if g.document != doc {
				return
			}
			if doc.Format() != document.FormatPDF && g.image.Image != nil {
				pageSize = g.image.Image.Bounds().Size()
			}
			size := prepared.Preview.Bounds().Size()
			rect, err := defaultSignatureRect(pageSize.X, pageSize.Y, size.X, size.Y)
			if err != nil {
				dialog.ShowError(err, g.window)
				return
			}
			signature, err := doc.AddSignatureAsset(page, rect, prepared)
			if err != nil {
				dialog.ShowError(err, g.window)
				return
			}
			layer := g.signatureLayers[page]
			if layer != nil {
				g.setAnnotationTool(toolSelect)
				layer.reload()
				g.selectSignature(layer, signature.ID)
			}
			g.documentChanged(page)
		})
	}()
}

func (g *Gui) editArchiveDrawing(entry signatures.Entry, done func()) {
	g.editDrawing(entry.Asset.Drawing.Clone(), true, func(asset signatures.Asset, closeEditor func()) {
		entry.Asset = asset
		g.saveArchiveEntry(entry, func(signatures.Entry) {
			closeEditor()
			if done != nil {
				done()
			}
		})
	})
}

func (g *Gui) editSelectedSignature() {
	if g.document == nil || !g.hasSelectedSignature {
		dialog.ShowInformation("Signature", "Select a signature drawn in Proview.", g.window)
		return
	}
	for _, signature := range g.document.Signatures() {
		if signature.ID == g.selectedSignature {
			g.editSelectedDrawing(signature)
			return
		}
	}
}

func (g *Gui) editSelectedDrawing(signature document.SignatureOverlay) {
	asset := signature.Asset()
	if asset.Drawing == nil {
		dialog.ShowInformation("Smoothness", "Smoothness is available for signatures drawn in Proview.", g.window)
		return
	}
	doc := g.document
	g.editDrawing(asset.Drawing.Clone(), false, func(updated signatures.Asset, closeEditor func()) {
		if g.working {
			return
		}
		g.working = true
		progress := dialog.NewCustomWithoutButtons("Updating signature", widget.NewProgressBarInfinite(), g.window)
		progress.Show()
		go func() {
			prepared, err := updated.Prepare()
			fyne.Do(func() {
				progress.Hide()
				g.working = false
				if err != nil {
					dialog.ShowError(err, g.window)
					return
				}
				if g.document != doc {
					closeEditor()
					return
				}
				if err = doc.ReplaceSignatureAsset(signature.ID, prepared); err != nil {
					dialog.ShowError(err, g.window)
					return
				}
				if layer := g.signatureLayers[signature.Page]; layer != nil {
					layer.reload()
					g.selectSignature(layer, signature.ID)
				}
				g.documentChanged(signature.Page)
				closeEditor()
			})
		}()
	})
}

// Track the topmost signature dialog so Escape never dismisses one behind a prompt.
func (g *Gui) showSignatureDialog(d *dialog.CustomDialog, onClosed func()) {
	d.SetOnClosed(func() {
		if g.signatureDialog == d {
			g.signatureDialog = nil
			g.signatureDialogOverlay = nil
		}
		if onClosed != nil {
			onClosed()
		}
	})
	d.Show()
	g.signatureDialog = d
	g.signatureDialogOverlay = g.window.Canvas().Overlays().Top()
}

func (g *Gui) closeSignatureDialogOnEscape() bool {
	if g.signaturePopup != nil && g.window.Canvas().Overlays().Top() == g.signaturePopup {
		g.signaturePopup.Hide()
		g.signaturePopup = nil
		return true
	}
	if g.signatureDialog == nil || g.window.Canvas().Overlays().Top() != g.signatureDialogOverlay {
		return false
	}
	g.signatureDialog.Hide()
	return true
}
