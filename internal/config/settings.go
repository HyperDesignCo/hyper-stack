package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Settings are per-machine options in <data dir>/settings.json.
type Settings struct {
	HTTPPort    int `json:"http_port"`
	HTTPSPort   int `json:"https_port"`
	MySQLPort   int `json:"mysql_port"`
	FPMBasePort int `json:"fpm_base_port"` // PHP 8.2 -> base+2, 8.3 -> base+3 ... each with 4 workers
	// Autostart lists project folders to start whenever Hyper Stack starts at login.
	Autostart []string `json:"autostart,omitempty"`
}

// DefaultSettings are the standard ports.
func DefaultSettings() Settings {
	return Settings{HTTPPort: 80, HTTPSPort: 443, MySQLPort: 3306, FPMBasePort: 9000}
}

// LoadSettings reads settings.json, filling missing values with defaults.
func (p Paths) LoadSettings() (Settings, error) {
	s := DefaultSettings()
	b, err := os.ReadFile(filepath.Join(p.Root, "settings.json"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("settings.json is invalid: %w. Fix or delete %s", err, filepath.Join(p.Root, "settings.json"))
	}
	d := DefaultSettings()
	if s.HTTPPort == 0 {
		s.HTTPPort = d.HTTPPort
	}
	if s.HTTPSPort == 0 {
		s.HTTPSPort = d.HTTPSPort
	}
	if s.MySQLPort == 0 {
		s.MySQLPort = d.MySQLPort
	}
	if s.FPMBasePort == 0 {
		s.FPMBasePort = d.FPMBasePort
	}
	return s, nil
}

// SaveSettings writes settings.json.
func (p Paths) SaveSettings(s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.Root, "settings.json"), b, 0o644)
}

// SiteURL builds the public URL for a domain given the HTTPS port.
func (s Settings) SiteURL(domain string) string {
	if s.HTTPSPort == 443 {
		return "https://" + domain
	}
	return fmt.Sprintf("https://%s:%d", domain, s.HTTPSPort)
}
