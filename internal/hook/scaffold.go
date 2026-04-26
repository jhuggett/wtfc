package hook

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed scaffold/post-release.sample
var scaffoldSample []byte

// Scaffold writes a sample post-release hook into the given hooks directory.
// The sample is intentionally not marked executable so it won't auto-fire —
// the user opts in by renaming it and chmod+x'ing it.
func Scaffold(hooksDir string) error {
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(hooksDir, "post-release.sample"), scaffoldSample, 0o644)
}
