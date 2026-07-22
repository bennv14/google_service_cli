// Package config manages named profiles persisted to <dir>/config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"gopkg.in/yaml.v3"
)

// ErrNoActiveProfile is returned when no active profile is configured.
var ErrNoActiveProfile = errors.New("no active profile; run 'gsvc config add'")

// Profile bundles an auth mechanism, its credential path, and defaults.
type Profile struct {
	Name       string            `yaml:"-"`
	AuthType   string            `yaml:"auth_type"`
	ClientPath string            `yaml:"client_path,omitempty"`
	KeyPath    string            `yaml:"key_path,omitempty"`
	Defaults   map[string]string `yaml:"defaults,omitempty"`
}

// Store reads and writes profiles.
type Store interface {
	Active() (Profile, error)
	Get(name string) (Profile, error)
	List() []Profile
	Save(p Profile) error
	SetActive(name string) error
}

type configFile struct {
	Active   string             `yaml:"active"`
	Profiles map[string]Profile `yaml:"profiles"`
}

type fileStore struct {
	dir  string
	data configFile
}

// DefaultDir returns the per-user config directory for gsvc.
func DefaultDir() (string, error) {
	const app = "google_service_cli"
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, app), nil
	}
	if runtime.GOOS == "windows" {
		d, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(d, app), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", app), nil
}

// NewStore loads <dir>/config.yaml if present; an absent file yields an empty store.
func NewStore(dir string) (Store, error) {
	s := &fileStore{dir: dir, data: configFile{Profiles: map[string]Profile{}}}
	b, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("parse config.yaml: %w", err)
	}
	if s.data.Profiles == nil {
		s.data.Profiles = map[string]Profile{}
	}
	return s, nil
}

func (s *fileStore) Get(name string) (Profile, error) {
	p, ok := s.data.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("profile %q not found", name)
	}
	p.Name = name
	return p, nil
}

func (s *fileStore) Active() (Profile, error) {
	if s.data.Active == "" {
		return Profile{}, ErrNoActiveProfile
	}
	return s.Get(s.data.Active)
}

func (s *fileStore) List() []Profile {
	out := make([]Profile, 0, len(s.data.Profiles))
	for name, p := range s.data.Profiles {
		p.Name = name
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *fileStore) Save(p Profile) error {
	if p.Name == "" {
		return errors.New("profile name is required")
	}
	name := p.Name
	p.Name = "" // the map key is the identity; don't duplicate it in the value
	s.data.Profiles[name] = p
	if s.data.Active == "" {
		s.data.Active = name
	}
	return s.flush()
}

func (s *fileStore) SetActive(name string) error {
	if _, ok := s.data.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	s.data.Active = name
	return s.flush()
}

func (s *fileStore) flush() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(s.data)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, "config.yaml"), b, 0o600)
}
