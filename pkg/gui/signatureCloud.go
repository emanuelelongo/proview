package gui

import (
	"context"
	"emanuelelongo/proview/pkg/signatures"
	"emanuelelongo/proview/pkg/signatures/cloud"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const cloudProviderKey = "signatures.cloud.provider"
const s3ConfigKey = "signatures.s3.config"

const driveConnectedKey = "signatures.google.connected"
const driveFolderNameKey = "signatures.google.directory"
const driveFolderKey = "signatures.google.folder"
const driveFolderExplicitKey = "signatures.google.folderExplicit"

// One store and coordinator per application, shared by all document windows.
var signatureServices = struct {
	sync.Mutex
	apps map[fyne.App]*signatureCloudService
}{apps: map[fyne.App]*signatureCloudService{}}

type signatureCloudService struct {
	app            fyne.App
	store          *signatures.Store
	coordinator    *cloud.Coordinator
	credentials    cloud.Credentials
	s3Credentials  cloud.S3Credentials
	activeProvider string
	mu             sync.Mutex
	lifecycle      sync.Mutex
	authCancel     context.CancelFunc
	restored       bool
	closed         bool
	warning        error
	tokenWarning   func() error
	stopTokens     func()
	lastDiagnostic string
}

func signatureService(app fyne.App) (*signatureCloudService, error) {
	signatureServices.Lock()
	defer signatureServices.Unlock()
	if service := signatureServices.apps[app]; service != nil {
		return service, nil
	}
	store, err := signatures.DefaultStore()
	if err != nil {
		return nil, err
	}
	service := &signatureCloudService{app: app, store: store, credentials: cloud.SystemCredentials{}, s3Credentials: cloud.SystemS3Credentials{}}
	service.coordinator = &cloud.Coordinator{Store: store}
	service.coordinator.Subscribe(func(status cloud.Status) { service.logCloudFailure(status.Err) })
	signatureServices.apps[app] = service
	return service, nil
}
func (s *signatureCloudService) restore() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.restored || s.closed {
		s.mu.Unlock()
		return
	}
	s.restored = true
	s.mu.Unlock()
	if s.app.Preferences().String(cloudProviderKey) == "s3" {
		var config cloud.S3Config
		if err := json.Unmarshal([]byte(s.app.Preferences().String(s3ConfigKey)), &config); err != nil {
			s.setWarning(errors.New("S3 settings are unavailable. Reconnect to S3"))
			return
		}
		keys, err := s.s3Credentials.Get()
		if err != nil {
			s.setWarning(errors.New("S3 credentials are unavailable. Reconnect to S3"))
			return
		}
		provider, err := cloud.NewS3(config, keys)
		if err != nil {
			s.setWarning(err)
			return
		}
		s.mu.Lock()
		s.activeProvider = "S3"
		s.mu.Unlock()
		s.coordinator.Connect(provider)
		return
	}
	if !s.app.Preferences().Bool(driveConnectedKey) {
		return
	}
	token, err := s.credentials.Get()
	if err != nil {
		s.setWarning(errors.New("Google authorization is unavailable. Reconnect to Google Drive"))
		return
	}
	client, warning, stop := cloud.GoogleClient(token, s.credentials, true)
	s.mu.Lock()
	s.tokenWarning = warning
	s.stopTokens = stop
	s.activeProvider = "Google Drive"
	s.mu.Unlock()
	s.coordinator.Connect(s.provider(client))
}
func (s *signatureCloudService) setWarning(err error) {
	s.logCloudFailure(err)
	s.mu.Lock()
	s.warning = err
	s.mu.Unlock()
	s.coordinator.Report(err)
}

func (s *signatureCloudService) logCloudFailure(err error) {
	detail := diagnosticError(err)
	s.mu.Lock()
	if detail == s.lastDiagnostic {
		s.mu.Unlock()
		return
	}
	s.lastDiagnostic = detail
	s.mu.Unlock()
	if err != nil {
		log.Printf("event=operation_failed level=ERROR operation=signature_cloud service=%p error=%q", s, detail)
	}
}

func cloudErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	reason := strings.ToLower(err.Error())
	switch {
	case strings.Contains(reason, "this session"):
		return "This connection lasts only for this session. Reconnect after restarting Proview."
	case strings.Contains(reason, "authorization"), strings.Contains(reason, "reconnect"), strings.Contains(reason, "not connected"):
		if strings.Contains(reason, "s3") {
			return "Reconnect to S3 to sync signatures."
		}
		if strings.Contains(reason, "google") {
			return "Reconnect to Google Drive to sync signatures."
		}
		return "Reconnect to your cloud provider to sync signatures."
	case strings.Contains(reason, "configuration is missing"):
		return "Cloud connection is not configured in this build. See the cloud sync setup documentation."
	default:
		return operationErrorMessage("signature_cloud", err)
	}
}

