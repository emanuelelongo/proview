package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emanuelelongo/proview/pkg/signatures"
	"emanuelelongo/proview/pkg/signatures/cloud"
	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

type testS3Credentials struct {
	keys    cloud.S3Keys
	fail    bool
	deleted bool
}

func (c *testS3Credentials) Get() (cloud.S3Keys, error) {
	if c.keys.AccessKey == "" {
		return cloud.S3Keys{}, errors.New("not found")
	}
	return c.keys, nil
}
func (c *testS3Credentials) Set(keys cloud.S3Keys) error {
	if c.fail {
		return errors.New("keyring unavailable")
	}
	c.keys = keys
	c.deleted = false
	return nil
}
func (c *testS3Credentials) Delete() error { c.keys = cloud.S3Keys{}; c.deleted = true; return nil }
func testS3Service(app fyne.App, store *signatures.Store, credentials *testS3Credentials) *signatureCloudService {
	return &signatureCloudService{app: app, store: store, coordinator: &cloud.Coordinator{Store: store}, s3Credentials: credentials, credentials: &testCloudCredentials{}}
}
func waitS3Service(t *testing.T, service *signatureCloudService) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !service.coordinator.Status().Busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("S3 synchronization did not finish")
}
func TestS3ConnectionPersistenceRestoreAndDisconnect(t *testing.T) {
	for _, sessionOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(sessionOnly), func(t *testing.T) {
			app := fyneTest.NewApp()
			defer app.Quit()
			store := &signatures.Store{Root: t.TempDir()}
			asset, err := signatures.Import("signature.svg", []byte(`<svg viewBox="0 0 10 10"><path d="M1 1L9 9" stroke="black"/></svg>`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.Save(signatures.Entry{Asset: asset}); err != nil {
				t.Fatal(err)
			}
			var uploads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("list-type") == "2" {
					fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
					return
				}
				if r.Method == "PUT" {
					uploads.Add(1)
					return
				}
				w.WriteHeader(404)
				fmt.Fprint(w, `<Error><Code>NoSuchKey</Code></Error>`)
			}))
			defer server.Close()
			creds := &testS3Credentials{fail: sessionOnly}
			service := testS3Service(app, store, creds)
			app.Preferences().SetBool(driveConnectedKey, true)
			app.Preferences().SetString(driveFolderKey, "old-drive-folder")
			// Simulate a working Drive session without performing OAuth or network IO.
			service.restored = true
			service.coordinator.Connect(&cloud.GoogleDrive{FolderID: "old-drive-folder"})
			stopped := false
			service.stopTokens = func() { stopped = true }
			config := cloud.S3Config{Endpoint: server.URL, Bucket: "bucket", PathStyle: true}
			keys := cloud.S3Keys{AccessKey: "private-access", SecretKey: "private-secret"}
			if err = service.connectS3(context.Background(), config, keys); err != nil {
				t.Fatal(err)
			}
			waitS3Service(t, service)
			if status := service.coordinator.Status(); !status.Connected || status.Err != nil || status.Pending != 0 {
				t.Fatalf("sync status: %+v", status)
			}
			if uploads.Load() != 2 || !stopped {
				t.Fatal("initial local upload or Drive cleanup did not run")
			}
			if app.Preferences().Bool(driveConnectedKey) || app.Preferences().String(driveFolderKey) != "" {
				t.Fatal("S3 retained the old Drive connection")
			}
			prefs := app.Preferences().String(s3ConfigKey)
			for _, secret := range []string{keys.AccessKey, keys.SecretKey} {
				if strings.Contains(prefs, secret) {
					t.Fatal("credentials leaked into preferences")
				}
			}
			restored := testS3Service(app, store, creds)
			restored.restore()
			if restored.coordinator.Status().Connected == sessionOnly {
				t.Fatal("persistent/session-only connection restored incorrectly")
			}
			if sessionOnly && !strings.Contains(service.message(service.coordinator.Status()), "this session") {
				t.Fatal("missing session-only warning")
			}
			restored.coordinator.Disconnect()
			if err = service.disconnect(); err != nil {
				t.Fatal(err)
			}
			if !creds.deleted || app.Preferences().String(cloudProviderKey) != "" || app.Preferences().String(s3ConfigKey) != "" {
				t.Fatal("disconnect retained S3 settings or credentials")
			}
			entries, err := store.List()
			if err != nil || len(entries) != 1 || uploads.Load() != 2 {
				t.Fatal("disconnect changed the local or remote archive")
			}
		})
	}
}

