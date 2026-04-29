// Package auto resolves the named auto-fill sources declared on schema
// fields (e.g. `source = "git.user"`). Resolution runs at the write
// boundary alongside config.Validate, so every entry point — TUI, CLI,
// MCP — benefits from the same logic without separate plumbing.
//
// The contract is simple: user-supplied values always win. Sources fill
// only when a field's slot is missing or empty. If a source can't
// resolve (no git, not in a repo, no user.name), the slot stays empty
// and downstream required-field validation handles it the same way it
// would for any other unset field.
package auto

import (
	"os"
	"os/exec"
	"strings"

	"github.com/jhuggett/wtfc/internal/config"
)

// Source produces a value for a field from the runtime environment.
// projectRoot is the directory containing the wtfc/ folder; sources
// that shell out to git use it as `git -C <root>` so they work even
// when wtfc was launched from a different cwd (typical for MCP).
type Source func(projectRoot string) string

// Sources is the registry of built-in auto-fill providers. Add new
// sources by registering them here; the schema references them by name
// (e.g. `source = "git.user"`).
var Sources = map[string]Source{
	"git.user":    gitConfig("user.name"),
	"git.email":   gitConfig("user.email"),
	"git.sha":     gitRevParse("HEAD"),
	"git.branch":  gitBranch,
	"system.user": systemUser,
}

// Names returns the registered source names in stable alphabetical
// order — used to surface the list in error messages and docs.
func Names() []string {
	out := make([]string, 0, len(Sources))
	for name := range Sources {
		out = append(out, name)
	}
	// Simple insertion sort to avoid pulling in sort just for this.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// Resolve fills missing slots in values from each field's declared
// source. Existing non-empty values are left alone. Unknown source
// names are silently ignored — a typo there is a config issue, not a
// runtime crash, and the field will simply not auto-fill.
func Resolve(projectRoot string, schema []config.Field, values map[string]any) {
	for _, f := range schema {
		if f.Source == "" {
			continue
		}
		if !isEmpty(values[f.Name]) {
			continue
		}
		src, ok := Sources[f.Source]
		if !ok {
			continue
		}
		v := strings.TrimSpace(src(projectRoot))
		if v != "" {
			values[f.Name] = v
		}
	}
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case []string:
		return len(x) == 0
	}
	return false
}

// gitConfig returns a Source that reads `git config <key>`.
func gitConfig(key string) Source {
	return func(root string) string {
		out, err := runGit(root, "config", key)
		if err != nil {
			return ""
		}
		return out
	}
}

// gitRevParse returns a Source that runs `git rev-parse <ref>`.
func gitRevParse(ref string) Source {
	return func(root string) string {
		out, err := runGit(root, "rev-parse", ref)
		if err != nil {
			return ""
		}
		return out
	}
}

func gitBranch(root string) string {
	out, err := runGit(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	if out == "HEAD" {
		// Detached HEAD — branch is meaningless, leave empty so the
		// field stays unset rather than carrying a misleading value.
		return ""
	}
	return out
}

func systemUser(_ string) string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("USERNAME") // Windows
}

func runGit(root string, args ...string) (string, error) {
	full := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
