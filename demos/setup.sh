#!/bin/bash
# Sets up a clean wtfc demo workspace at /tmp/wtfc-demo.
# Used by VHS tape recordings so each demo starts from a known state.
#
# We initialise a tiny git repo and enable the git auto-fill sources so
# the TUI shows author/commit metadata on every change — same behaviour
# you'd get on a real project.

set -euo pipefail

DIR="${1:-/tmp/wtfc-demo}"
rm -rf "$DIR"
mkdir -p "$DIR"
cd "$DIR"

# Tiny git repo so git.user / git.sha / git.branch resolve to something
# nice and stable for the demo.
git init -q
git config user.name "Demo Author"
git config user.email "demo@example.com"
git checkout -q -B main 2>/dev/null || git symbolic-ref HEAD refs/heads/main
echo "demo project" > README.md
git add README.md
git commit -q -m "initial commit"

wtfc init >/dev/null

# Replace the scaffolded config with one that enables git auto-fill on
# both changesets and releases. User-supplied values still win, but
# anything left implicit gets stamped from git.
cat > wtfc/config.toml <<'TOML'
[changeset]
fields = [
  { name = "summary",  type = "string", required = true },
  { name = "type",     type = "enum",   values = ["feat", "fix", "chore"], required = true },
  { name = "audience", type = "list",   values = ["public", "internal"] },
  { name = "author",   type = "string", source = "git.user" },
  { name = "commit",   type = "string", source = "git.sha" },
  { name = "branch",   type = "string", source = "git.branch" },
]

[release]
fields = [
  { name = "notes",    type = "string" },
  { name = "audience", type = "list",   values = ["public", "internal"] },
  { name = "commit",   type = "string", source = "git.sha" },
]
TOML

# Seed: cut one prior release so the "released" section has content,
# then queue three pending changes for the next-release medallion.
wtfc change --field summary="add user profile page" --field type=feat --field audience=public >/dev/null
wtfc release v0.1.0 --field notes="first cut" --field audience=public >/dev/null

wtfc change --field summary="add user profile page" --field type=feat --field audience=public >/dev/null
wtfc change --field summary="fix login redirect loop" --field type=fix --field audience=public >/dev/null
wtfc change --field summary="bump grpc to 1.62" --field type=chore --field audience=internal >/dev/null

echo "demo workspace ready at $DIR"
