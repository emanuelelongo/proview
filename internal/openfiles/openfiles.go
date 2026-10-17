// Package openfiles forwards desktop file-opening requests to a running instance.
package openfiles

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type endpoint struct{ Address, Token string }
type request struct {
	Token string
	Paths []string
}

type Server struct {
	listener     net.Listener
	lock         *os.File
	endpointPath string
	once         sync.Once
	workers      sync.WaitGroup
}

// Start uses a private endpoint and an OS lock; crashed processes leave no live lock.
// A second launch with paths forwards them and returns forwarded=true. A launch
// without paths may still create a separate empty window/process.
func Start(directory string, paths []string, receive func([]string) error) (*Server, bool, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, false, err
	}
	lock, err := os.OpenFile(filepath.Join(directory, "open-files.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	if err = tryLock(lock); err != nil {
		_ = lock.Close()
		if len(paths) == 0 {
			return nil, false, nil
		}
		absolute := make([]string, len(paths))
		for i, path := range paths {
			absolute[i], err = filepath.Abs(path)
			if err != nil {
				return nil, false, err
			}
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			err = forward(filepath.Join(directory, "open-files.json"), absolute)
			if err == nil {
				return nil, true, nil
			}
			if time.Now().After(deadline) {
				return nil, false, fmt.Errorf("forward opening request: %w", err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		unlock(lock)
		_ = lock.Close()
		return nil, false, err
	}
	server := &Server{listener: listener, lock: lock, endpointPath: filepath.Join(directory, "open-files.json")}
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		server.Close()
		return nil, false, err
	}
	info := endpoint{Address: listener.Addr().String(), Token: hex.EncodeToString(token)}
	data, _ := json.Marshal(info)
	// The OS lock is held throughout publication; readers retry a partial write.
	if err = os.WriteFile(server.endpointPath, data, 0600); err != nil {
		server.Close()
		return nil, false, err
	}
	server.workers.Add(1)
	go func() {
		defer server.workers.Done()
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			// Bounded sequential handling avoids unbounded local connection workers.
			server.handle(connection, info.Token, receive)
		}
	}()
	return server, false, nil
}

func (s *Server) handle(connection net.Conn, token string, receive func([]string) error) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	var input request
	if err := json.NewDecoder(io.LimitReader(connection, 1<<20)).Decode(&input); err != nil {
		return
	}
	if subtle.ConstantTimeCompare([]byte(input.Token), []byte(token)) != 1 || len(input.Paths) == 0 || len(input.Paths) > 256 {
		return
	}
	for _, path := range input.Paths {
		if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
			return
		}
	}
	if err := receive(input.Paths); err != nil {
		return
	}
	_, _ = io.WriteString(connection, "OK\n")
}

func forward(path string, paths []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var info endpoint
	if err = json.Unmarshal(data, &info); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(info.Address)
	if err != nil || host != "127.0.0.1" || len(info.Token) != 64 {
		return errors.New("invalid local opening endpoint")
	}
	connection, err := net.DialTimeout("tcp4", info.Address, 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	if err = json.NewEncoder(connection).Encode(request{Token: info.Token, Paths: paths}); err != nil {
		return err
	}
	reply := make([]byte, 3)
	if _, err = io.ReadFull(connection, reply); err != nil {
		return err
	}
	if string(reply) != "OK\n" {
		return errors.New("opening request was not accepted")
	}
	return nil
}

func (s *Server) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		_ = s.listener.Close()
		s.workers.Wait()
		_ = os.Remove(s.endpointPath)
		unlock(s.lock)
		_ = s.lock.Close()
	})
}
