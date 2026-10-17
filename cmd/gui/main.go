package main

import (
	"context"
	"emanuelelongo/proview/internal/applog"
	"emanuelelongo/proview/internal/buildinfo"
	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/internal/openfiles"
	"emanuelelongo/proview/pkg/gui"
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
	if len(os.Args) == 2 && os.Args[1] == "--check-ocr" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := ocr.SelfTest(ctx); err != nil {
			log.Println(err)
			os.Exit(1)
		}
		log.Println("Bundled OCR check passed")
		return
	}

	closeLogs, logErr := applog.Initialize()
	if logErr != nil {
		log.Printf("level=ERROR event=log_setup error=%q", logErr)
	}
	defer closeLogs()
	info := buildinfo.Current()
	log.Printf("event=session_started version=%q commit=%q build_date=%q os=%s arch=%s go=%s files=%d ocr=%t", info.Version, info.Commit, info.Date, runtime.GOOS, runtime.GOARCH, runtime.Version(), len(os.Args)-1, ocr.Enabled())
	requests := make(chan []string, 64)
	config, err := os.UserConfigDir()
	if err == nil {
		server, forwarded, startErr := openfiles.Start(filepath.Join(config, "Proview", "desktop"), os.Args[1:], func(paths []string) error {
			select {
			case requests <- paths:
				return nil
			default:
				return fmt.Errorf("desktop opening queue is full")
			}
		})
		if forwarded {
			log.Printf("event=desktop_request_forwarded files=%d", len(os.Args)-1)
			return
		}
		if startErr != nil {
			log.Printf("Desktop request forwarding unavailable: %v", startErr)
		}
		defer server.Close()
	}
	log.Println("event=application_started")

	gui := gui.NewGui()
	go func() {
		for paths := range requests {
			gui.OpenFiles(paths)
		}
	}()
	gui.RunWithFiles(os.Args[1:])

	log.Println("event=session_stopped")
}
