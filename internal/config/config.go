package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Profile struct {
	Name     string `json:"name"`
	TokenEnv string `json:"token_env,omitempty"`
	Token    string `json:"token,omitempty"`
}

type Store struct {
	Profiles map[string]Profile `json:"profiles"`
	Default  string             `json:"default,omitempty"`
}

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".airvault", "config.json"), nil
}

func Load() (*Store, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Store{Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	if store.Profiles == nil {
		store.Profiles = map[string]Profile{}
	}
	return &store, nil
}

func Save(store *Store) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func (s *Store) Redacted() *Store {
	cp := &Store{Profiles: map[string]Profile{}, Default: s.Default}
	for name, profile := range s.Profiles {
		if profile.Token != "" {
			profile.Token = "redacted"
		}
		cp.Profiles[name] = profile
	}
	return cp
}
