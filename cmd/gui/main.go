package main

import (
	"context"
	"emanuelelongo/proview/internal/applog"
	"emanuelelongo/proview/internal/appprofile"
	"emanuelelongo/proview/internal/buildinfo"
	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/internal/openfiles"
	"emanuelelongo/proview/pkg/gui"
	"errors"
	"flag"
	"fmt"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func main() {
	options, err := parseLaunchOptions(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "proview:", err)
		os.Exit(2)
	}
	if options.checkOCR {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := ocr.SelfTest(ctx); err != nil {
			log.Println(err)
			os.Exit(1)
		}
		log.Println("Bundled OCR check passed")
		return
	}

	appprofile.Root = options.dataDir

	closeLogs, logErr := applog.Initialize()
	if logErr != nil {
		log.Printf("level=ERROR event=log_setup error=%q", logErr)
	}
	defer closeLogs()
	info := buildinfo.Current()
	log.Printf("event=session_started version=%q commit=%q build_date=%q os=%s arch=%s go=%s files=%d ocr=%t", info.Version, info.Commit, info.Date, runtime.GOOS, runtime.GOARCH, runtime.Version(), len(options.Paths), ocr.Enabled())
	requests := make(chan openfiles.Request, 64)
	config, err := os.UserConfigDir()
	endpoint := filepath.Join(config, "Proview", "desktop")
	if options.dataDir != "" {
		endpoint, err = filepath.Join(options.dataDir, "desktop"), nil
	}
	if err == nil {
		server, forwarded, startErr := openfiles.Start(endpoint, options.Request, func(request openfiles.Request) error {
			select {
			case requests <- request:
				return nil
			default:
				return fmt.Errorf("desktop opening queue is full")
			}
		})
		if forwarded {
			log.Printf("event=desktop_request_forwarded files=%d", len(options.Paths))
			return
		}
		if startErr != nil {
			log.Printf("Desktop request forwarding unavailable: %v", startErr)
		}
		defer server.Close()
	}
	log.Println("event=application_started")

	editor := gui.NewGui()
	go func() {
		for request := range requests {
			editor.OpenFilesWithOptions(request.Paths, gui.OpenOptions{NewWindow: request.NewWindow, WorkDir: request.WorkDir})
		}
	}()
	editor.RunWithOptions(options.Paths, gui.OpenOptions{NewWindow: options.NewWindow, WorkDir: options.WorkDir})

	log.Println("event=session_stopped")
}
