package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jhuggett/wtfc/internal/auto"
	"github.com/jhuggett/wtfc/internal/change"
	"github.com/jhuggett/wtfc/internal/config"
)

// cmdMCP starts an MCP stdio server exposing change-related tools so an
// agent host (Claude Code, etc.) can drive the propose/confirm/commit flow
// natively. Releases and unreleases are intentionally not exposed — those
// are human decisions.
// serverInstructions is sent to the client on connect. Hosts surface this
// to the model once per session, so it's the most reliable place to anchor
// the recommended workflow — much harder to skip than a per-tool hint.
const serverInstructions = `wtfc tracks pending changelog entries against a project-defined schema. Recommended workflow:

1. Call get_schema first to learn what fields exist and what values are valid (especially enums and lists).
2. Call propose_change to build a record from the user's request without writing it. Show the structured record back to the user — call out any field you inferred or left null — and confirm before persisting.
3. Call create_change only after confirmation. Treat it as the commit step.
4. To adjust a field on an already-created change, use update_change. Do not delete_change + create_change for edits — that churns the id and created_at.

Skip the propose step only when the user has explicitly specified every schema field they care about. When in doubt, propose first.`

func cmdMCP(_ []string) error {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "wtfc", Version: "0.1.0"},
		&mcp.ServerOptions{Instructions: serverInstructions},
	)

	registerSchemaTool(server)
	registerListPendingTool(server)
	registerProposeTool(server)
	registerCreateTool(server)
	registerUpdateTool(server)
	registerDeleteTool(server)

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

// projectPathField is embedded in every tool's args struct so an agent can
// (and usually should) tell the server which project to act on. If empty,
// the server falls back to its own cwd.
type projectPathField struct {
	ProjectPath string `json:"project_path,omitempty" jsonschema:"absolute path to the project root (or any directory below it). Defaults to the server's current working directory if omitted, but agents should set this explicitly."`
}

// loadConfig finds wtfc/config.toml for a tool call. If projectPath is
// empty, walks up from the server's cwd; otherwise walks up from the path.
func loadConfig(projectPath string) (*config.Config, error) {
	if projectPath == "" {
		return config.LoadFromCwd()
	}
	p, err := config.FindUp(projectPath)
	if err != nil {
		return nil, fmt.Errorf("no wtfc/config.toml found at or above %s", projectPath)
	}
	return config.Load(p)
}

// ── schema ────────────────────────────────────────────────────────────────

type schemaArgs struct {
	projectPathField
}

type schemaResult struct {
	Changeset section `json:"changeset"`
	Release   section `json:"release"`
}

type section struct {
	Fields []config.Field `json:"fields"`
}

func registerSchemaTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_schema",
		Description: "Return the changeset and release schemas defined in wtfc/config.toml. " +
			"Call this before propose_change or create_change so you know which fields exist and which enum/list values are valid — otherwise you can't tell what's missing from the user's request.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, args schemaArgs) (*mcp.CallToolResult, schemaResult, error) {
		cfg, err := loadConfig(args.ProjectPath)
		if err != nil {
			return nil, schemaResult{}, err
		}
		return nil, schemaResult{
			Changeset: section{Fields: cfg.Changeset.Fields},
			Release:   section{Fields: cfg.Release.Fields},
		}, nil
	})
}

// ── list_pending_changes ──────────────────────────────────────────────────

type listArgs struct {
	projectPathField
}

// listResult wraps the slice in a record (MCP requires structuredContent to
// be an object) and uses []map[string]any so the inferred output schema
// allows the dynamic, schema-driven fields on each change.
type listResult struct {
	Changes []map[string]any `json:"changes"`
}

func registerListPendingTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_pending_changes",
		Description: "List all pending change files queued for the next release.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, listResult, error) {
		cfg, err := loadConfig(args.ProjectPath)
		if err != nil {
			return nil, listResult{}, err
		}
		changes, _, err := change.List(cfg)
		if err != nil {
			return nil, listResult{}, err
		}
		items := make([]map[string]any, 0, len(changes))
		for _, c := range changes {
			data, err := json.Marshal(c)
			if err != nil {
				return nil, listResult{}, err
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, listResult{}, err
			}
			items = append(items, m)
		}
		return nil, listResult{Changes: items}, nil
	})
}

