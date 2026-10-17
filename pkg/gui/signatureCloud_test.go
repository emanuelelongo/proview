package gui

import (
	"emanuelelongo/proview/pkg/signatures"
	"emanuelelongo/proview/pkg/signatures/cloud"
	"errors"
	"golang.org/x/oauth2"
	"image/png"
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
)

type testCloudCredentials struct{ deleted bool }

func (c *testCloudCredentials) Get() (*oauth2.Token, error) { return nil, cloud.ErrNotConnected }
func (c *testCloudCredentials) Set(*oauth2.Token) error     { return nil }
func (c *testCloudCredentials) Delete() error               { c.deleted = true; return nil }

func TestSignatureCloudIsSharedBetweenWindowsAndReleased(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	first, second := newGui(app), newGui(app)
	left, ok := first.signatureArchive()
	if !ok {
		t.Fatal("archive not available")
	}
	right, ok := second.signatureArchive()
	if !ok {
		t.Fatal("second archive not available")
	}
	if left != right || first.signatureCloud != second.signatureCloud {
		t.Fatal("windows do not share the archive and coordinator")
	}
	service := first.signatureCloud
	releaseSignatureService(app)
	signatureServices.Lock()
	remaining := signatureServices.apps[app]
	signatureServices.Unlock()
	if remaining != nil || service.coordinator.Status().Connected {
		t.Fatal("application service retained after shutdown")
	}
}
func TestCloudDisconnectKeepsSignaturesAndClearsPreferences(t *testing.T) {
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
	credentials := &testCloudCredentials{}
	service := &signatureCloudService{app: app, store: store, coordinator: &cloud.Coordinator{Store: store}, credentials: credentials}
	app.Preferences().SetBool(driveConnectedKey, true)
	app.Preferences().SetString(driveFolderKey, "remote-folder")
	stopped := false
	service.stopTokens = func() { stopped = true }
	if err = service.disconnect(); err != nil {
		t.Fatal(err)
	}
	if !stopped || !credentials.deleted || app.Preferences().Bool(driveConnectedKey) || app.Preferences().String(driveFolderKey) != "" {
		t.Fatal("disconnect retained credentials or connection settings")
	}
	entries, err := store.List()
	if err != nil || len(entries) != 1 {
		t.Fatal("disconnect removed local signatures")
	}
}
func TestCloudSettingsRemainAvailableWithoutOAuthAndCloseOnEscape(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	store := &signatures.Store{Root: t.TempDir()}
	service := &signatureCloudService{app: app, store: store, coordinator: &cloud.Coordinator{Store: store}, restored: true}
	g.signatureStore = store
	g.signatureCloud = service
	g.window.Resize(fyne.NewSize(1000, 700))
	g.window.Show()
	g.showSignatureCloud()
	if g.signatureDialog == nil || g.window.Canvas().Overlays().Top() == nil {
		t.Fatal("cloud settings did not open")
	}
	if path := os.Getenv("PROVIEW_CLOUD_PREVIEW"); path != "" {
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
		t.Fatal("Escape did not close cloud settings")
	}
	service.coordinator.Report(nil)
	service.setWarning(errors.New("reconnect required"))
	if !strings.Contains(service.message(service.coordinator.Status()), "Reconnect") {
		t.Fatal("disconnected error is not visible")
	}
}
