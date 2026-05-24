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
	Defaults Defaults           `json:"defaults"`
}

type Defaults struct {
	BackupRoot         string   `json:"backup_root,omitempty"`
	Include            []string `json:"include,omitempty"`
	Exclude            []string `json:"exclude,omitempty"`
	VerifyMode         string   `json:"verify_mode,omitempty"`
	SampleSize         int      `json:"sample_size,omitempty"`
	Exporters          []string `json:"exporters,omitempty"`
	NoAttachments      bool     `json:"no_attachments,omitempty"`
	MaxAttachmentBytes int64    `json:"max_attachment_bytes,omitempty"`
}

func BuiltinDefaults() Defaults {
	return Defaults{
		Include:    []string{"schema", "records", "attachments", "views"},
		VerifyMode: "exists",
		SampleSize: 25,
		Exporters:  []string{"jsonl", "sqlite", "postgres"},
	}
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
		return &Store{Profiles: map[string]Profile{}, Defaults: BuiltinDefaults()}, nil
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
	store.Defaults = MergeDefaults(BuiltinDefaults(), store.Defaults)
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
	cp := &Store{Profiles: map[string]Profile{}, Default: s.Default, Defaults: s.Defaults}
	for name, profile := range s.Profiles {
		if profile.Token != "" {
			profile.Token = "redacted"
		}
		cp.Profiles[name] = profile
	}
	return cp
}

func MergeDefaults(base, override Defaults) Defaults {
	out := base
	if override.BackupRoot != "" {
		out.BackupRoot = override.BackupRoot
	}
	if len(override.Include) > 0 {
		out.Include = override.Include
	}
	if len(override.Exclude) > 0 {
		out.Exclude = override.Exclude
	}
	if override.VerifyMode != "" {
		out.VerifyMode = override.VerifyMode
	}
	if override.SampleSize > 0 {
		out.SampleSize = override.SampleSize
	}
	if len(override.Exporters) > 0 {
		out.Exporters = override.Exporters
	}
	if override.NoAttachments {
		out.NoAttachments = true
	}
	if override.MaxAttachmentBytes > 0 {
		out.MaxAttachmentBytes = override.MaxAttachmentBytes
	}
	return out
}
