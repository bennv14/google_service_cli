package auth

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
)

// TokenStore persists OAuth tokens per profile.
type TokenStore interface {
	Load(profile string) (*oauth2.Token, error)
	Save(profile string, tok *oauth2.Token) error
	Delete(profile string) error
}

type fileTokenStore struct{ dir string }

// NewFileTokenStore stores tokens as <dir>/<profile>.json (mode 0600).
func NewFileTokenStore(dir string) TokenStore { return &fileTokenStore{dir: dir} }

func (f *fileTokenStore) path(profile string) string {
	return filepath.Join(f.dir, profile+".json")
}

func (f *fileTokenStore) Load(profile string) (*oauth2.Token, error) {
	b, err := os.ReadFile(f.path(profile))
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (f *fileTokenStore) Save(profile string, tok *oauth2.Token) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return os.WriteFile(f.path(profile), b, 0o600)
}

func (f *fileTokenStore) Delete(profile string) error {
	err := os.Remove(f.path(profile))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
