// Package hook runs user-defined executables from wtfc/hooks/ after
// significant events (release, unrelease). The hook receives the event
// payload as JSON on stdin and contextual paths as env vars.
package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jhuggett/wtfc/internal/config"
)

const Dir = "hooks"

// Known events. Hook scripts should be placed at wtfc/hooks/<event>.
const (
	EventPostRelease   = "post-release"
	EventPostUnrelease = "post-unrelease"
)

// KnownEvents is the canonical list of events the runtime fires.
var KnownEvents = []string{EventPostRelease, EventPostUnrelease}

func isKnownEvent(name string) bool {
	for _, e := range KnownEvents {
		if e == name {
			return true
		}
	}
	return false
}

// Result describes one hook execution. Result is non-nil even when the hook
// exits with a non-zero status, so callers can surface captured output
// alongside the error.
type Result struct {
	Ran    bool   // false if no hook script exists for this event
	Path   string // resolved path of the hook (empty if !Ran)
	Stdout []byte
	Stderr []byte
}

// HooksDir is the directory hooks are looked up in.
func HooksDir(cfg *config.Config) string {
	return filepath.Join(cfg.Dir(), Dir)
}

// Run looks for wtfc/hooks/<event> and executes it. Missing-or-not-executable
// is a no-op (Result.Ran == false, nil error). Non-zero exit is an error;
// captured stdout/stderr is still returned via Result so the caller can
// display it.
func Run(cfg *config.Config, event string, payload any, extraEnv map[string]string) (*Result, error) {
	path := filepath.Join(HooksDir(cfg), event)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Result{Ran: false}, nil
		}
		return nil, err
	}
	if info.IsDir() {
		return &Result{Ran: false}, nil
	}
	if info.Mode()&0o111 == 0 {
		return &Result{Ran: false, Path: path}, fmt.Errorf(
			"%s hook at %s is not executable — run `chmod +x %s`",
			event, path, path)
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}

	res := &Result{Ran: true, Path: path}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(path)
	cmd.Dir = cfg.ProjectRoot()
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	env := os.Environ()
	env = append(env,
		"WTFC_EVENT="+event,
		"WTFC_PROJECT_ROOT="+cfg.ProjectRoot(),
		"WTFC_DIR="+cfg.Dir(),
		"WTFC_CHANGELOG_PATH="+cfg.ChangelogPath(),
		"WTFC_PENDING_DIR="+cfg.PendingDir(),
	)
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	runErr := cmd.Run()
	res.Stdout = stdout.Bytes()
	res.Stderr = stderr.Bytes()
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			return res, fmt.Errorf("%s hook exited %d", event, ee.ExitCode())
		}
		return res, fmt.Errorf("%s hook: %w", event, runErr)
	}
	return res, nil
}
