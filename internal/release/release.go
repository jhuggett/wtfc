package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jhuggett/wtfc/internal/auto"
	"github.com/jhuggett/wtfc/internal/change"
	"github.com/jhuggett/wtfc/internal/config"
)

// Release is one entry in changelog.json. It carries its own metadata plus the
// flattened change objects from the pending dir at release time.
type Release struct {
	Name       string           `json:"name"`
	ReleasedAt time.Time        `json:"released_at"`
	Fields     map[string]any   `json:"-"`
	Changes    []*change.Change `json:"-"`
}

func (r Release) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range r.Fields {
		out[k] = v
	}
	out["name"] = r.Name
	out["released_at"] = r.ReleasedAt
	out["changes"] = r.Changes
	return json.Marshal(out)
}

func (r *Release) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Fields = map[string]any{}
	for k, v := range raw {
		switch k {
		case "name":
			if err := json.Unmarshal(v, &r.Name); err != nil {
				return err
			}
		case "released_at":
			if err := json.Unmarshal(v, &r.ReleasedAt); err != nil {
				return err
			}
		case "changes":
			if err := json.Unmarshal(v, &r.Changes); err != nil {
				return err
			}
		default:
			var any any
			if err := json.Unmarshal(v, &any); err != nil {
				return err
			}
			r.Fields[k] = any
		}
	}
	return nil
}

// Changelog is the top-level wrapper persisted at config.ChangelogPath().
// Releases are stored chronologically (oldest first); display can reverse.
type Changelog struct {
	Releases []*Release `json:"releases"`
}

// Load reads the changelog from disk; returns an empty one if the file is missing.
func Load(cfg *config.Config) (*Changelog, error) {
	data, err := os.ReadFile(cfg.ChangelogPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Changelog{}, nil
		}
		return nil, err
	}
	var cl Changelog
	if err := json.Unmarshal(data, &cl); err != nil {
		return nil, fmt.Errorf("parse changelog: %w", err)
	}
	return &cl, nil
}

func (cl *Changelog) Save(cfg *config.Config) error {
	data, err := json.MarshalIndent(cl, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfg.ChangelogPath(), data, 0o644)
}

// History returns release names with their release timestamps, newest first.
func (cl *Changelog) History() []*Release {
	out := make([]*Release, len(cl.Releases))
	for i, r := range cl.Releases {
		out[len(cl.Releases)-1-i] = r
	}
	return out
}

// Cut creates a new release: collapses pending changes into a release entry,
// appends to the changelog, writes it, and deletes the pending change files.
// `fields` is the typed metadata map (use CoerceFields to convert from string
// CLI flags, or pass values from a parsed JSON object directly).
func Cut(cfg *config.Config, name string, fields map[string]any) (*Release, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("release name is required")
	}
	// Auto-fill resolution then required-field enforcement, mirroring
	// change.Write. User-provided values always win; missing slots get
	// populated from declared sources (git.sha, git.branch, etc.).
	auto.Resolve(cfg.ProjectRoot(), cfg.Release.Fields, fields)
	if err := config.Validate(cfg.Release.Fields, fields); err != nil {
		return nil, err
	}
	changes, paths, err := change.List(cfg)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("no pending changes in %s", cfg.PendingDir())
	}
	cl, err := Load(cfg)
	if err != nil {
		return nil, err
	}

	// Seed every schema field as null so the on-disk record shows what was
	// settable, then layer user-supplied values on top.
	merged := map[string]any{}
	for _, f := range cfg.Release.Fields {
		merged[f.Name] = nil
	}
	for k, v := range fields {
		merged[k] = v
	}

	rel := &Release{
		Name:       name,
		ReleasedAt: time.Now().UTC(),
		Fields:     merged,
		Changes:    changes,
	}
	cl.Releases = append(cl.Releases, rel)
	if err := cl.Save(cfg); err != nil {
		return nil, err
	}
	// Only delete pending files after the changelog write succeeded.
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			return rel, fmt.Errorf("release saved but failed to remove %s: %w", p, err)
		}
	}
	return rel, nil
}

// CoerceFields converts a map of string CLI values into typed values per the
// schema. Unknown fields fall through as plain strings.
func CoerceFields(schema []config.Field, raw map[string]string) (map[string]any, error) {
	out := map[string]any{}
	for k, v := range raw {
		coerced, err := coerce(schema, k, v)
		if err != nil {
			return nil, err
		}
		out[k] = coerced
	}
	return out, nil
}

// Unrelease pops the most recent release and restores its changes back into
// the pending dir as individual JSON files. The release's own metadata
// (name, released_at, release-level fields) is discarded — only the change
// records are recovered.
func Unrelease(cfg *config.Config) (*Release, error) {
	cl, err := Load(cfg)
	if err != nil {
		return nil, err
	}
	if len(cl.Releases) == 0 {
		return nil, fmt.Errorf("no releases to unrelease")
	}
	last := cl.Releases[len(cl.Releases)-1]

	// Refuse if any change UUID already exists in the pending dir, so we
	// don't silently clobber in-flight work.
	for _, c := range last.Changes {
		p := filepath.Join(cfg.PendingDir(), c.ID+".json")
		if _, err := os.Stat(p); err == nil {
			return nil, fmt.Errorf("pending file %s already exists; refusing to overwrite", p)
		}
	}

	if err := os.MkdirAll(cfg.PendingDir(), 0o755); err != nil {
		return nil, err
	}
	for _, c := range last.Changes {
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return nil, err
		}
		p := filepath.Join(cfg.PendingDir(), c.ID+".json")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, err
		}
	}

	cl.Releases = cl.Releases[:len(cl.Releases)-1]
	if err := cl.Save(cfg); err != nil {
		return last, fmt.Errorf("changes restored but failed to update changelog: %w", err)
	}
	return last, nil
}

// coerce mirrors change.coerce but for release-level schema fields.
func coerce(schema []config.Field, key, raw string) (any, error) {
	for _, f := range schema {
		if f.Name != key {
			continue
		}
		switch f.Type {
		case "bool":
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", key, err)
			}
			return b, nil
		case "int":
			n, err := strconv.Atoi(raw)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", key, err)
			}
			return n, nil
		case "list":
			parts := strings.Split(raw, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			return out, nil
		default:
			return raw, nil
		}
	}
	return raw, nil
}