func (s *signatureCloudService) provider(client *http.Client) *cloud.GoogleDrive {
	return &cloud.GoogleDrive{Client: client, FolderName: s.app.Preferences().String(driveFolderNameKey), FolderID: s.app.Preferences().String(driveFolderKey), ExplicitFolder: s.app.Preferences().Bool(driveFolderExplicitKey), OnFolder: func(id string) { s.app.Preferences().SetString(driveFolderKey, id) }}
}
func (s *signatureCloudService) message(status cloud.Status) string {
	s.logCloudFailure(status.Err)
	s.mu.Lock()
	warning := s.warning
	providerName := s.activeProvider
	if providerName == "" {
		providerName = "Google Drive"
	}
	tokenWarning := s.tokenWarning
	s.mu.Unlock()
	if tokenWarning != nil {
		if err := tokenWarning(); err != nil {
			warning = err
		}
	}
	if !status.Connected {
		if warning != nil {
			return cloudErrorMessage(warning)
		}
		return "Cloud sync is off"
	}
	text := "Connected to " + providerName
	if status.Busy {
		text = "Syncing signatures"
	} else if !status.LastSync.IsZero() {
		text = providerName + " · Last sync " + status.LastSync.Local().Format("15:04:05")
	}
	if status.Pending > 0 {
		text += fmt.Sprintf(" · %d pending", status.Pending)
	}
	if status.Err != nil {
		text += "\n" + cloudErrorMessage(status.Err)
	}
	if warning != nil {
		text += "\n" + cloudErrorMessage(warning)
	}
	return text
}
func (s *signatureCloudService) open() {
	go func() {
		s.restore()
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if !closed {
			s.coordinator.Open(context.Background())
		}
	}()
}
func (s *signatureCloudService) push() { s.coordinator.Push(context.Background()) }

func (s *signatureCloudService) connect(ctx context.Context, open func(*url.URL) error, folderName string) error {
	folderName, err := cloud.NormalizeDriveFolderName(folderName)
	if err != nil {
		return err
	}
	s.restore()
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.authCancel = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.authCancel = nil; s.mu.Unlock() }()
	auth, err := cloud.Authorize(ctx, open, false)
	if err != nil {
		return err
	}
	if err = s.store.EnableSync(); err != nil {
		return err
	}
	// Validate and prepare the new destination before replacing a working login.
	client, _, stopTemporary := cloud.GoogleClient(auth.Token, s.credentials, false)
	defer stopTemporary()
	provider := s.provider(client)
	provider.FolderName = folderName
	provider.FolderID = ""
	provider.ExplicitFolder = false
	provider.OnFolder = nil
	if err = provider.Prepare(ctx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	s.coordinator.Disconnect()
	s.mu.Lock()
	stopPrevious := s.stopTokens
	s.stopTokens = nil
	s.mu.Unlock()
	if stopPrevious != nil {
		stopPrevious()
	}
	persistent := true
	var warningError error
	if err = s.credentials.Set(auth.Token); err != nil {
		persistent = false
		_ = s.credentials.Delete()
		warningError = errors.New("The system keyring is unavailable. This connection lasts for this session")
	}
	client, warning, stop := cloud.GoogleClient(auth.Token, s.credentials, persistent)
	provider.Client = client
	provider.OnFolder = func(id string) { s.app.Preferences().SetString(driveFolderKey, id) }
	s.app.Preferences().SetString(driveFolderKey, provider.FolderID)
	s.app.Preferences().SetString(driveFolderNameKey, provider.FolderName)
	s.app.Preferences().SetBool(driveFolderExplicitKey, false)
	s.app.Preferences().SetBool(driveConnectedKey, persistent)
	s.app.Preferences().RemoveValue(s3ConfigKey)
	s.app.Preferences().RemoveValue(cloudProviderKey)
	if persistent {
		s.app.Preferences().SetString(cloudProviderKey, "google")
	}
	if s.s3Credentials != nil {
		if err := s.s3Credentials.Delete(); err != nil {
			warningError = errors.Join(warningError, errors.New("The saved S3 credentials could not be removed from the system keyring"))
		}
	}
	s.mu.Lock()
	s.warning = warningError
	s.activeProvider = "Google Drive"
	s.tokenWarning = warning
	s.stopTokens = stop
	s.restored = true
	s.mu.Unlock()
	s.coordinator.Connect(provider)
	s.coordinator.Open(context.Background())
	return nil
}
func (s *signatureCloudService) disconnect() error {
	s.mu.Lock()
	cancel := s.authCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.coordinator.Disconnect()
	s.mu.Lock()
	stop := s.stopTokens
	s.stopTokens = nil
	s.tokenWarning = nil
	s.warning = nil
	s.activeProvider = ""
	s.restored = true
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	s.app.Preferences().SetBool(driveConnectedKey, false)
	s.app.Preferences().RemoveValue(driveFolderKey)
	s.app.Preferences().RemoveValue(driveFolderNameKey)
	s.app.Preferences().RemoveValue(driveFolderExplicitKey)
	s.app.Preferences().RemoveValue(cloudProviderKey)
	s.app.Preferences().RemoveValue(s3ConfigKey)
	var failures []error
	if s.credentials != nil {
		if err := s.credentials.Delete(); err != nil {
			failures = append(failures, errors.New("The saved Google authorization could not be removed from the system keyring"))
		}
	}
	if s.s3Credentials != nil {
		if err := s.s3Credentials.Delete(); err != nil {
			failures = append(failures, errors.New("The saved S3 credentials could not be removed from the system keyring"))
		}
	}
	if err := errors.Join(failures...); err != nil {
		s.setWarning(err)
		return err
	}
	return nil
}

