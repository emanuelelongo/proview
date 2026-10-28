package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"emanuelelongo/proview/internal/appprofile"
	"emanuelelongo/proview/internal/openfiles"
)

type launchOptions struct {
	openfiles.Request
	checkOCR   bool
	foreground bool
	dataDir    string
}

func parseLaunchOptions(args []string, output io.Writer) (launchOptions, error) {
	var options launchOptions
	flags := flag.NewFlagSet("proview", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.BoolVar(&options.foreground, "foreground", false, "Keep the application attached to the terminal for diagnostics")
	flags.BoolVar(&options.NewWindow, "new-window", false, "Open a new editor window, including in an already running application")
	flags.StringVar(&options.WorkDir, "work-dir", "", "Start this window's open and Save As dialogs in this directory")
	flags.StringVar(&options.dataDir, "data-dir", "", "Use a separate application data directory and preferences profile")
	flags.BoolVar(&options.checkOCR, "check-ocr", false, "Check the bundled OCR runtime and exit")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: proview [options] [--] [files...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	options.Paths = flags.Args()
	if options.checkOCR && (options.foreground || options.NewWindow || options.WorkDir != "" || options.dataDir != "" || len(options.Paths) != 0) {
		return options, fmt.Errorf("--check-ocr must be used on its own")
	}
	if options.WorkDir != "" {
		directory, err := filepath.Abs(options.WorkDir)
		if err != nil {
			return options, fmt.Errorf("invalid --work-dir: %w", err)
		}
		info, err := os.Stat(directory)
		if err != nil {
			return options, fmt.Errorf("cannot use --work-dir: %w", err)
		}
		if !info.IsDir() {
			return options, fmt.Errorf("--work-dir must name an existing directory")
		}
		options.WorkDir = filepath.Clean(directory)
	} else {
		// An explicitly empty directory is usually a missing automation argument.
		supplied := false
		flags.Visit(func(value *flag.Flag) {
			if value.Name == "work-dir" {
				supplied = true
			}
		})
		if supplied {
			return options, fmt.Errorf("--work-dir must name an existing directory")
		}
	}
	suppliedData := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "data-dir" {
			suppliedData = true
		}
	})
	if suppliedData {
		if options.dataDir == "" {
			return options, fmt.Errorf("--data-dir must name a directory")
		}
		if options.checkOCR {
			return options, fmt.Errorf("--check-ocr must be used on its own")
		}
		directory, err := appprofile.Resolve(options.dataDir)
		if err != nil {
			return options, fmt.Errorf("cannot use --data-dir: %w", err)
		}
		options.dataDir = directory
	}

	return options, nil
}
