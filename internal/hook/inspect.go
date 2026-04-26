package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jhuggett/wtfc/internal/config"
)

// Status classifies a file in the hooks directory.
type Status int

const (
	// StatusEnabled — filename matches a known event AND is executable.
	StatusEnabled Status = iota
	// StatusNotExecutable — filename matches a known event but isn't executable.
	StatusNotExecutable
	// StatusSample — filename ends with `.sample`. Won't fire; meant to be enabled.
	StatusSample
	// StatusUnknown — file in hooks/ that doesn't match a known event and isn't a sample.
	StatusUnknown
)

func (s Status) String() string {
	switch s {
	case StatusEnabled:
		return "enabled"
	case StatusNotExecutable:
		return "not executable"
	case StatusSample:
		return "sample"
	case StatusUnknown:
		return "unknown event"
	}
	return ""
}

// Entry describes one file in the hooks directory.
type Entry struct {
	Name   string // base filename
	Path   string // absolute path
	Event  string // resolved event name (filename minus .sample); "" if unknown
	Status Status
}

// CanEnable reports whether `wtfc hooks enable` would do something to this entry.
func (e Entry) CanEnable() bool {
	return e.Status == StatusSample || e.Status == StatusNotExecutable
}

// List returns every file in wtfc/hooks/, classified. README files and
// subdirectories are skipped.
func List(cfg *config.Config) ([]Entry, error) {
	dir := HooksDir(cfg)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if strings.EqualFold(name, "README.md") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := de.Info()
		if err != nil {
			return nil, err
		}
		event := name
		isSample := strings.HasSuffix(name, ".sample")
		if isSample {
			event = strings.TrimSuffix(name, ".sample")
		}
		var status Status
		switch {
		case isSample:
			status = StatusSample
		case !isKnownEvent(event):
			status = StatusUnknown
		case info.Mode()&0o111 == 0:
			status = StatusNotExecutable
		default:
			status = StatusEnabled
		}
		if !isKnownEvent(event) && !isSample {
			event = ""
		}
		out = append(out, Entry{Name: name, Path: path, Event: event, Status: status})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Enable promotes a sample / non-executable hook into a working one. For
// `.sample` files it strips the suffix; in all cases it sets the executable
// bit. Returns the resolved (post-rename) path.
func Enable(entry Entry) (string, error) {
	path := entry.Path
	if entry.Status == StatusSample {
		newPath := strings.TrimSuffix(path, ".sample")
		if _, err := os.Stat(newPath); err == nil {
			return "", fmt.Errorf("%s already exists; refusing to overwrite", newPath)
		}
		if err := os.Rename(path, newPath); err != nil {
			return "", err
		}
		path = newPath
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(path, info.Mode()|0o111); err != nil {
		return "", err
	}
	return path, nil
}
