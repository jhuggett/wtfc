package change

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jhuggett/wtfc/internal/auto"
	"github.com/jhuggett/wtfc/internal/config"
)

// Change is a single pending changeset on disk.
// Stored as JSON. Unknown fields are preserved via the Extras map so schema
// drift never loses data.
type Change struct {
	ID        string         `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	Fields    map[string]any `json:"-"`
	raw       map[string]json.RawMessage
}

// MarshalJSON flattens ID, CreatedAt, and all user fields into one object.
func (c Change) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range c.Fields {
		out[k] = v
	}
	out["id"] = c.ID
	out["created_at"] = c.CreatedAt
	return json.Marshal(out)
}

// UnmarshalJSON reads everything; pulls id/created_at into typed fields,
// leaves the rest in Fields.
func (c *Change) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.raw = raw
	c.Fields = map[string]any{}
	for k, v := range raw {
		switch k {
		case "id":
			if err := json.Unmarshal(v, &c.ID); err != nil {
				return err
			}
		case "created_at":
			if err := json.Unmarshal(v, &c.CreatedAt); err != nil {
				return err
			}
		default:
			var any any
			if err := json.Unmarshal(v, &any); err != nil {
				return err
			}
			c.Fields[k] = any
		}
	}
	return nil
}

// New builds a Change with id + timestamp set and user fields applied per the
// schema. Unknown fields are accepted (loose schema).
func New(cfg *config.Config, values map[string]string) (*Change, error) {
	fields := map[string]any{}
	for _, f := range cfg.Changeset.Fields {
		// Seed every schema field as null so the on-disk file shows
		// what's settable. Users/agents can fill in later.
		fields[f.Name] = nil
	}
	for k, v := range values {
		coerced, err := coerce(cfg.Changeset.Fields, k, v)
		if err != nil {
			return nil, err
		}
		fields[k] = coerced
	}
	return &Change{
		ID:        uuid.NewString(),
		CreatedAt: time.Now().UTC(),
		Fields:    fields,
	}, nil
}

// NewFromValues is like New but takes already-typed values (e.g. from a
// JSON object). The values map should be in canonical form per the schema
// (string for enum, []string or []any for list, bool, int, etc). Unknown
// fields are accepted and stored as-is.
func NewFromValues(cfg *config.Config, values map[string]any) *Change {
	fields := map[string]any{}
	for _, f := range cfg.Changeset.Fields {
		fields[f.Name] = nil
	}
	for k, v := range values {
		if k == "id" || k == "created_at" {
			continue
		}
		fields[k] = v
	}
	return &Change{
		ID:        uuid.NewString(),
		CreatedAt: time.Now().UTC(),
		Fields:    fields,
	}
}

// CoerceFields converts a string flag map into typed values per the schema.
// Mirrors release.CoerceFields so the CLI can merge --field with --json.
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

// coerce turns the CLI string into the appropriate Go type given the schema.
// Unknown fields fall back to string.
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
		default: // string, enum, anything else
			return raw, nil
		}
	}
	return raw, nil
}

// Apply merges values into this change's Fields. Provided keys overwrite;
// absent keys are left untouched. Pass null to clear a field. id and
// created_at are read-only and skipped.
func (c *Change) Apply(values map[string]any) {
	if c.Fields == nil {
		c.Fields = map[string]any{}
	}
	for k, v := range values {
		if k == "id" || k == "created_at" {
			continue
		}
		c.Fields[k] = v
	}
}

// Path returns where this change should be written.
func (c *Change) Path(cfg *config.Config) string {
	return filepath.Join(cfg.PendingDir(), c.ID+".json")
}

// Write persists the change file, creating the pending dir if needed.
// Validates required fields against the configured changeset schema —
// every entry point (TUI / CLI / MCP) ultimately routes through Write,
// so this is the single enforcement boundary. Auto-fill resolution
// (e.g. `source = "git.user"`) runs first so user-provided values win
// but missing slots still get populated before validation.
func (c *Change) Write(cfg *config.Config) error {
	auto.Resolve(cfg.ProjectRoot(), cfg.Changeset.Fields, c.Fields)
	if err := config.Validate(cfg.Changeset.Fields, c.Fields); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.PendingDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path(cfg), data, 0o644)
}

// FindByID locates a single pending change file by id (full UUID or any
// prefix that uniquely identifies one). Returns the change, its file path,
// and ErrAmbiguous if the prefix matches more than one, or ErrNotFound if
// nothing matches.
func FindByID(cfg *config.Config, id string) (*Change, string, error) {
	if id == "" {
		return nil, "", fmt.Errorf("id required")
	}
	changes, paths, err := List(cfg)
	if err != nil {
		return nil, "", err
	}
	var hits []int
	for i, c := range changes {
		if c.ID == id || strings.HasPrefix(c.ID, id) {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return nil, "", fmt.Errorf("no pending change matches %q", id)
	case 1:
		return changes[hits[0]], paths[hits[0]], nil
	default:
		var matches []string
		for _, h := range hits {
			matches = append(matches, changes[h].ID)
		}
		return nil, "", fmt.Errorf("id %q matches %d changes: %s",
			id, len(hits), strings.Join(matches, ", "))
	}
}

// List returns all pending change files, sorted by CreatedAt ascending.
func List(cfg *config.Config) ([]*Change, []string, error) {
	dir := cfg.PendingDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var changes []*Change
	var paths []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		var c Change
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", p, err)
		}
		changes = append(changes, &c)
		paths = append(paths, p)
	}
	// Sort by CreatedAt then ID for stable order
	idx := make([]int, len(changes))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		ca, cb := changes[idx[a]], changes[idx[b]]
		if ca.CreatedAt.Equal(cb.CreatedAt) {
			return ca.ID < cb.ID
		}
		return ca.CreatedAt.Before(cb.CreatedAt)
	})
	sortedC := make([]*Change, len(changes))
	sortedP := make([]string, len(paths))
	for i, j := range idx {
		sortedC[i] = changes[j]
		sortedP[i] = paths[j]
	}
	return sortedC, sortedP, nil
}
