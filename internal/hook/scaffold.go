package hook

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed scaffold/on-release-changed.sample
var scaffoldSample []byte

// Scaffold writes a sample on-release-changed hook into the given hooks
// directory. on-release-changed is the most useful default because it
// fires on both release and unrelease — regenerators stay in sync
// without per-event glue. The sample is intentionally not marked
// executable so it won't auto-fire; the user opts in by renaming it and
// chmod+x'ing it.
func Scaffold(hooksDir string) error {
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(hooksDir, "on-release-changed.sample"), scaffoldSample, 0o644)
}
