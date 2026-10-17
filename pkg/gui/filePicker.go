package gui

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/storage"
	"github.com/ncruces/zenity"
)

type filePickerRequest struct {
	title      string
	extensions []string
	multiple   bool
	directory  string
	parent     any
}

type filePickerBackend func(context.Context, filePickerRequest) ([]string, error)

type filePickerState struct {
	cancel context.CancelFunc
	hide   func()
	done   func([]fyne.URIReadCloser, error)
}

// Native dialogs on macOS belong to the application, so serialize pickers
// across editor windows. This state is accessed only on the Fyne event thread.
var filePickerOwner *Gui

func nativeFilePicker(ctx context.Context, request filePickerRequest) ([]string, error) {
	patterns := make([]string, 0, len(request.extensions))
	for _, extension := range request.extensions {
		patterns = append(patterns, "*"+extension)
	}
	options := []zenity.Option{zenity.Context(ctx), zenity.Title(request.title), zenity.Modal(),
		zenity.FileFilter{Name: "Supported files", Patterns: patterns, CaseFold: true}}
	if request.directory != "" {
		options = append(options, zenity.Filename(request.directory+string(os.PathSeparator)))
	}
	if request.parent != nil {
		options = append(options, zenity.Attach(request.parent))
	}
	if request.multiple {
		return zenity.SelectFileMultiple(options...)
	}
	path, err := zenity.SelectFile(options...)
	if err != nil || path == "" {
		return nil, err
	}
	return []string{path}, nil
}

func (g *Gui) chooseFile(title string, extensions []string, done func(fyne.URIReadCloser, error)) {
	g.chooseFiles(title, extensions, false, func(readers []fyne.URIReadCloser, err error) {
		if len(readers) == 0 {
			done(nil, err)
			return
		}
		done(readers[0], err)
	})
}

func (g *Gui) chooseFiles(title string, extensions []string, multiple bool, done func([]fyne.URIReadCloser, error)) {
	if g.closed || g.working {
		return
	}
	if filePickerOwner != nil {
		filePickerOwner.window.RequestFocus()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	state := &filePickerState{cancel: cancel, done: done}
	g.filePicker, filePickerOwner, g.working = state, g, true
	request := filePickerRequest{title: title, extensions: extensions, multiple: multiple, directory: g.filePickerDirectory}
	if request.directory == "" {
		request.directory, _ = os.UserHomeDir()
	}
	backend := g.filePickerBackend
	if backend == nil {
		native, ok := g.window.(driver.NativeWindow)
		available := ok && zenity.IsAvailable()
		if runtime.GOOS == "darwin" {
			_, err := exec.LookPath("osascript")
			available = available && err == nil
		}
		if !available {
			g.showFyneFilePicker(state, request)
			return
		}
		native.RunNative(func(value any) {
			switch value := value.(type) {
			case driver.MacWindowContext:
				// Standard Additions cannot run chooseFile in a non-scriptable
				// Fyne process. Let osascript own the AppKit panel instead.
			case driver.WindowsWindowContext:
				if value.HWND != 0 {
					request.parent = value.HWND
				}
			case driver.X11WindowContext:
				if value.WindowHandle != 0 {
					request.parent = int(value.WindowHandle)
				}
			}
		})
		backend = nativeFilePicker
	}
	go func() {
		paths, err := backend(ctx, request)
		fyne.Do(func() { g.completeNativeFilePicker(ctx, state, request, paths, err) })
	}()
}

func (g *Gui) completeNativeFilePicker(ctx context.Context, state *filePickerState, request filePickerRequest, paths []string, err error) {
	if g.filePicker != state || g.closed {
		return
	}
	if errors.Is(err, zenity.ErrCanceled) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
		g.finishFilePicker(state, nil, nil)
		return
	}
	if err != nil {
		log.Printf("Native file dialog unavailable: %v", err)
		if state.hide != nil {
			state.hide()
		}
		g.showFyneFilePicker(state, request)
		return
	}
	readers, openErr := openPickedFiles(paths)
	g.finishFilePicker(state, readers, openErr)
}

func openPickedFiles(paths []string) ([]fyne.URIReadCloser, error) {
	var readers []fyne.URIReadCloser
	for _, path := range paths {
		reader, err := storage.Reader(storage.NewFileURI(path))
		if err != nil {
			closePickedFiles(readers)
			return nil, err
		}
		readers = append(readers, reader)
	}
	return readers, nil
}

func closePickedFiles(readers []fyne.URIReadCloser) {
	for _, reader := range readers {
		_ = reader.Close()
	}
}

func (g *Gui) showFyneFilePicker(state *filePickerState, request filePickerRequest) {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		var readers []fyne.URIReadCloser
		if reader != nil {
			readers = []fyne.URIReadCloser{reader}
		}
		g.finishFilePicker(state, readers, err)
	}, g.window)
	picker.SetTitleText(request.title)
	picker.SetFilter(storage.NewExtensionFileFilter(request.extensions))
	if location, err := storage.ListerForURI(storage.NewFileURI(request.directory)); err == nil {
		picker.SetLocation(location)
	}
	state.hide = picker.Hide
	picker.Resize(fyne.NewSize(720, 480))
	picker.Show()
}

func (g *Gui) finishFilePicker(state *filePickerState, readers []fyne.URIReadCloser, err error) {
	defer g.processPendingFiles()
	if g.filePicker != state || g.closed {
		closePickedFiles(readers)
		return
	}
	g.filePicker, filePickerOwner, g.working = nil, nil, false
	state.cancel()
	if state.hide != nil {
		state.hide()
	}
	g.window.RequestFocus()
	if len(readers) != 0 {
		g.filePickerDirectory = filepath.Dir(readers[0].URI().Path())
	}
	state.done(readers, err)
}

func (g *Gui) cancelFilePicker() {
	state := g.filePicker
	if state == nil {
		return
	}
	g.filePicker, g.working = nil, false
	if filePickerOwner == g {
		filePickerOwner = nil
	}
	state.cancel()
	if state.hide != nil {
		state.hide()
	}
}
