package gui

import (
	"testing"

	"emanuelelongo/proview/pkg/signatures"
	"emanuelelongo/proview/pkg/signatures/cloud"
	"fyne.io/fyne/v2"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestCloudDialogOffersGoogleDriveAndS3WithoutOAuthConfiguration(t *testing.T) {
	t.Setenv("PROVIEW_GOOGLE_CLIENT_ID", "")
	t.Setenv("PROVIEW_GOOGLE_CLIENT_SECRET", "")
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	store := &signatures.Store{Root: t.TempDir()}
	// Missing authorization must keep saved connection preferences intact.
	service := &signatureCloudService{app: app, store: store, coordinator: &cloud.Coordinator{Store: store}, credentials: &testCloudCredentials{}}
	app.Preferences().SetBool(driveConnectedKey, true)
	app.Preferences().SetString(driveFolderKey, "saved-folder")
	app.Preferences().SetString(cloudProviderKey, "google")
	service.restore()
	if service.coordinator.Status().Connected || !app.Preferences().Bool(driveConnectedKey) {
		t.Fatal("unauthorized provider restored or removed the saved Google connection")
	}
	g.signatureStore, g.signatureCloud = store, service
	g.window.Show()
	g.showSignatureCloud()
	visited := map[fyne.CanvasObject]bool{}
	found := false
	var connect *widget.Button
	var walk func(fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		if object == nil || visited[object] {
			return
		}
		visited[object] = true
		if button, ok := object.(*widget.Button); ok && button.Text == "Connect to Google Drive" {
			connect = button
		}
		if selectProvider, ok := object.(*widget.Select); ok {
			found = true
			if len(selectProvider.Options) != 2 || selectProvider.Options[0] != "Google Drive" || selectProvider.Options[1] != "S3" || selectProvider.Selected != "Google Drive" {
				t.Fatalf("unexpected provider options: %v, selected %q", selectProvider.Options, selectProvider.Selected)
			}
		}
		if container, ok := object.(*fyne.Container); ok {
			for _, child := range container.Objects {
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
	if !found {
		t.Fatal("missing provider selection")
	}
	if connect == nil || connect.Disabled() {
		t.Fatal("Google Drive connection is unavailable without embedded OAuth configuration")
	}
	g.closeSignatureDialogOnEscape()
}
