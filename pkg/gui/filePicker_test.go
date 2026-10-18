package gui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
	"github.com/ncruces/zenity"
)

func pickerTestState(t *testing.T, done func([]fyne.URIReadCloser, error)) (*Gui, *filePickerState) {
	t.Helper()
	a := fyneTest.NewApp()
	g := newGui(a)
	g.window.Show()
	_, cancel := context.WithCancel(context.Background())
	state := &filePickerState{cancel: cancel, done: done}
	g.filePicker, g.working, filePickerOwner = state, true, g
	t.Cleanup(func() { g.cancelFilePicker(); a.Quit() })
	return g, state
}

func TestNativePickerCancellationDoesNotFallBack(t *testing.T) {
	for _, pickerErr := range []error{zenity.ErrCanceled, context.Canceled} {
		t.Run(pickerErr.Error(), func(t *testing.T) {
			called := false
			g, state := pickerTestState(t, func(readers []fyne.URIReadCloser, err error) {
				called = true
				if len(readers) != 0 || err != nil {
					t.Fatalf("cancellation returned files or error: %v", err)
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if errors.Is(pickerErr, context.Canceled) {
				cancel()
			}
			g.completeNativeFilePicker(ctx, state, filePickerRequest{}, nil, pickerErr)
			if !called || g.working || g.filePicker != nil || g.window.Canvas().Overlays().Top() != nil {
				t.Fatal("cancellation left a picker or busy state")
			}
		})
	}
}

func TestNativePickerFailureShowsCancellableFyneFallback(t *testing.T) {
	called := false
	g, state := pickerTestState(t, func([]fyne.URIReadCloser, error) { called = true })
	g.completeNativeFilePicker(context.Background(), state, filePickerRequest{
		title: "Import signature", extensions: []string{".svg", ".png"}, directory: t.TempDir(),
	}, nil, zenity.ErrUnsupported)
	if called || !g.working || g.window.Canvas().Overlays().Top() == nil {
		t.Fatal("native failure did not open the fallback")
	}
	tapDialogButton(t, g, "Cancel")
	if !called || g.working || g.filePicker != nil || filePickerOwner != nil {
		t.Fatal("fallback cancellation left a picker or busy state")
	}
}

func TestNativePickerMultipleFilesKeepOrderAndOriginWindow(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "firma è blu.pdf"), filepath.Join(dir, "second.pdf")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var g *Gui
	called := false
	g, state := pickerTestState(t, func(readers []fyne.URIReadCloser, err error) {
		called = true
		defer closePickedFiles(readers)
		if err != nil || g.working || len(readers) != len(paths) {
			t.Fatalf("unexpected completion: %v", err)
		}
		for i, reader := range readers {
			data, err := io.ReadAll(reader)
			if err != nil || string(data) != filepath.Base(paths[i]) || filepath.Clean(reader.URI().Path()) != filepath.Clean(paths[i]) {
				t.Fatalf("file %d: path=%q, want %q; content=%q, error=%v", i, reader.URI().Path(), paths[i], data, err)
			}
		}
	})
	other := newGui(g.app)
	other.window.Show()
	g.completeNativeFilePicker(context.Background(), state, filePickerRequest{multiple: true}, paths, nil)
	if !called || other.filePicker != nil || g.filePickerDirectory != dir {
		t.Fatal("result was not delivered to the requesting window")
	}
}

func TestNativePickerIgnoresLateResultAfterClose(t *testing.T) {
	called := false
	g, state := pickerTestState(t, func([]fyne.URIReadCloser, error) { called = true })
	g.closeWindow()
	g.completeNativeFilePicker(context.Background(), state, filePickerRequest{}, []string{"missing.pdf"}, nil)
	if called || !g.closed || g.filePicker != nil || filePickerOwner != nil {
		t.Fatal("closed window accepted a late result")
	}
}

func TestNativePickerMissingFileIsAnErrorNotFallback(t *testing.T) {
	called := false
	g, state := pickerTestState(t, func(readers []fyne.URIReadCloser, err error) {
		called = true
		if err == nil || len(readers) != 0 {
			t.Fatal("missing file was accepted")
		}
	})
	g.completeNativeFilePicker(context.Background(), state, filePickerRequest{}, []string{filepath.Join(t.TempDir(), "missing.pdf")}, nil)
	if !called || g.working || g.window.Canvas().Overlays().Top() != nil {
		t.Fatal("file read error reopened the picker")
	}
}

func TestNativePickerHasNoWaitingDialogAndFallsBackDirectly(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		name := "cancel"
		if fallback {
			name = "fallback"
		}
		t.Run(name, func(t *testing.T) {
			g, state := pickerTestState(t, func([]fyne.URIReadCloser, error) {})
			if fallback {
				g.completeNativeFilePicker(context.Background(), state, filePickerRequest{
					title: "Open file", extensions: []string{".pdf"}, directory: t.TempDir(),
				}, nil, zenity.ErrUnsupported)
				if g.filePicker != state || !g.working || g.window.Canvas().Overlays().Top() == nil {
					t.Fatal("native failure did not open the Fyne fallback")
				}
				tapDialogButton(t, g, "Cancel")
			} else {
				if g.window.Canvas().Overlays().Top() != nil {
					t.Fatal("native picker displayed a temporary Fyne dialog")
				}
				g.cancelFilePicker()
			}
			if g.filePicker != nil || g.working || filePickerOwner != nil {
				t.Fatal("Cancel did not release the picker")
			}
		})
	}
}
