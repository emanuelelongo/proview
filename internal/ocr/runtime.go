package ocr

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"emanuelelongo/proview/internal/textselect"
)

// Packaged builds enable OCR without environment variables or external installs.
func Enabled() bool {
	return os.Getenv("PROVIEW_OCR") != "0" && (len(runtimePayload) > 0 || os.Getenv("PROVIEW_OCR") == "1")
}
func NewEngine() Engine {
	if path := os.Getenv("PROVIEW_TESSERACT_PATH"); path != "" {
		return &Tesseract{Path: path}
	}
	if len(runtimePayload) == 0 {
		return &Tesseract{}
	}
	return &packagedEngine{}
}

type packagedEngine struct{}

var runtimeState struct {
	sync.Mutex
	engine *Tesseract
}

func (*packagedEngine) Recognize(ctx context.Context, img image.Image) (textselect.Layout, error) {
	runtimeState.Lock()
	engine := runtimeState.engine
	if engine == nil {
		cache, err := os.UserCacheDir()
		if err == nil {
			var directory string
			directory, err = unpackRuntime(ctx, runtimePayload, filepath.Join(cache, "Proview", "ocr"), runtime.GOOS, runtime.GOARCH)
			if err == nil {
				engine = &Tesseract{Path: filepath.Join(directory, "bin", helperName()), DataDir: filepath.Join(directory, "tessdata")}
				runtimeState.engine = engine
			}
		}
		if err != nil {
			runtimeState.Unlock()
			if ctx.Err() != nil {
				return textselect.Layout{}, ctx.Err()
			}
			return textselect.Layout{}, &SetupError{fmt.Errorf("prepare bundled OCR: %w", err)}
		}
	}
	runtimeState.Unlock()
	return engine.Recognize(ctx, img)
}
func helperName() string {
	if runtime.GOOS == "windows" {
		return "tesseract.exe"
	}
	return "tesseract"
}

type runtimeManifest struct {
	OS         string            `json:"os"`
	Arch       string            `json:"arch"`
	Executable string            `json:"executable"`
	Files      map[string]string `json:"files"`
}

// The cache is content-addressed. Staging and atomic publication allow multiple
// app processes to start together without ever running a partially written helper.
func unpackRuntime(ctx context.Context, payload []byte, cache, goos, arch string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return "", err
	}
	entries := map[string]*zip.File{}
	for _, entry := range archive.File {
		name := entry.Name
		if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") || entry.Mode()&os.ModeType != 0 || entry.UncompressedSize64 > 64*1024*1024 || entries[name] != nil {
			return "", errors.New("invalid OCR archive entry")
		}
		entries[name] = entry
	}
	manifestFile := entries["manifest.json"]
	if manifestFile == nil || manifestFile.UncompressedSize64 > 1024*1024 {
		return "", errors.New("missing OCR manifest")
	}
	reader, err := manifestFile.Open()
	if err != nil {
		return "", err
	}
	var manifest runtimeManifest
	manifestData, readErr := io.ReadAll(reader)
	reader.Close()
	err = readErr
	if err == nil {
		err = json.Unmarshal(manifestData, &manifest)
	}
	if err != nil {
		return "", err
	}
	executable := "bin/tesseract"
	if goos == "windows" {
		executable += ".exe"
	}
	if manifest.OS != goos || manifest.Arch != arch || manifest.Executable != executable || len(manifest.Files)+1 != len(entries) {
		return "", errors.New("OCR runtime target or manifest mismatch")
	}
	for _, name := range []string{executable, "tessdata/eng.traineddata", "tessdata/ita.traineddata"} {
		if manifest.Files[name] == "" {
			return "", errors.New("incomplete OCR runtime")
		}
	}
	for name, digest := range manifest.Files {
		decoded, e := hex.DecodeString(digest)
		if e != nil || len(decoded) != sha256.Size || entries[name] == nil {
			return "", errors.New("invalid OCR manifest digest")
		}
	}
	checksum := sha256.Sum256(payload)
	destination := filepath.Join(cache, hex.EncodeToString(checksum[:]))
	if validRuntime(ctx, destination, manifest) == nil {
		return destination, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err = os.MkdirAll(cache, 0700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(cache, ".extract-")
	if err != nil {
		return "", err
	}
	published := false
	defer func() {
		if !published {
			os.RemoveAll(stage)
		}
	}()
	for name, digest := range manifest.Files {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		target := filepath.Join(stage, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return "", err
		}
		mode := os.FileMode(0600)
		if name == executable {
			mode = 0700
		}
		file, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if e != nil {
			return "", e
		}
		source, e := entries[name].Open()
		if e != nil {
			file.Close()
			return "", e
		}
		hash := sha256.New()
		_, e = io.Copy(io.MultiWriter(file, hash), source)
		source.Close()
		closeErr := file.Close()
		if e != nil {
			return "", e
		}
		if closeErr != nil {
			return "", closeErr
		}
		if hex.EncodeToString(hash.Sum(nil)) != digest {
			return "", errors.New("OCR runtime checksum mismatch")
		}
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(stage, "manifest.json"), manifestData, 0600); err != nil {
		return "", err
	}
	if err = os.Rename(stage, destination); err == nil {
		published = true
		return destination, nil
	}
	// Another process may have won publication. A damaged/locked older directory
	// is left alone; use this verified private copy instead of deleting active files.
	if validRuntime(ctx, destination, manifest) == nil {
		return destination, nil
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	published = true
	return stage, nil
}
func validRuntime(ctx context.Context, directory string, manifest runtimeManifest) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid OCR cache directory")
	}
	for name, digest := range manifest.Files {
		if err = ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(directory, filepath.FromSlash(name))
		// Refuse symlinked parent directories as well as symlinked files.
		parent := filepath.Dir(target)
		for parent != directory {
			info, err = os.Lstat(parent)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("invalid OCR cache parent")
			}
			parent = filepath.Dir(parent)
		}
		info, err = os.Lstat(target)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 64*1024*1024 || (runtime.GOOS != "windows" && name == manifest.Executable && info.Mode().Perm()&0111 == 0) {
			return errors.New("invalid OCR cache file")
		}
		file, e := os.Open(target)
		if e != nil {
			return e
		}
		hash := sha256.New()
		_, e = io.Copy(hash, file)
		file.Close()
		if e != nil {
			return e
		}
		if hex.EncodeToString(hash.Sum(nil)) != digest {
			return errors.New("damaged OCR cache")
		}
	}
	return nil
}
