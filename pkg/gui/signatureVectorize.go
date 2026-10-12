package gui

import (
	"context"
	"image"
	"image/color"
	"math"

	xdraw "golang.org/x/image/draw"

	"emanuelelongo/proview/pkg/signatures"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type signatureTraceRequest struct {
	ctx      context.Context
	options  signatures.TraceOptions
	revision uint64
}
type signatureTraceResult struct {
	asset   signatures.Asset
	preview image.Image
	err     error
}

// All editor state belongs to the Fyne thread; only immutable requests cross to
// the worker. A one-slot mailbox and cancellation coalesce slider updates.
func (g *Gui) editSignatureScan(scan signatures.Scan, done func(signatures.Entry)) {
	source := newSignatureScanView(scan.Image)
	preview := canvas.NewImageFromImage(nil)
	preview.FillMode = canvas.ImageFillContain
	preview.SetMinSize(fyne.NewSize(320, 240))
	status := widget.NewLabel("")
	status.Hide()
	status.Wrapping = fyne.TextWrapWord
	intensity := widget.NewSlider(0, 100)
	intensity.Step = 1
	intensity.Value = 50
	crop := scan.Image.Bounds()
	var asset signatures.Asset
	var editor *dialog.CustomDialog
	closed := false
	revision := uint64(0)
	var cancel context.CancelFunc
	stop := make(chan struct{})
	requests := make(chan signatureTraceRequest, 1)
	var save *widget.Button
	save = widget.NewButton("Save", func() {
		if closed || save.Disabled() || source.crop.selecting || asset.Kind != signatures.SVG {
			return
		}
		g.saveArchiveEntry(signatures.Entry{Asset: asset.Clone()}, func(entry signatures.Entry) {
			editor.Hide()
			if done != nil {
				done(entry)
			}
		})
	})
	save.Importance = widget.HighImportance
	save.Disable()
	go func() {
		for {
			select {
			case <-stop:
				return
			case request := <-requests:
				result := signatureTraceResult{}
				result.asset, result.err = scan.Vectorize(request.ctx, request.options)
				if result.err == nil {
					result.preview, result.err = result.asset.Render(800)
				}
				if request.ctx.Err() != nil {
					continue
				}
				fyne.Do(func() {
					if closed || g.closed || request.revision != revision {
						return
					}
					if result.err != nil {
						preview.Image = nil
						preview.Refresh()
						status.SetText(result.err.Error())
						status.Show()
						return
					}
					asset = result.asset
					preview.Image = result.preview
					preview.Refresh()
					status.Hide()
					save.Enable()
				})
			}
		}
	}()
	schedule := func() {
		if closed {
			return
		}
		revision++
		if cancel != nil {
			cancel()
		}
		ctx, newCancel := context.WithCancel(context.Background())
		cancel = newCancel
		save.Disable()
		asset = signatures.Asset{}
		status.Hide()
		request := signatureTraceRequest{ctx: ctx, options: signatures.TraceOptions{Crop: crop, Intensity: intensity.Value, Smooth: 100}, revision: revision}
		select {
		case <-requests:
		default:
		}
		requests <- request
	}
	intensity.OnChanged = func(float64) { schedule() }
	source.crop.ActivateLive(image.Rect(0, 0, crop.Dx(), crop.Dy()), func(rect image.Rectangle) {
		crop = rect.Add(scan.Image.Bounds().Min)
		schedule()
	})
	panels := container.NewGridWithColumns(2,
		container.NewBorder(widget.NewLabel("Source"), nil, nil, nil, source),
		container.NewBorder(widget.NewLabel("Preview"), nil, nil, nil, container.NewStack(canvas.NewRectangle(color.NRGBA{R: 235, G: 238, B: 242, A: 255}), preview)),
	)
	controls := container.NewVBox(widget.NewLabel("Sensitivity"), intensity, status)
	editor = dialog.NewCustomWithoutButtons("Import signature", container.NewBorder(nil, controls, nil, nil, panels), g.window)
	editor.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", editor.Hide), save})
	g.showSignatureDialog(editor, func() {
		if closed {
			return
		}
		closed = true
		if cancel != nil {
			cancel()
		}
		close(stop)
	})
	schedule()
}

type signatureScanView struct {
	widget.BaseWidget
	picture    *canvas.Image
	crop       *cropOverlay
	sourceSize image.Point
}

func newSignatureScanView(source image.Image) *signatureScanView {
	view := &signatureScanView{picture: canvas.NewImageFromImage(source), crop: newCropOverlay()}
	view.picture.FillMode = canvas.ImageFillStretch
	view.ExtendBaseWidget(view)
	view.setImage(source, source.Bounds())
	return view
}

func (view *signatureScanView) setImage(source image.Image, crop image.Rectangle) {
	// The overlay measures the displayed crop relative to its top-left corner;
	// the editor maps each selection back into the immutable full scan.
	scale := math.Min(1, 1200/float64(max(crop.Dx(), crop.Dy())))
	img := image.NewNRGBA(image.Rect(0, 0, max(1, int(math.Round(float64(crop.Dx())*scale))), max(1, int(math.Round(float64(crop.Dy())*scale)))))
	xdraw.ApproxBiLinear.Scale(img, img.Bounds(), source, crop, xdraw.Src, nil)
	view.sourceSize = crop.Size()
	view.picture.Image = img
	view.Refresh()
}

func (view *signatureScanView) CreateRenderer() fyne.WidgetRenderer {
	return &signatureScanRenderer{view: view, background: canvas.NewRectangle(color.NRGBA{R: 235, G: 238, B: 242, A: 255})}
}

type signatureScanRenderer struct {
	view       *signatureScanView
	background *canvas.Rectangle
}

func (r *signatureScanRenderer) Destroy()           {}
func (r *signatureScanRenderer) MinSize() fyne.Size { return fyne.NewSize(320, 240) }
func (r *signatureScanRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.background, r.view.picture, r.view.crop}
}
func (r *signatureScanRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	sourceSize := r.view.sourceSize
	scale := min(size.Width/float32(sourceSize.X), size.Height/float32(sourceSize.Y))
	fitted := fyne.NewSize(float32(sourceSize.X)*scale, float32(sourceSize.Y)*scale)
	position := fyne.NewPos((size.Width-fitted.Width)/2, (size.Height-fitted.Height)/2)
	r.view.picture.Move(position)
	r.view.picture.Resize(fitted)
	r.view.crop.Move(position)
	r.view.crop.Resize(fitted)
}
func (r *signatureScanRenderer) Refresh() {
	r.Layout(r.view.Size())
	r.background.Refresh()
	r.view.picture.Refresh()
	r.view.crop.Refresh()
}
