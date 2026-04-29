# Changelog

> Newest first.

## v0.2.0 — 2026-04-29

> 2 changes · by Joel Huggett · `ffc8af2`

hook + install docs

### Fixes

- use annotated tags in on-release-changed hook so `git push --follow-tags` actually pushes them <kbd>internal</kbd>  
  <sub>Joel Huggett · `6cecc35`</sub>

### Chores

- lead the README Install section with `brew install jhuggett/tap/wtfc` and add a concrete curl|tar fallback for when brew isn't available  
  <sub>Joel Huggett · `6cecc35`</sub>

---

## v0.1.0 — 2026-04-29

> 7 changes · by Joel Huggett · `deb9c5d`

first published cut

### Features

- wire up GoReleaser + GitHub Actions release workflow + Homebrew tap (jhuggett/homebrew-tap) — `wtfc release vX.Y.Z` auto-tags via the hook and the workflow ships binaries  
  <sub>Joel Huggett · `2ffcadf`</sub>

### Fixes

- wrap long summaries inside expanded release box instead of stretching it horizontally
- run auto-fill in MCP propose_change so the preview reflects what create_change will write  
  <sub>Joel Huggett · `7c8d750`</sub>

### Chores

- fancy CHANGELOG.md hook: type-grouped sections, per-change author/commit, audience tags, release meta line <kbd>internal</kbd>
- document release-side auto-fill, CHANGELOG.md sample output, and link to dogfooded CHANGELOG.md in README
- regenerate VHS demos against the auto-fill schema so cards show author/commit metadata
- record live Claude Code session calling wtfc MCP (claude.gif) — real agent, real tool calls  
  <sub>Joel Huggett · `7c8d750`</sub>

---

## 1.4 — 2026-04-29

> 7 changes · by Joel Huggett · `7c8d750`

### Features

- add source schema attribute for declaring auto-fill providers on fields  
  <sub>Joel Huggett · `7c8d750`</sub>
- build internal/auto package with git and system source resolvers <kbd>internal</kbd>  
  <sub>Joel Huggett · `7c8d750`</sub>
- wire auto.Resolve into change.Write and release.Cut so every entry point auto-fills uniformly <kbd>internal</kbd>  
  <sub>Joel Huggett · `7c8d750`</sub>
- pre-fill TUI form modal with auto-resolved values so users see git context before submit  
  <sub>Joel Huggett · `7c8d750`</sub>

### Chores

- add author/commit/branch auto-fields to wtfc's own changeset and release schema <kbd>internal</kbd>  
  <sub>Joel Huggett · `7c8d750`</sub>
- document source attribute and built-in auto-fill sources in README and init scaffold  
  <sub>Joel Huggett · `7c8d750`</sub>
- add VHS-recorded TUI/CLI/MCP demos and rewrite README around them  
  <sub>Joel Huggett · `7c8d750`</sub>

---

## 1.3 — 2026-04-28

> 14 changes

### Features

- ship MCP server with full change-management toolset (get_schema, list_pending_changes, propose_change, create_change, update_change, delete_change)
- simplify hooks to a single post-changelog event with WTFC_OP env var
- scaffolded post-changelog hook regenerates CHANGELOG.md from authoritative state, idempotent under release and unrelease
- redesign TUI as a centered vertical timeline with track/cut fork, pending and released sections
- render TUI forms as a centered modal floating over a dimmed timeline backdrop
- add bottom-right toast notifications with 3-second auto-dismiss
- single-expand release mode where the medallion morphs into a unified container holding its changes
- edit and delete pending changes directly from the timeline (e/d/enter)
- confirmation modal before destructive actions like unrelease
- schema `required` flag enforced uniformly from TUI/CLI/MCP via shared validator at the write boundary
- `wtfc change edit <id>` CLI subcommand for partial-update edits preserving id and created_at

### Fixes

- list_pending_changes structuredContent must be a record, not an array <kbd>internal</kbd>

### Chores

- tighten MCP tool descriptions and add server-level workflow instructions <kbd>internal</kbd>
- gitignore bin/ and untrack the built binary <kbd>internal</kbd>

---

## 1.2 — 2026-04-28

> 3 changes

### Features

- test

### Fixes

- test <kbd>internal</kbd>

### Other

- somethingg

---

## 1.1 — 2026-04-28

> 3 changes

### Features

- this is a test <kbd>internal</kbd>
- 123

### Fixes

- abc <kbd>internal</kbd>

---

## 1.0 — 2026-04-27

> 4 changes

### Features

- ship MCP server with update_change tool
- simplify hooks to single post-changelog event
- redesign TUI as a centered vertical timeline

### Fixes

- fix list_pending_changes structuredContent schema <kbd>internal</kbd>

---

## test — 2026-04-26

> 2 changes

### Features

- test

### Other

- 11b6fbd0-177a-4832-9b32-90c071def61e

