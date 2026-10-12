package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestOAuthLoopbackStatePKCEAndFolderSelection(t *testing.T) {
	for _, pick := range []bool{false, true} {
		t.Run(fmt.Sprint(pick), func(t *testing.T) {
			challenge := ""
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if base64.RawURLEncoding.EncodeToString(hash[:]) != challenge {
					t.Error("PKCE verifier does not match challenge")
				}
				if r.Form.Get("code") != "authorization-code" {
					t.Error("wrong authorization code")
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`)
			}))
			defer tokenServer.Close()
			config := &oauth2.Config{ClientID: "desktop-client", ClientSecret: "desktop-secret", Scopes: []string{driveScope}, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.example/authorize", TokenURL: tokenServer.URL}}
			result, err := authorize(context.Background(), config, func(auth *url.URL) error {
				q := auth.Query()
				challenge = q.Get("code_challenge")
				if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || q.Get("scope") != driveScope {
					t.Error("incorrect authorization parameters")
				}
				if pick && (q.Get("trigger_onepick") != "true" || q.Get("allow_folder_selection") != "true" || q.Get("mimetypes") != folderMIME) {
					t.Error("folder Picker not enabled")
				}
				callback, _ := url.Parse(q.Get("redirect_uri"))
				if !strings.HasPrefix(callback.Host, "127.0.0.1:") {
					t.Error("callback is not loopback")
				}
				callback.RawQuery = url.Values{"state": {"wrong"}, "code": {"authorization-code"}}.Encode()
				response, err := http.Get(callback.String())
				if err != nil {
					return err
				}
				response.Body.Close()
				if response.StatusCode != 400 {
					t.Error("incorrect state was accepted")
				}
				values := url.Values{"state": {q.Get("state")}, "code": {"authorization-code"}}
				if pick {
					values.Set("picked_file_ids", "folder-id")
				}
				callback.RawQuery = values.Encode()
				response, err = http.Get(callback.String())
				if err != nil {
					return err
				}
				response.Body.Close()
				return nil
			}, pick)
			if err != nil || result.Token.RefreshToken != "refresh" {
				t.Fatalf("authorization failed: %+v %v", result, err)
			}
			if pick && result.FolderID != "folder-id" {
				t.Fatal("folder selection lost")
			}
			if config.RedirectURL != "" {
				t.Fatal("authorization mutated shared configuration")
			}
		})
	}
}
func TestOAuthCancellationAndBrowserFailure(t *testing.T) {
	config := &oauth2.Config{ClientID: "desktop", Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.example"}}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := authorize(ctx, config, func(*url.URL) error { cancel(); return nil }, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation failed: %v", err)
	}
	expected := errors.New("no browser")
	_, err = authorize(context.Background(), config, func(*url.URL) error { return expected }, false)
	if !errors.Is(err, expected) {
		t.Fatal("browser error was lost")
	}
}

type memoryCredentials struct {
	saved *oauth2.Token
	fail  bool
	sets  int
}

func (m *memoryCredentials) Get() (*oauth2.Token, error) {
	if m.saved == nil {
		return nil, ErrNotConnected
	}
	return m.saved, nil
}
func (m *memoryCredentials) Set(t *oauth2.Token) error {
	m.sets++
	if m.fail {
		return errors.New("keyring unavailable")
	}
	copy := *t
	m.saved = &copy
	return nil
}
func (m *memoryCredentials) Delete() error { m.saved = nil; return nil }

type fixedSource struct{ token *oauth2.Token }

func (f fixedSource) Token() (*oauth2.Token, error) { return f.token, nil }
func TestRefreshTokenPersistenceAndSessionFallback(t *testing.T) {
	token := &oauth2.Token{AccessToken: "fresh", RefreshToken: "rotated", Expiry: time.Now().Add(time.Hour)}
	credentials := &memoryCredentials{}
	source := &persistentTokens{source: fixedSource{token}, credentials: credentials, persistent: true, last: &oauth2.Token{AccessToken: "old"}}
	if _, err := source.Token(); err != nil || credentials.saved.RefreshToken != "rotated" {
		t.Fatal("refresh token was not persisted")
	}
	source.Token()
	if credentials.sets != 1 {
		t.Fatal("unchanged token written again")
	}
	credentials.fail = true
	source.last = &oauth2.Token{AccessToken: "old"}
	if _, err := source.Token(); err != nil || source.Warning() == nil {
		t.Fatal("keyring failure blocked session instead of reporting warning")
	}
	source.Token()
	if credentials.sets != 2 {
		t.Fatal("unavailable keyring retried on every request")
	}
	source.closed = true
	if _, err := source.Token(); !errors.Is(err, ErrNotConnected) {
		t.Fatal("closed token source still authorizes requests")
	}
}