func TestS3FailedReplacementKeepsWorkingConnection(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	store := &signatures.Store{Root: t.TempDir()}
	creds := &testS3Credentials{keys: cloud.S3Keys{AccessKey: "old-access", SecretKey: "old-secret"}}
	service := testS3Service(app, store, creds)
	service.restored = true
	service.activeProvider = "Google Drive"
	service.coordinator.Connect(&cloud.GoogleDrive{FolderID: "old-folder"})
	app.Preferences().SetBool(driveConnectedKey, true)
	app.Preferences().SetString(driveFolderKey, "old-folder")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
	}))
	defer server.Close()
	err := service.connectS3(context.Background(), cloud.S3Config{Endpoint: server.URL, Bucket: "bucket", PathStyle: true}, cloud.S3Keys{AccessKey: "new", SecretKey: "new"})
	if err == nil {
		t.Fatal("denied replacement succeeded")
	}
	if !service.coordinator.Status().Connected || !app.Preferences().Bool(driveConnectedKey) || app.Preferences().String(driveFolderKey) != "old-folder" || creds.keys.AccessKey != "old-access" {
		t.Fatal("failed replacement changed the working connection")
	}
	service.coordinator.Disconnect()
}

func TestS3RestoreWithoutCredentialsReportsReconnect(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	store := &signatures.Store{Root: t.TempDir()}
	service := testS3Service(app, store, &testS3Credentials{})
	config, _ := json.Marshal(cloud.S3Config{Endpoint: "https://s3.example.com", Bucket: "bucket"})
	app.Preferences().SetString(cloudProviderKey, "s3")
	app.Preferences().SetString(s3ConfigKey, string(config))
	service.restore()
	if service.coordinator.Status().Connected || !strings.Contains(service.message(service.coordinator.Status()), "Reconnect to S3") {
		t.Fatal("missing S3 credentials were not reported")
	}
}

func TestS3SettingsAvailableWithoutGoogleConfiguration(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	store := &signatures.Store{Root: t.TempDir()}
	service := testS3Service(app, store, &testS3Credentials{})
	service.restored = true
	g.signatureStore = store
	g.signatureCloud = service
	g.window.Resize(fyne.NewSize(1000, 700))
	g.window.Show()
	g.showSignatureCloud()
	var selection *widget.Select
	var connect *widget.Button
	visited := map[fyne.CanvasObject]bool{}
	var walk func(fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		if object == nil || visited[object] {
			return
		}
		visited[object] = true
		if candidate, ok := object.(*widget.Select); ok {
			selection = candidate
		}
		if candidate, ok := object.(*widget.Button); ok && strings.HasPrefix(candidate.Text, "Connect to") {
			connect = candidate
		}
		if c, ok := object.(*fyne.Container); ok {
			for _, child := range c.Objects {
				walk(child)
			}
		}
		if w, ok := object.(fyne.Widget); ok {
			for _, child := range fyneTest.WidgetRenderer(w).Objects() {
				walk(child)
			}
		}
	}
	walk(g.window.Canvas().Overlays().Top())
	if selection == nil || connect == nil {
		t.Fatal("missing provider controls")
	}
	selection.SetSelected("S3")
	if connect.Disabled() || connect.Text != "Connect to S3" {
		t.Fatal("S3 connection is unavailable")
	}
	if path := os.Getenv("PROVIEW_S3_PREVIEW"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err = png.Encode(file, g.window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
	if !g.closeSignatureDialogOnEscape() {
		t.Fatal("S3 settings did not close")
	}
}
