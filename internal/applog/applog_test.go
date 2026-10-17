package applog

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFlushCloseAndRotationRetainBoundedRecentRecords(t *testing.T) {
	directory := t.TempDir()
	logger, err := newLogger(directory, 128)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := fmt.Fprintf(logger, "event=record index=%02d padding=abcdefghijklmnop\n", i); err != nil {
			t.Fatal(err)
		}
		if err := logger.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(directory, "proview.log*"))
	if err != nil || len(files) != backups+1 {
		t.Fatalf("files=%v error=%v", files, err)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 128 || len(data) == 0 {
			t.Fatalf("unexpected rotation size: %s %d", path, len(data))
		}
	}
	data, _ := os.ReadFile(filepath.Join(directory, "proview.log"))
	if !bytes.Contains(data, []byte("index=19")) {
		t.Fatal("latest record was lost")
	}
	if _, err := logger.Write([]byte("after close")); err != os.ErrClosed {
		t.Fatalf("closed write: %v", err)
	}
}

func TestConcurrentProcessesShareRotationWithoutLosingRecords(t *testing.T) {
	// Independent lock handles exercise the same path as separate processes.
	directory := t.TempDir()
	var workers sync.WaitGroup
	for i := 0; i < 3; i++ {
		logger, err := newLogger(directory, maxSize)
		if err != nil {
			t.Fatal(err)
		}
		workers.Go(func() {
			for n := 0; n < 40; n++ {
				fmt.Fprintf(logger, "worker=%d record=%d\n", i, n)
			}
			if err := logger.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	data, err := os.ReadFile(filepath.Join(directory, "proview.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(data, []byte("\n")); got != 120 {
		t.Fatalf("records=%d want=120", got)
	}
}

func TestSlowDiskDoesNotBlockWritesAndOverloadIsReported(t *testing.T) {
	directory := t.TempDir()
	logger, err := newLogger(directory, maxSize)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := os.OpenFile(filepath.Join(directory, "logs.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	if err := lockFile(blocker); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			_, _ = logger.Write(bytes.Repeat([]byte("x"), 512))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		unlock(blocker)
		t.Fatal("log writes waited for disk")
	}
	if logger.dropped.Load() == 0 {
		unlock(blocker)
		t.Fatal("unbounded queue did not drop overload")
	}
	unlock(blocker)
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(directory, "proview.log"))
	if !bytes.Contains(data, []byte("event=log_overflow")) {
		t.Fatal("overload was silent")
	}
}

func TestCrashReport(t *testing.T) {
	if directory := os.Getenv("PROVIEW_TEST_CRASH_LOG"); directory != "" {
		logger, err := newLogger(directory, maxSize)
		if err != nil {
			panic(err)
		}
		if err := logger.enableCrashOutput(); err != nil {
			panic(err)
		}
		panic("diagnostic crash probe")
	}
	directory := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestCrashReport$")
	command.Env = append(os.Environ(), "PROVIEW_TEST_CRASH_LOG="+directory)
	if err := command.Run(); err == nil {
		t.Fatal("crash subprocess unexpectedly succeeded")
	}
	data, err := os.ReadFile(filepath.Join(directory, "crash.log"))
	if err != nil || !strings.Contains(string(data), "diagnostic crash probe") || !strings.Contains(string(data), "goroutine") {
		t.Fatalf("missing crash report: %s %v", data, err)
	}
}

func TestRecordSizeIsBounded(t *testing.T) {
	directory := t.TempDir()
	logger, err := newLogger(directory, maxSize)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Repeat([]byte("x"), maxRecord*10)
	if n, err := logger.Write(original); n != len(original) || err != nil {
		t.Fatalf("write: %d %v", n, err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(directory, "proview.log"))
	if len(data) > maxRecord+32 || !bytes.Contains(data, []byte("[truncated]")) {
		t.Fatal("oversized record was not truncated")
	}
}
