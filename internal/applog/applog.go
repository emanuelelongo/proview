// Package applog provides bounded, batched local diagnostic logs.
package applog

import (
	"bytes"
	"emanuelelongo/proview/internal/appprofile"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxSize       = 2 << 20
	backups       = 3
	batchSize     = 64 << 10
	maxRecord     = 8 << 10
	flushInterval = 2 * time.Second
)

type record struct {
	data    []byte
	flushed chan error
}
type Logger struct {
	closeOnce sync.Once
	closeErr  error
	mu        sync.Mutex
	closed    bool
	queue     chan record
	done      chan struct{}
	dropped   atomic.Uint64
	lock      *os.File
	directory string
	limit     int64
}

var active atomic.Pointer[Logger]

func Directory() (string, error) {
	if appprofile.Root != "" {
		return filepath.Join(appprofile.Root, "logs"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Logs", "Proview"), nil
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "Proview", "logs"), nil
	default:
		base := os.Getenv("XDG_STATE_HOME")
		if !filepath.IsAbs(base) {
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "proview", "logs"), nil
	}
}

// Initialize keeps terminal output and additionally captures standard Go/Fyne
// logs and unrecovered Go crash reports. It is called once by the executable.
func Initialize() (func(), error) {
	directory, err := Directory()
	if err != nil {
		return func() {}, err
	}
	sink, err := newLogger(directory, maxSize)
	if err != nil {
		return func() {}, err
	}
	previous, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC)
	log.SetPrefix(fmt.Sprintf("pid=%d ", os.Getpid()))
	log.SetOutput(terminalAndFile{previous, sink})
	active.Store(sink)
	// The runtime writes crashes directly, without relying on a worker or recover.
	crashErr := sink.enableCrashOutput()
	if crashErr != nil {
		log.Printf("level=WARN event=crash_log_setup error=%q", crashErr)
	}
	return func() {
		log.SetOutput(previous)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
		active.Store(nil)
		// Keep the runtime crash descriptor alive through deferred main cleanup.
		if err := sink.Close(); err != nil {
			_, _ = fmt.Fprintf(previous, "Log shutdown failed: %v\n", err)
		}
	}, nil
}

type terminalAndFile struct {
	terminal io.Writer
	file     *Logger
}

func (w terminalAndFile) Write(data []byte) (int, error) {
	_, _ = w.terminal.Write(data) // Windows GUI processes may have no stderr handle.
	return w.file.Write(data)
}

func newLogger(directory string, limit int64) (*Logger, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(directory, "logs.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	l := &Logger{queue: make(chan record, 256), done: make(chan struct{}), lock: lock, directory: directory, limit: limit}
	if err = l.append(nil); err != nil {
		_ = lock.Close()
		return nil, err
	}
	go l.run()
	return l, nil
}

// Write performs no file I/O and never waits for the disk worker. Under overload
// records are dropped and counted rather than blocking the UI or growing memory.
func (l *Logger) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, os.ErrClosed
	}
	length := len(data)
	if len(data) > maxRecord {
		data = append(append([]byte(nil), data[:maxRecord]...), []byte(" [truncated]\n")...)
	} else {
		data = bytes.Clone(data)
	}
	select {
	case l.queue <- record{data: data}:
	default:
		l.dropped.Add(1)
	}
	return length, nil
}

func Flush() error {
	if l := active.Load(); l != nil {
		return l.Flush()
	}
	return nil
}
func (l *Logger) Flush() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return os.ErrClosed
	}
	done := make(chan error, 1)
	l.queue <- record{flushed: done}
	l.mu.Unlock()
	return <-done
}
func (l *Logger) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		close(l.queue)
		l.mu.Unlock()
		<-l.done
		l.closeErr = errors.Join(l.closeErr, l.lock.Close())
	})
	return l.closeErr
}

func (l *Logger) run() {
	defer close(l.done)
	defer func() {}()
	timer := time.NewTicker(flushInterval)
	defer timer.Stop()
	var buffer bytes.Buffer
	var lastErr error
	reported := false
	flush := func() {
		if dropped := l.dropped.Swap(0); dropped > 0 {
			fmt.Fprintf(&buffer, "%s level=WARN event=log_overflow dropped=%d\n", time.Now().UTC().Format(time.RFC3339Nano), dropped)
		}
		if buffer.Len() == 0 {
			return
		}
		lastErr = l.append(buffer.Bytes())
		buffer.Reset()
		if lastErr != nil && !reported {
			fmt.Fprintf(os.Stderr, "Diagnostic log write failed: %v\n", lastErr)
			reported = true
		}
		if lastErr == nil {
			reported = false
		}
	}
	for {
		select {
		case item, ok := <-l.queue:
			if !ok {
				flush()
				l.closeErr = lastErr
				return
			}
			buffer.Write(item.data)
			if item.flushed != nil || buffer.Len() >= batchSize {
				flush()
			}
			if item.flushed != nil {
				item.flushed <- lastErr
			}
		case <-timer.C:
			flush()
		}
	}
}

func (l *Logger) append(data []byte) error {
	if err := lockFile(l.lock); err != nil {
		return err
	}
	defer unlock(l.lock)
	path := filepath.Join(l.directory, "proview.log")
	if err := rotate(path, int64(len(data)), l.limit); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return errors.Join(err, file.Close())
}

func rotate(path string, incoming, limit int64) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() == 0 || info.Size()+incoming <= limit {
		return nil
	}
	if err := os.Remove(fmt.Sprintf("%s.%d", path, backups)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for index := backups - 1; index >= 0; index-- {
		source := path
		if index > 0 {
			source = fmt.Sprintf("%s.%d", path, index)
		}
		if err := os.Rename(source, fmt.Sprintf("%s.%d", path, index+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (l *Logger) enableCrashOutput() error {
	if err := lockFile(l.lock); err != nil {
		return err
	}
	defer unlock(l.lock)
	path := filepath.Join(l.directory, "crash.log")
	if err := rotate(path, 0, maxSize); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return debug.SetCrashOutput(file, debug.CrashOptions{})
}
