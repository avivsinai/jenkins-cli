package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	configFileName = "config.yml"
	currentVersion = 1
)

var (
	ErrContextNotFound = errors.New("context not found")
)

// Config models the persisted CLI configuration.
type Config struct {
	Version     int                 `yaml:"version"`
	Active      string              `yaml:"active,omitempty"`
	Contexts    map[string]*Context `yaml:"contexts,omitempty"`
	Preferences Preferences         `yaml:"preferences,omitempty"`
	path        string              `yaml:"-"`
	mu          sync.RWMutex        `yaml:"-"`
}

// Context represents a Jenkins connection configuration.
type Context struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username,omitempty"`
	Insecure bool   `yaml:"insecure,omitempty"`
	Proxy    string `yaml:"proxy,omitempty"`
	CAFile   string `yaml:"ca_file,omitempty"`
}

// Preferences capture user-level CLI options.
type Preferences struct {
	Color          string `yaml:"color,omitempty"`
	OutputFormat   string `yaml:"output_format,omitempty"`
	MaxConcurrency int    `yaml:"max_concurrency,omitempty"`
}

// Load retrieves configuration from disk, returning default values when the
// file does not exist.
func Load() (*Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Version:  currentVersion,
		Contexts: make(map[string]*Context),
		path:     path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	cfg.path = path
	return cfg, nil
}

// Save persists the configuration atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.path == "" {
		path, err := DefaultPath()
		if err != nil {
			return err
		}
		c.path = path
	}

	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if c.Version == 0 {
		c.Version = currentVersion
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".config-*.yml")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	defer func() {
		_ = os.Remove(tmpFile.Name())
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp config: %w", err)
	}

	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}

	if err := os.Rename(tmpFile.Name(), c.path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// DefaultPath returns the on-disk location for the config file.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(dir, "jk", configFileName), nil
}

// Path returns the config file path on disk.
func (c *Config) Path() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.path
}

// SetContext adds or replaces a context by name.
func (c *Config) SetContext(name string, ctx *Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Contexts == nil {
		c.Contexts = make(map[string]*Context)
	}
	c.Contexts[name] = ctx
}

// RemoveContext deletes a named context.
func (c *Config) RemoveContext(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.Contexts, name)
	if c.Active == name {
		c.Active = ""
	}
}

// Context retrieves a context by name.
func (c *Config) Context(name string) (*Context, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if ctx, ok := c.Contexts[name]; ok {
		return ctx, nil
	}
	return nil, ErrContextNotFound
}

// SetActive updates the active context name after verifying existence.
func (c *Config) SetActive(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if name == "" {
		c.Active = ""
		return nil
	}

	if c.Contexts == nil {
		return ErrContextNotFound
	}

	if _, ok := c.Contexts[name]; !ok {
		return ErrContextNotFound
	}

	c.Active = name
	return nil
}

// ActiveContext returns the currently selected context, if any.
func (c *Config) ActiveContext() (*Context, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.Active == "" {
		return nil, "", nil
	}

	ctx, ok := c.Contexts[c.Active]
	if !ok {
		return nil, c.Active, ErrContextNotFound
	}
	return ctx, c.Active, nil
}
