package gui

import (
	"context"
	"encoding/json"
	"errors"

	"emanuelelongo/proview/pkg/signatures/cloud"
)

func (s *signatureCloudService) connectS3(ctx context.Context, config cloud.S3Config, keys cloud.S3Keys) error {
	s.restore()
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return context.Canceled
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.authCancel = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.authCancel = nil; s.mu.Unlock() }()
	provider, err := cloud.NewS3(config, keys)
	if err != nil {
		return err
	}
	// A failed replacement leaves the previous working connection intact.
	if err = provider.Prepare(ctx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.store.EnableSync(); err != nil {
		return err
	}
	s.coordinator.Disconnect()
	s.mu.Lock()
	stop := s.stopTokens
	s.stopTokens = nil
	s.tokenWarning = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	var warning error
	persistent := true
	if err = s.s3Credentials.Set(keys); err != nil {
		persistent = false
		_ = s.s3Credentials.Delete()
		warning = errors.New("The system keyring is unavailable. This connection lasts for this session")
	}
	if s.credentials != nil {
		if err = s.credentials.Delete(); err != nil {
			warning = errors.Join(warning, errors.New("The saved Google authorization could not be removed from the system keyring"))
		}
	}
	prefs := s.app.Preferences()
	prefs.SetBool(driveConnectedKey, false)
	prefs.RemoveValue(driveFolderKey)
	prefs.RemoveValue(driveFolderNameKey)
	prefs.RemoveValue(driveFolderExplicitKey)
	data, _ := json.Marshal(provider.Config)
	prefs.SetString(s3ConfigKey, string(data))
	prefs.RemoveValue(cloudProviderKey)
	if persistent {
		prefs.SetString(cloudProviderKey, "s3")
	}
	s.mu.Lock()
	s.warning = warning
	s.activeProvider = "S3"
	s.restored = true
	s.mu.Unlock()
	s.coordinator.Connect(provider)
	s.coordinator.Open(context.Background())
	return nil
}
