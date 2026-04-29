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
  wtfc                            launch the TUI
  wtfc init                       scaffold wtfc/ (config.toml + pending/) in the cwd
  wtfc schema [--json]            print the changeset + release schema
  wtfc change [flags]             create a new pending change file
  wtfc change show <id> [--json]  print one pending change
  wtfc change edit <id> [flags]   merge field values into a pending change
  wtfc change rm <id>             delete one pending change
  wtfc list [--json]              list pending change files
  wtfc release <name> [flags]     collapse pending changes into changelog.json
  wtfc unrelease                  pop the last release and restore its changes
  wtfc history [--json]           print previous releases (newest first)
  wtfc help                       this message

Flags for change/release:
  --field key=value               may be repeated; sets a schema field
  --json '{...}'                  bulk values as a JSON object (typed)
  --dry-run                       (change) validate + print what would be saved; do not write

Other:
  --json                          (list/history/schema/change show) emit JSON to stdout

Agent integration:
  wtfc mcp                        start an MCP stdio server for first-party agent use
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
	case "schema":
		return cmdSchema(args[1:])
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
	case "mcp":
		return cmdMCP(args[1:])
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
# Add new fields any time; old data is preserved.
#
# Field attributes:
#   required = true        must be set at create / release time
#   source   = "name"      auto-fill from a built-in source if unset
#                          (git.user, git.email, git.sha, git.branch,
#                          system.user). User-provided values always win.
#
# Same rules enforced from the TUI, CLI, and MCP server.

[changeset]
fields = [
  { name = "summary",  type = "string", required = true },
  { name = "type",     type = "enum",   values = ["feat", "fix", "chore"], required = true },
  { name = "audience", type = "list",   values = ["public", "internal"] },
  # Uncomment to auto-attach git context to each change:
  # { name = "author",  type = "string", source = "git.user" },
  # { name = "commit",  type = "string", source = "git.sha" },
  # { name = "branch",  type = "string", source = "git.branch" },
]

[release]
fields = [
  { name = "notes",    type = "string" },
  { name = "audience", type = "list",   values = ["public", "internal"] },
  # { name = "commit", type = "string", source = "git.sha" },
]
`
	if err := os.WriteFile(cfgPath, []byte(starter), 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s/ with config.toml, pending/, and hooks/on-release-changed.sample\n", config.DirName)
	return nil
}

func cmdChange(args []string) error {
	// Subcommands: `wtfc change show <id>` and `wtfc change rm <id>`.
	// If the first arg is a known subcommand, dispatch; otherwise treat
	// all args as flags for the create flow.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "show":
			return cmdChangeShow(args[1:])
		case "edit":
			return cmdChangeEdit(args[1:])
		case "rm":
			return cmdChangeRm(args[1:])
		default:
			return fmt.Errorf("unknown change subcommand %q (try `show`, `edit`, `rm`, or pass --field/--json to create)", args[0])
		}
	}
	return cmdChangeCreate(args)
}

func cmdChangeCreate(args []string) error {
	fs := flag.NewFlagSet("change", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stringFields := fieldFlag{}
	jsonBlob := fs.String("json", "", `JSON object of field values, e.g. '{"summary":"…","type":"feat"}'`)
	dryRun := fs.Bool("dry-run", false, "validate + print what would be saved; do not write")
	fs.Var(stringFields, "field", "key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}

	// Build typed values: --json first, --field overrides on top.
	values := map[string]any{}
	if strings.TrimSpace(*jsonBlob) != "" {
		if err := json.Unmarshal([]byte(*jsonBlob), &values); err != nil {
			return fmt.Errorf("--json: %w", err)
		}
	}
	coerced, err := change.CoerceFields(cfg.Changeset.Fields, stringFields)
	if err != nil {
		return err
	}
	for k, v := range coerced {
		values[k] = v
	}

	c := change.NewFromValues(cfg, values)
	if *dryRun {
		// Preview only; render the canonical record without writing.
		data, _ := json.MarshalIndent(c, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	if err := c.Write(cfg); err != nil {
		return err
	}
	fmt.Println(c.Path(cfg))
	return nil
}

func cmdChangeShow(args []string) error {
	fs := flag.NewFlagSet("change show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "emit JSON to stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("id required: wtfc change show <id>")
	}
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}
	c, path, err := change.FindByID(cfg, rest[0])
	if err != nil {
		return err
	}
	if *asJSON {
		data, _ := json.MarshalIndent(c, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	fmt.Println(path)
	// Schema fields in declared order, then any extras at the end.
	seen := map[string]bool{}
	for _, f := range cfg.Changeset.Fields {
		seen[f.Name] = true
		fmt.Printf("  %s = %v\n", f.Name, c.Fields[f.Name])
	}
	for k, v := range c.Fields {
		if !seen[k] {
			fmt.Printf("  %s = %v\n", k, v)
		}
	}
	return nil
}

func cmdChangeEdit(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("id required: wtfc change edit <id> [--field key=value | --json '{...}']")
	}
	id := args[0]
	fs := flag.NewFlagSet("change edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stringFields := fieldFlag{}
	jsonBlob := fs.String("json", "", `JSON object of field values to merge, e.g. '{"audience":["public"]}'`)
	fs.Var(stringFields, "field", "key=value (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}

	values := map[string]any{}
	if strings.TrimSpace(*jsonBlob) != "" {
		if err := json.Unmarshal([]byte(*jsonBlob), &values); err != nil {
			return fmt.Errorf("--json: %w", err)
		}
	}
	coerced, err := change.CoerceFields(cfg.Changeset.Fields, stringFields)
	if err != nil {
		return err
	}
	for k, v := range coerced {
		values[k] = v
	}
	if len(values) == 0 {
		return fmt.Errorf("no values provided (use --field or --json)")
	}

	c, _, err := change.FindByID(cfg, id)
	if err != nil {
		return err
	}
	c.Apply(values)
	if err := c.Write(cfg); err != nil {
		return err
	}
	fmt.Println(c.Path(cfg))
	return nil
}

func cmdChangeRm(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("id required: wtfc change rm <id>")
	}
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}
	_, path, err := change.FindByID(cfg, args[0])
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	fmt.Printf("removed %s\n", path)
	return nil
}

func cmdSchema(_ []string) error {
	cfg, err := config.LoadFromCwd()
	if err != nil {
		return err
	}
	out := map[string]any{
		"changeset": map[string]any{"fields": cfg.Changeset.Fields},
		"release":   map[string]any{"fields": cfg.Release.Fields},
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	return nil
}

// cmdMCP starts the MCP stdio server. Defined in mcp.go.
// (Wired here so the dispatcher compiles before the MCP server lands.)

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
	return runChangelogHookCLI(cfg, hook.OpRelease, rel.Name)
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
	return runChangelogHookCLI(cfg, hook.OpUnrelease, rel.Name)
}

// runChangelogHookCLI fires on-release-changed with the full Changelog JSON as
// payload. WTFC_OP tells the hook whether a release was added or removed;
// hooks that only care about one direction branch on it.
func runChangelogHookCLI(cfg *config.Config, op, releaseName string) error {
	cl, err := release.Load(cfg)
	if err != nil {
		return err
	}
	return runHookCLI(cfg, hook.EventOnReleaseChanged, cl, map[string]string{
		"WTFC_OP":           op,
		"WTFC_RELEASE_NAME": releaseName,
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
