package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Configuration lives in two files inside ~/.falconia, and the split is
// deliberate.
//
//	instance.json  written by the installer: the instance's address
//	config.json    written by `falconia login`: the token
//
// Whoever installs the CLI on a shared machine, or bakes it into a CI image,
// ships the first file and not the second. Environment variables win over
// both, which is what a pipeline needs when the token comes from a secret
// rather than a file.
type Config struct {
	APIBaseURL string `json:"api_base_url"`
	Token      string `json:"token"`
	Email      string `json:"email"`

	appURL string // cached answer of appBase
}

// env reads a FALCONIA_* variable, falling back to the SFA_* name it had
// before the project was renamed, so existing pipelines keep working.
func env(name string) string {
	if v := os.Getenv("FALCONIA_" + name); v != "" {
		return v
	}
	return os.Getenv("SFA_" + name)
}

func configDir() string {
	if d := env("CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".falconia"
	}
	dir := filepath.Join(home, ".falconia")
	// A machine set up before the rename has ~/.sfa: keep using it until the
	// next login writes the new location.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		legacy := filepath.Join(home, ".sfa")
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return dir
}

func loadConfig() (*Config, error) {
	c := &Config{}
	// The address written by the installer is the baseline.
	if b, err := os.ReadFile(filepath.Join(configDir(), "instance.json")); err == nil {
		var inst struct {
			APIBaseURL string `json:"api_base_url"`
		}
		if json.Unmarshal(b, &inst) == nil {
			c.APIBaseURL = inst.APIBaseURL
		}
	}
	if b, err := os.ReadFile(filepath.Join(configDir(), "config.json")); err == nil {
		var saved Config
		if json.Unmarshal(b, &saved) == nil {
			if saved.APIBaseURL != "" {
				c.APIBaseURL = saved.APIBaseURL
			}
			c.Token = saved.Token
			c.Email = saved.Email
		}
	}
	if v := env("API_URL"); v != "" {
		c.APIBaseURL = v
	}
	if v := env("TOKEN"); v != "" {
		c.Token = v
	}
	c.APIBaseURL = strings.TrimRight(c.APIBaseURL, "/")
	if c.APIBaseURL == "" {
		return nil, errors.New("no instance configured: set FALCONIA_API_URL, or install the CLI from <instance>/api/install")
	}
	return c, nil
}

func (c *Config) save() error {
	home, err := os.UserHomeDir()
	dir := configDir()
	if err == nil && env("CONFIG_DIR") == "" {
		dir = filepath.Join(home, ".falconia")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Keep the instance file next to the token, so a migrated ~/.sfa setup
	// ends up complete in ~/.falconia.
	if _, err := os.Stat(filepath.Join(dir, "instance.json")); os.IsNotExist(err) {
		inst, _ := json.Marshal(map[string]string{"api_base_url": c.APIBaseURL})
		_ = os.WriteFile(filepath.Join(dir, "instance.json"), append(inst, '\n'), 0o600)
	}
	// 0600: the file holds a token worth as much as a password.
	return os.WriteFile(filepath.Join(dir, "config.json"), append(b, '\n'), 0o600)
}

func (c *Config) requireToken() error {
	if c.Token == "" {
		return fmt.Errorf("not signed in: run `falconia login` (or set FALCONIA_TOKEN)")
	}
	return nil
}

// appBase returns the web interface's address, asking the instance once: in
// production it is usually the API's host, in development it is not.
func (c *Config) appBase() string {
	if c.appURL != "" {
		return c.appURL
	}
	c.appURL = strings.TrimSuffix(c.APIBaseURL, "/api")
	var out struct {
		AppBaseURL string `json:"app_base_url"`
	}
	cl := &http.Client{Timeout: 10 * time.Second}
	if resp, err := cl.Get(c.APIBaseURL + "/api/config"); err == nil {
		defer resp.Body.Close()
		if json.NewDecoder(resp.Body).Decode(&out) == nil && out.AppBaseURL != "" {
			c.appURL = strings.TrimRight(out.AppBaseURL, "/")
		}
	}
	return c.appURL
}

func clearToken() error {
	path := filepath.Join(configDir(), "config.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
