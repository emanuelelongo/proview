package main

import (
	"context"
	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/pkg/gui"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
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

	log.Println("proview started")

	gui := gui.NewGui()
	gui.RunWithFiles(os.Args[1:])

	log.Println("proview stopped")
}
