package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// All wtfc data lives under a visible `wtfc/` directory at the project root.
//
//	wtfc/
//	  config.toml      # schema
//	  pending/         # pending change files (one JSON per change)
//	  changelog.json   # collapsed releases
//	  hooks/           # (future) user-defined hooks
const (
	DirName    = "wtfc"
	ConfigFile = "config.toml"
)

// Field describes one user-defined attribute on a changeset or release.
// The schema is additive — adding fields later doesn't break old data.
// Required=true means the field must have a non-empty value at create
// or release time. Source names a built-in auto-fill provider (e.g.
// "git.user", "git.sha"); user-supplied values always win, but missing
// slots get populated from the source at the write boundary.
type Field struct {
	Name     string   `toml:"name"               json:"name"`
	Type     string   `toml:"type"               json:"type"`               // string, bool, int, enum, list
	Values   []string `toml:"values,omitempty"   json:"values,omitempty"`   // for enum/list
	Required bool     `toml:"required,omitempty" json:"required,omitempty"` // must be set at create/release time
	Source   string   `toml:"source,omitempty"   json:"source,omitempty"`   // auto-fill provider (e.g. "git.user")
}

type Section struct {
	Fields []Field `toml:"fields"`
}

type Config struct {
	Changeset Section `toml:"changeset"`
	Release   Section `toml:"release"`

	// Path to the config.toml file that produced this config.
	Path string `toml:"-"`
}

// Dir returns the wtfc/ directory itself (the parent of config.toml).
func (c *Config) Dir() string {
	return filepath.Dir(c.Path)
}

// ProjectRoot is the directory that contains the wtfc/ directory.
func (c *Config) ProjectRoot() string {
	return filepath.Dir(c.Dir())
}

// PendingDir is where individual change JSON files live.
func (c *Config) PendingDir() string {
	return filepath.Join(c.Dir(), "pending")
}

// ChangelogPath is the collapsed releases file inside the wtfc/ dir.
func (c *Config) ChangelogPath() string {
	return filepath.Join(c.Dir(), "changelog.json")
}

// Load reads the config from an explicit path.
func Load(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	c.Path = path
	return &c, nil
}

// FindUp walks from start up to filesystem root looking for wtfc/config.toml.
// Returns the absolute path of the config file if found.
func FindUp(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, DirName, ConfigFile)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

// LoadFromCwd finds the nearest wtfc/config.toml walking up from cwd.
func LoadFromCwd() (*Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	path, err := FindUp(cwd)
	if err != nil {
		return nil, err
	}
	return Load(path)
}

// FindDown walks down from root and returns paths of every wtfc/config.toml
// found. Used by the TUI to let the user pick when there are multiple.
// Skips hidden directories and common heavy ones (node_modules, vendor, .git).
func FindDown(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // ignore unreadable dirs
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "node_modules" || name == "vendor" || name == ".git" || (len(name) > 0 && name[0] == '.')) {
				return filepath.SkipDir
			}
			return nil
		}
		// Match files named config.toml whose parent directory is "wtfc".
		if d.Name() == ConfigFile && filepath.Base(filepath.Dir(path)) == DirName {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

var ErrNotFound = errors.New("wtfc/config.toml not found")

// Validate checks that every Required field in schema has a non-empty
// value in values. Returns an error listing all missing required field
// names, or nil if all are satisfied. Type-aware: empty strings, empty
// lists, and missing keys all count as "not set".
//
// Single source of truth for required-field enforcement — TUI form
// submit, CLI commands, and MCP tool handlers all call this so the
// rules are identical across entry points.
func Validate(schema []Field, values map[string]any) error {
	var missing []string
	for _, f := range schema {
		if !f.Required {
			continue
		}
		v, ok := values[f.Name]
		if !ok || v == nil {
			missing = append(missing, f.Name)
			continue
		}
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) == "" {
				missing = append(missing, f.Name)
			}
		case []any:
			if len(x) == 0 {
				missing = append(missing, f.Name)
			}
		case []string:
			if len(x) == 0 {
				missing = append(missing, f.Name)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required field(s): %s", strings.Join(missing, ", "))
	}
	return nil
}
