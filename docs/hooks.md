# wtfc hooks

Hooks are user-defined executables that run automatically after wtfc events.
They're how you turn the structured `changelog.json` into anything else —
a markdown file, a Slack post, a deploy trigger, an email.

wtfc owns the trigger. You own the logic.

## How a hook is recognised

A file at `wtfc/hooks/<event>` that is executable. That's it.

- No extension required. Add a shebang (`#!/bin/bash`, `#!/usr/bin/env python3`,
  `#!/usr/bin/env node`, …) and `chmod +x`.
- If the file is missing or not executable, the hook is skipped silently.
- Files with any other name (e.g. `post-release.sample`) are ignored.

## Events

| Event             | Fires after                              |
| ----------------- | ---------------------------------------- |
| `post-release`    | `wtfc release <name>` succeeds.          |
| `post-unrelease`  | `wtfc unrelease` succeeds.               |

The release record is fully committed to `changelog.json` *before* the hook
runs. A failing hook does not roll the release back — it just surfaces the
error.

## What the hook receives

**Stdin**: the full release JSON object. Same shape as one entry in
`changelog.json`. For `post-unrelease` it's the release that was just popped.

```json
{
  "name": "v1.2.0",
  "released_at": "2026-04-25T17:00:00Z",
  "notes": "...",
  "audience": ["public"],
  "changes": [
    { "id": "...", "created_at": "...", "summary": "...", "type": "feat" },
    ...
  ]
}
```

**Working directory**: the project root (the parent of `wtfc/`). Relative
paths in your script resolve from there.

**Environment variables**:

| Variable                | Value                                             |
| ----------------------- | ------------------------------------------------- |
| `WTFC_EVENT`            | the event name (e.g. `post-release`)              |
| `WTFC_RELEASE_NAME`     | the release name                                  |
| `WTFC_PROJECT_ROOT`     | absolute path to the project root                 |
| `WTFC_DIR`              | absolute path to the `wtfc/` dir                  |
| `WTFC_CHANGELOG_PATH`   | absolute path to `changelog.json`                 |
| `WTFC_PENDING_DIR`      | absolute path to `wtfc/pending/`                  |

## Failure semantics

- Exit `0`: success. wtfc continues normally.
- Exit non-zero: wtfc exits with code 1 and forwards your hook's stderr.
  The release itself **is not** rolled back. The hook is for side-effects
  (rendering, notifying); the changelog is the source of truth.

## Getting started

A `post-release.sample` is included that prepends a markdown section to
`CHANGELOG.md` at the project root. To enable it:

```sh
mv wtfc/hooks/post-release.sample wtfc/hooks/post-release
chmod +x wtfc/hooks/post-release
```

Edit it freely — it's yours.

## Tips

- Use `jq` (or your favourite JSON tool) to slice the payload.
- For multiple output formats (public CHANGELOG.md + internal one), filter
  on whatever metadata you defined in `[changeset]` / `[release]` in
  `config.toml` — e.g. `audience`, `kind`, `prerelease`.
- Hooks run synchronously and block the wtfc command. For long-running work
  (deploys, emails), `nohup`/`disown` it inside the hook and exit 0 fast.
