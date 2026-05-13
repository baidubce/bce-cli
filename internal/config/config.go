package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultConfigDir  = ".bce"
	configFileName    = "config.json"
)

// Configuration is the top-level structure persisted to ~/.bce/config.json.
type Configuration struct {
	Current  string    `json:"current"`
	Profiles []Profile `json:"profiles"`
}

// ConfigDir returns the path to the bce config directory (~/.bce).
func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, defaultConfigDir)
}

func configFilePath() string {
	return filepath.Join(ConfigDir(), configFileName)
}

// Load reads the configuration file. Returns an empty config if the file does not exist.
func Load() (*Configuration, error) {
	data, err := os.ReadFile(configFilePath())
	if os.IsNotExist(err) {
		return &Configuration{Current: "default"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Configuration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// Save writes the configuration to disk atomically.
func Save(cfg *Configuration) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := configFilePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, configFilePath())
}

// GetProfile looks up a profile by name. If name is empty the current profile is used.
func (cfg *Configuration) GetProfile(name string) *Profile {
	target := name
	if target == "" {
		target = cfg.Current
	}
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Name == target {
			return &cfg.Profiles[i]
		}
	}
	return nil
}

// SetProfile upserts a profile into the configuration.
func (cfg *Configuration) SetProfile(p Profile) {
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Name == p.Name {
			cfg.Profiles[i] = p
			return
		}
	}
	cfg.Profiles = append(cfg.Profiles, p)
}
