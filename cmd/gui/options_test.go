package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchOptionsResolveWorkDirWithoutChangingCWDOrFileArguments(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	directory := filepath.Join(root, "work")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, directory)
	if err != nil {
		t.Fatal(err)
	}
	options, err := parseLaunchOptions([]string{"--new-window", "--work-dir", relative, "photo with spaces.png", "doc.pdf"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !options.NewWindow || options.WorkDir != directory || !reflect.DeepEqual(options.Paths, []string{"photo with spaces.png", "doc.pdf"}) {
		t.Fatal("incorrect launch options", options)
	}
	after, err := os.Getwd()
	if err != nil || after != cwd {
		t.Fatal("work-dir changed process cwd")
	}
	options, err = parseLaunchOptions([]string{"--", "--new-window.png"}, &bytes.Buffer{})
	if err != nil || options.NewWindow || !reflect.DeepEqual(options.Paths, []string{"--new-window.png"}) {
		t.Fatal("escaped file arguments were parsed as flags")
	}
}

func TestLaunchOptionsRejectInvalidDirectoryAndOCRCombinations(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--work-dir"}, {"--work-dir="}, {"--work-dir", file}, {"--work-dir", filepath.Join(t.TempDir(), "missing")}, {"--check-ocr", "--new-window"}, {"--check-ocr", "file.pdf"}, {"--unknown"}} {
		if _, err := parseLaunchOptions(args, &bytes.Buffer{}); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
	options, err := parseLaunchOptions([]string{"--check-ocr"}, &bytes.Buffer{})
	if err != nil || !options.checkOCR || options.NewWindow || options.WorkDir != "" || len(options.Paths) != 0 {
		t.Fatal("standalone OCR check changed", options, err)
	}
}

func TestLaunchHelpDescribesWindowOptions(t *testing.T) {
	var output bytes.Buffer
	_, err := parseLaunchOptions([]string{"--help"}, &output)
	if !errors.Is(err, flag.ErrHelp) || !strings.Contains(output.String(), "new-window") || !strings.Contains(output.String(), "work-dir") {
		t.Fatal("help missing launch options", output.String(), err)
	}
}

func TestDataDirectoryCreationAndValidation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new", "profile")
	options, err := parseLaunchOptions([]string{"--data-dir", root, "--new-window", "sample.pdf"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if options.dataDir != canonical || !options.NewWindow || len(options.Paths) != 1 {
		t.Fatalf("unexpected options: %+v", options)
	}
	for _, args := range [][]string{{"--data-dir="}, {"--check-ocr", "--data-dir", root}} {
		if _, err := parseLaunchOptions(args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseLaunchOptions([]string{"--data-dir", file}, io.Discard); err == nil {
		t.Fatal("accepted a file as data directory")
	}
}
