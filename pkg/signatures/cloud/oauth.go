package cloud

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"emanuelelongo/proview/internal/appprofile"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// Set with -ldflags in distributed builds. A desktop client's configuration is
// public; user tokens must never be embedded in a build or stored in preferences.
var GoogleClientID string
var GoogleClientSecret string

const driveScope = "https://www.googleapis.com/auth/drive.file"

var ErrNotConnected = errors.New("Google Drive is not connected")

type Credentials interface {
	Get() (*oauth2.Token, error)
	Set(*oauth2.Token) error
	Delete() error
}
type SystemCredentials struct{}

func (SystemCredentials) Get() (*oauth2.Token, error) {
	data, err := keyring.Get(appprofile.CredentialService("google-drive"), "oauth")
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotConnected
	}
	if err != nil {
		return nil, err
	}
	var token oauth2.Token
	if err = json.Unmarshal([]byte(data), &token); err != nil {
		return nil, err
	}
	return &token, nil
}
func (SystemCredentials) Set(token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return keyring.Set(appprofile.CredentialService("google-drive"), "oauth", string(data))
}
func (SystemCredentials) Delete() error {
	err := keyring.Delete(appprofile.CredentialService("google-drive"), "oauth")
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func oauthConfig() *oauth2.Config {
	id, secret := GoogleClientID, GoogleClientSecret
	if id == "" {
		id = os.Getenv("PROVIEW_GOOGLE_CLIENT_ID")
	}
	if secret == "" {
		secret = os.Getenv("PROVIEW_GOOGLE_CLIENT_SECRET")
	}
	return &oauth2.Config{ClientID: id, ClientSecret: secret, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token"}, Scopes: []string{driveScope}}
}

func randomValue() (string, error) {
	var data [32]byte
	_, err := rand.Read(data[:])
	return base64.RawURLEncoding.EncodeToString(data[:]), err
}

type Authorization struct {
	Token    *oauth2.Token
	FolderID string
}

// Authorize opens only the system browser. The temporary callback is bound to
// loopback, validates state, exchanges the code with PKCE, then shuts down.
func Authorize(ctx context.Context, openBrowser func(*url.URL) error, pickFolder bool) (Authorization, error) {
	return authorize(ctx, oauthConfig(), openBrowser, pickFolder)
}
func authorize(ctx context.Context, config *oauth2.Config, openBrowser func(*url.URL) error, pickFolder bool) (Authorization, error) {
	var result Authorization
	if config.ClientID == "" {
		return result, errors.New("Google OAuth client configuration is missing. See the cloud sync setup documentation")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return result, err
	}
	defer listener.Close()
	state, err := randomValue()
	if err != nil {
		return result, err
	}
	verifier, err := randomValue()
	if err != nil {
		return result, err
	}
	configCopy := *config
	config = &configCopy
	config.RedirectURL = "http://" + listener.Addr().String() + "/oauth/callback"
	type callback struct {
		code, folder string
		err          error
	}
	received := make(chan callback, 1)
	mux := http.NewServeMux()
	var once sync.Once
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid authorization state", http.StatusBadRequest)
			return
		}
		query := r.URL.Query()
		cb := callback{code: query.Get("code")}
		if query.Get("error") != "" {
			cb.err = errors.New("Google authorization was cancelled or denied")
		}
		if cb.code == "" && cb.err == nil {
			cb.err = errors.New("Google did not return an authorization code")
		}
		if pickFolder {
			ids := strings.Split(query.Get("picked_file_ids"), ",")
			if len(ids) != 1 || ids[0] == "" {
				cb.err = errors.New("select one proview folder")
			} else {
				cb.folder = ids[0]
			}
		}
		accepted := false
		once.Do(func() { accepted = true; received <- cb })
		if !accepted {
			http.Error(w, "Authorization already received", http.StatusConflict)
			return
		}
		fmt.Fprint(w, "Return to Proview. You can close this tab.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()
	defer server.Close()
	challenge := sha256.Sum256([]byte(verifier))
	options := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:])), oauth2.SetAuthURLParam("code_challenge_method", "S256"), oauth2.SetAuthURLParam("prompt", "consent")}
	if pickFolder {
		options = append(options, oauth2.SetAuthURLParam("trigger_onepick", "true"), oauth2.SetAuthURLParam("allow_folder_selection", "true"), oauth2.SetAuthURLParam("mimetypes", folderMIME))
	}
	authURL, err := url.Parse(config.AuthCodeURL(state, options...))
	if err != nil {
		return result, err
	}
	if err = openBrowser(authURL); err != nil {
		return result, err
	}
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case err := <-serverErrors:
		return result, err
	case cb := <-received:
		if cb.err != nil {
			return result, cb.err
		}
		token, err := config.Exchange(ctx, cb.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return result, errors.New("Google authorization exchange failed; reconnect and try again")
		}
		if token.RefreshToken == "" {
			return result, errors.New("Google did not grant offline access; reconnect and try again")
		}
		return Authorization{Token: token, FolderID: cb.folder}, nil
	}
}

// Refreshes run with a session lifetime context, rather than the short-lived
// callback context, so later syncs remain functional after login completes.
type persistentTokens struct {
	source      oauth2.TokenSource
	credentials Credentials
	persistent  bool
	mu          sync.Mutex
	last        *oauth2.Token
	err         error
	closed      bool
	warningMu   sync.Mutex
}

func (p *persistentTokens) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, ErrNotConnected
	}
	token, err := p.source.Token()
	if err != nil {
		return nil, errors.New("Google authorization could not be refreshed. Reconnect to Google Drive")
	}
	if p.persistent && (p.last == nil || p.last.AccessToken != token.AccessToken || p.last.RefreshToken != token.RefreshToken) {
		if err := p.credentials.Set(token); err != nil {
			p.warningMu.Lock()
			p.err = errors.New("Google authorization could not be saved; this connection lasts for this session")
			p.warningMu.Unlock()
			p.persistent = false
		}
	}
	copyToken := *token
	p.last = &copyToken
	return token, nil
}
func (p *persistentTokens) Warning() error {
	p.warningMu.Lock()
	defer p.warningMu.Unlock()
	return p.err
}

func GoogleClient(token *oauth2.Token, credentials Credentials, persistent bool) (*http.Client, func() error, func()) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 45 * time.Second}))
	source := &persistentTokens{source: oauthConfig().TokenSource(ctx, token), credentials: credentials, persistent: persistent, last: token}
	client := oauth2.NewClient(context.Background(), source)
	client.Timeout = 45 * time.Second
	return client, source.Warning, func() { cancel(); source.mu.Lock(); source.closed = true; source.mu.Unlock() }
}