// ── propose_change / create_change ────────────────────────────────────────

type changeArgs struct {
	projectPathField
	Values map[string]any `json:"values" jsonschema:"the field values for the change, keyed by field name. Call get_schema first to discover valid field names and value types."`
}

func registerProposeTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "propose_change",
		Description: "Build a change record without writing it to disk. Use this whenever the user's request leaves any field implicit — inferred values, null fields, or anything you'd have to guess. Show the returned record to the user, call out what you filled in or left null, and only call create_change after they confirm.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, args changeArgs) (*mcp.CallToolResult, any, error) {
		cfg, err := loadConfig(args.ProjectPath)
		if err != nil {
			return nil, nil, err
		}
		c := change.NewFromValues(cfg, args.Values)
		// Run auto-fill so the preview reflects what create_change will
		// actually write — otherwise the agent sees null author/commit
		// and might try to fill them itself.
		auto.Resolve(cfg.ProjectRoot(), cfg.Changeset.Fields, c.Fields)
		return resultWithJSONText(c)
	})
}

func registerCreateTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_change",
		Description: "Write a change file to wtfc/pending/. Returns the canonical record including its assigned id and creation timestamp. " +
			"Prefer calling propose_change first and confirming with the user. Skip the preview only when every schema field the user cares about has been explicitly specified — otherwise you'll write the wrong values and need a delete or update to fix it. " +
			"Hosts may want to require user confirmation for this tool since it mutates the project.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, args changeArgs) (*mcp.CallToolResult, any, error) {
		cfg, err := loadConfig(args.ProjectPath)
		if err != nil {
			return nil, nil, err
		}
		c := change.NewFromValues(cfg, args.Values)
		if err := c.Write(cfg); err != nil {
			return nil, nil, err
		}
		return resultWithJSONText(c)
	})
}

// ── update_change ─────────────────────────────────────────────────────────

type updateArgs struct {
	projectPathField
	ID     string         `json:"id" jsonschema:"the id (full UUID or unique prefix) of the pending change to update"`
	Values map[string]any `json:"values" jsonschema:"partial field values to merge. Provided keys overwrite, absent keys are left untouched. Pass null to clear a field. id and created_at are read-only."`
}

func registerUpdateTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "update_change",
		Description: "Merge field values into an existing pending change in place. Preserves id and created_at. " +
			"Always prefer this over delete_change + create_change when adjusting fields — the latter churns the id and timestamp for what is logically the same change.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, args updateArgs) (*mcp.CallToolResult, any, error) {
		cfg, err := loadConfig(args.ProjectPath)
		if err != nil {
			return nil, nil, err
		}
		c, _, err := change.FindByID(cfg, args.ID)
		if err != nil {
			return nil, nil, err
		}
		c.Apply(args.Values)
		if err := c.Write(cfg); err != nil {
			return nil, nil, err
		}
		return resultWithJSONText(c)
	})
}

// ── delete_change ─────────────────────────────────────────────────────────

type deleteArgs struct {
	projectPathField
	ID string `json:"id" jsonschema:"the id (full UUID or unique prefix) of the pending change to delete"`
}

type deleteResult struct {
	Removed string `json:"removed"`
}

func registerDeleteTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_change",
		Description: "Remove a pending change file by id. Use only to discard a change entirely (e.g. undoing a mistaken create_change). To change a field value on an existing change, use update_change instead.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, args deleteArgs) (*mcp.CallToolResult, deleteResult, error) {
		cfg, err := loadConfig(args.ProjectPath)
		if err != nil {
			return nil, deleteResult{}, err
		}
		_, path, err := change.FindByID(cfg, args.ID)
		if err != nil {
			return nil, deleteResult{}, err
		}
		if err := os.Remove(path); err != nil {
			return nil, deleteResult{}, err
		}
		return nil, deleteResult{Removed: path}, nil
	})
}

// resultWithJSONText returns the value as both a TextContent block (so the
// agent sees pretty JSON it can show the user) and the structured payload.
func resultWithJSONText(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, v, nil
}