func (g *Gui) showSignatureCloud() {
	if _, ok := g.signatureArchive(); !ok {
		return
	}
	service := g.signatureCloud
	if service == nil {
		return
	}
	service.open()
	statusLabel := widget.NewLabel(service.message(service.coordinator.Status()))
	statusLabel.Wrapping = fyne.TextWrapWord
	connect := widget.NewButton("Connect to Google Drive", nil)
	disconnect := widget.NewButton("Disconnect", nil)
	driveDirectory := widget.NewEntry()
	directoryName, _ := cloud.NormalizeDriveFolderName(g.app.Preferences().String(driveFolderNameKey))
	driveDirectory.SetText(directoryName)
	driveForm := widget.NewForm(widget.NewFormItem("Directory", driveDirectory))
	endpoint, region, bucket, prefix := widget.NewEntry(), widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	endpoint.SetPlaceHolder("https://s3.example.com")
	region.SetText("us-east-1")
	prefix.SetText("proview/signatures")
	access, secret := widget.NewPasswordEntry(), widget.NewPasswordEntry()
	pathStyle := widget.NewCheck("Use path-style addressing", nil)
	pathStyle.SetChecked(true)
	var saved cloud.S3Config
	if json.Unmarshal([]byte(g.app.Preferences().String(s3ConfigKey)), &saved) == nil {
		endpoint.SetText(saved.Endpoint)
		region.SetText(saved.Region)
		bucket.SetText(saved.Bucket)
		prefix.SetText(saved.Prefix)
		pathStyle.SetChecked(saved.PathStyle)
	}
	s3Form := container.NewVBox(widget.NewForm(
		widget.NewFormItem("Endpoint", endpoint), widget.NewFormItem("Region", region),
		widget.NewFormItem("Bucket", bucket), widget.NewFormItem("Prefix", prefix),
		widget.NewFormItem("Access key", access), widget.NewFormItem("Secret key", secret),
	), pathStyle)
	info := widget.NewLabel("")
	info.Wrapping = fyne.TextWrapWord
	var authCancel context.CancelFunc
	disconnecting := false
	closed := false
	provider := widget.NewSelect([]string{"Google Drive", "S3"}, nil)
	var content *fyne.Container
	updateConnect := func(status cloud.Status) {
		locked := status.Connected || authCancel != nil || disconnecting
		for _, field := range []*widget.Entry{driveDirectory, endpoint, region, bucket, prefix} {
			if locked {
				field.Disable()
			} else {
				field.Enable()
			}
		}
		if locked {
			pathStyle.Disable()
			provider.Disable()
		} else {
			pathStyle.Enable()
			provider.Enable()
		}
		if provider.Selected == "S3" {
			connect.SetText("Connect to S3")
		} else {
			connect.SetText("Connect to Google Drive")
		}
		if status.Connected {
			service.mu.Lock()
			active := service.activeProvider
			service.mu.Unlock()
			if active == "S3" {
				connect.SetText("Connected to S3")
			} else {
				connect.SetText("Connected to Google Drive")
				name, _ := cloud.NormalizeDriveFolderName(g.app.Preferences().String(driveFolderNameKey))
				if driveDirectory.Text != name {
					driveDirectory.SetText(name)
				}
			}
		}
		if locked {
			connect.Disable()
		} else {
			connect.Enable()
		}
	}
	updateProvider := func(name string) {
		if name == "S3" {
			s3Form.Show()
			driveForm.Hide()
			info.Show()
			info.SetText("Use an existing bucket. Credentials are saved in the system keyring. Only signatures are synchronized.")
		} else {
			s3Form.Hide()
			driveForm.Show()
			info.Hide()
		}
		updateConnect(service.coordinator.Status())
		if content != nil {
			content.Refresh()
		}
	}
	provider.OnChanged = updateProvider
	service.mu.Lock()
	active := service.activeProvider
	service.mu.Unlock()
	if active == "S3" || g.app.Preferences().String(cloudProviderKey) == "s3" {
		provider.SetSelected("S3")
	} else {
		provider.SetSelected("Google Drive")
	}
	refresh := func(status cloud.Status) {
		updateConnect(status)
		statusLabel.SetText(service.message(status))
		if status.Connected && authCancel == nil && !disconnecting {
			disconnect.Enable()
		} else {
			disconnect.Disable()
		}
	}
	connect.OnTapped = func() {
		if authCancel != nil || disconnecting || service.coordinator.Status().Connected {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		authCancel = cancel
		selected := provider.Selected
		folderName := driveDirectory.Text
		config := cloud.S3Config{Endpoint: endpoint.Text, Region: region.Text, Bucket: bucket.Text, Prefix: prefix.Text, PathStyle: pathStyle.Checked}
		keys := cloud.S3Keys{AccessKey: access.Text, SecretKey: secret.Text}
		refresh(service.coordinator.Status())
		if selected == "S3" {
			statusLabel.SetText("Connecting to S3")
		} else {
			statusLabel.SetText("Complete the Google authorization in your browser")
		}
		go func() {
			var err error
			if selected == "S3" {
				err = service.connectS3(ctx, config, keys)
			} else {
				err = service.connect(ctx, g.app.OpenURL, folderName)
			}
			cancel()
			fyne.Do(func() {
				authCancel = nil
				if closed {
					return
				}
				updateProvider(provider.Selected)
				refresh(service.coordinator.Status())
				if err != nil && !errors.Is(err, context.Canceled) {
					service.logCloudFailure(err)
					statusLabel.SetText(cloudErrorMessage(err))
				}
				if err == nil {
					access.SetText("")
					secret.SetText("")
				}
			})
		}()
	}
	disconnect.OnTapped = func() {
		if authCancel != nil || disconnecting {
			return
		}
		disconnecting = true
		refresh(service.coordinator.Status())
		go func() {
			err := service.disconnect()
			fyne.Do(func() {
				disconnecting = false
				if closed {
					return
				}
				refresh(service.coordinator.Status())
				if err != nil {
					service.logCloudFailure(err)
					statusLabel.SetText(cloudErrorMessage(err))
				}
			})
		}()
	}
	refresh(service.coordinator.Status())
	content = container.NewVBox(statusLabel, provider, driveForm, s3Form, info, connect, disconnect)
	scroll := container.NewVScroll(content)
	scroll.SetMinSize(fyne.NewSize(min(560, g.signatureViewport().Width), min(520, g.signatureViewport().Height)))
	settings := dialog.NewCustom("Cloud sync", "Close", scroll, g.window)
	unsubscribe := service.coordinator.Subscribe(func(status cloud.Status) {
		fyne.Do(func() {
			if !closed && authCancel == nil && !disconnecting {
				refresh(status)
			}
		})
	})
	g.showSignatureDialog(settings, func() {
		closed = true
		unsubscribe()
		if authCancel != nil {
			authCancel()
		}
		access.SetText("")
		secret.SetText("")
		if !g.closed {
			g.showSignatureLibrary()
		}
	})
}

func releaseSignatureService(app fyne.App) {
	signatureServices.Lock()
	service := signatureServices.apps[app]
	delete(signatureServices.apps, app)
	signatureServices.Unlock()
	if service == nil {
		return
	}
	service.mu.Lock()
	service.closed = true
	cancel := service.authCancel
	service.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	service.coordinator.Disconnect()
	go func() {
		service.lifecycle.Lock()
		defer service.lifecycle.Unlock()
		service.coordinator.Disconnect()
		service.mu.Lock()
		stop := service.stopTokens
		service.stopTokens = nil
		service.mu.Unlock()
		if stop != nil {
			stop()
		}
	}()
}
