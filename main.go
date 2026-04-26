package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/jhuggett/wtfc/internal/change"
	"github.com/jhuggett/wtfc/internal/config"
	"github.com/jhuggett/wtfc/internal/hook"
	"github.com/jhuggett/wtfc/internal/release"
	"github.com/jhuggett/wtfc/internal/tui"
)

const usage = `wtfc — Work, Track, Format, Changelog

Usage:
  wtfc                       launch the TUI
  wtfc init                  scaffold wtfc/ (config.toml + pending/) in the cwd
  wtfc change [flags]        create a new pending change file
  wtfc list [--json]         list pending change files
  wtfc release <name> [flags]  collapse pending changes into changelog.json
  wtfc unrelease             pop the last release and restore its changes
  wtfc history [--json]      print previous releases (newest first)
  wtfc help                  this message

Flags:
  --field key=value          may be repeated (change, release): sets a schema field
  --json '{...}'             release: bulk metadata as a JSON object
                             change/list/history: emit JSON to stdout
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wtfc: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return tui.Run()
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "init":
		return cmdInit()
	case "change":
		return cmdChange(args[1:])
	case "list":
		return cmdList(args[1:])
	case "release":
		return cmdRelease(args[1:])
	case "unrelease":
		return cmdUnrelease(args[1:])
	case "history":
		return cmdHistory(args[1:])
	default:
		return fmt.Errorf("unknown command %q (try `wtfc help`)", args[0])
	}
}

// fieldFlag accumulates repeated `--field key=value` arguments.
type fieldFlag map[string]string

func (f fieldFlag) String() string { return "" }
func (f fieldFlag) Set(s string) error {
	i := strings.IndexByte(s, '=')
	if i <= 0 {
		return fmt.Errorf("expected key=value, got %q", s)
	}
	f[s[:i]] = s[i+1:]
	return nil
}

func cmdInit() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir := filepath.Join(cwd, config.DirName)
	cfgPath := filepath.Join(dir, config.ConfigFile)
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("%s already exists", filepath.Join(config.DirName, config.ConfigFile))
	}
	if err := os.MkdirAll(filepath.Join(dir, "pending"), 0o755); err != nil {
		return err
	}
	if err := hook.Scaffold(filepath.Join(dir, hook.Dir)); err != nil {
		return err
	}
	starter := `# wtfc config — defines the schema for changes and releases.
# All fields are optional. Add new ones any time; old data is preserved.

[changeset]
fields = [
  { name = "summary",  type = "string" },
  { name = "type",     type = "enum", values = ["feat", "fix", "chore"] },
  { name = "audience", type = "list", values = ["public", "internal"] },
]

[release]
fields = [
  { name = "notes",    type = "string" },
  { name = "audience", type = "list", values = ["public", "internal"] },
]
`
	if err := os.WriteFile(cfgPath, []byte(starter), 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s/ with config.toml, pending/, and hooks/post-release.sample\n", config.DirName)
	return nil
}

func cmdChange(args []string) error {
	fs := flag.NewFlagSet("change", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fields := fieldFlag{}
	asJSON := fs.Bool("json", false, "emit the new change as JSON to stdout")
	fs.Var(fields, "field", "key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}
	c, err := change.New(cfg, fields)
	if err != nil {
		return err
	}
	if err := c.Write(cfg); err != nil {
		return err
	}
	if *asJSON {
		data, _ := json.MarshalIndent(c, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("created %s\n", c.Path(cfg))
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "emit pending changes as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}
	changes, paths, err := change.List(cfg)
	if err != nil {
		return err
	}
	if *asJSON {
		data, _ := json.MarshalIndent(changes, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	if len(changes) == 0 {
		fmt.Println("no pending changes")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CREATED\tID\tSUMMARY\tFILE")
	for i, c := range changes {
		summary := ""
		if v, ok := c.Fields["summary"].(string); ok {
			summary = v
		}
		short := c.ID
		if len(short) > 8 {
			short = short[:8]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			c.CreatedAt.Local().Format("2006-01-02 15:04"),
			short, summary, filepath.Base(paths[i]))
	}
	return tw.Flush()
}

func cmdRelease(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("release name required: wtfc release <name> [flags]")
	}
	name := args[0]
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stringFields := fieldFlag{}
	jsonBlob := fs.String("json", "", `JSON object of release metadata, e.g. '{"beta":true}'`)
	fs.Var(stringFields, "field", "key=value (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}

	// Build typed metadata: --json first, then --field overrides on top.
	fields := map[string]any{}
	if strings.TrimSpace(*jsonBlob) != "" {
		if err := json.Unmarshal([]byte(*jsonBlob), &fields); err != nil {
			return fmt.Errorf("--json: %w", err)
		}
	}
	coerced, err := release.CoerceFields(cfg.Release.Fields, stringFields)
	if err != nil {
		return err
	}
	for k, v := range coerced {
		fields[k] = v
	}

	cl, err := release.Load(cfg)
	if err != nil {
		return err
	}
	if hist := cl.History(); len(hist) > 0 {
		fmt.Println("previous releases:")
		for i, r := range hist {
			if i >= 5 {
				break
			}
			fmt.Printf("  %s  (%s)\n", r.Name, r.ReleasedAt.Local().Format("2006-01-02"))
		}
		fmt.Println()
	}
	rel, err := release.Cut(cfg, name, fields)
	if err != nil {
		return err
	}
	fmt.Printf("released %s with %d change(s) → %s\n",
		rel.Name, len(rel.Changes), cfg.ChangelogPath())
	return runHookCLI(cfg, hook.EventPostRelease, rel, map[string]string{
		"WTFC_RELEASE_NAME": rel.Name,
	})
}

func cmdUnrelease(_ []string) error {
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}
	rel, err := release.Unrelease(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("unreleased %s — %d change(s) restored to %s\n",
		rel.Name, len(rel.Changes), cfg.PendingDir())
	return runHookCLI(cfg, hook.EventPostUnrelease, rel, map[string]string{
		"WTFC_RELEASE_NAME": rel.Name,
	})
}

// runHookCLI runs a hook and forwards its captured stdio to the user's
// terminal so any progress messages or errors are visible.
func runHookCLI(cfg *config.Config, event string, payload any, env map[string]string) error {
	res, err := hook.Run(cfg, event, payload, env)
	if res != nil && res.Ran {
		os.Stdout.Write(res.Stdout)
		os.Stderr.Write(res.Stderr)
	}
	return err
}

func cmdHistory(args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "emit history as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}
	cl, err := release.Load(cfg)
	if err != nil {
		return err
	}
	if *asJSON {
		data, _ := json.MarshalIndent(cl, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	hist := cl.History()
	if len(hist) == 0 {
		fmt.Println("no releases yet")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RELEASED\tNAME\tCHANGES")
	for _, r := range hist {
		fmt.Fprintf(tw, "%s\t%s\t%d\n",
			r.ReleasedAt.Local().Format("2006-01-02 15:04"),
			r.Name, len(r.Changes))
	}
	return tw.Flush()
}
