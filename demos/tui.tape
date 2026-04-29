Output demos/tui.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 900
Set Padding 20
Set TypingSpeed 40ms

# Setup a known demo workspace and launch the TUI hidden so the gif
# starts on the timeline view.
Hide
Type "demos/setup.sh /tmp/wtfc-tui >/dev/null && cd /tmp/wtfc-tui && clear && wtfc"
Enter
Sleep 1500ms
Show

Sleep 2s

# Browse the queue.
Down
Sleep 700ms
Down
Sleep 700ms
Down
Sleep 700ms
Down
Sleep 700ms
Down
Sleep 1s

# Open the focused release.
Enter
Sleep 2500ms

# Collapse and head back up to the fork.
Enter
Sleep 800ms
Up
Sleep 400ms
Up
Sleep 400ms
Up
Sleep 400ms
Up
Sleep 400ms
Up
Sleep 800ms

# Track a new change via the modal. The author/commit/branch fields are
# pre-filled from git auto-fill, so we just tab past them on the way to
# submit.
Type "t"
Sleep 1200ms
Type "tighten error message on auth failure"
Sleep 800ms
Tab           # → type
Sleep 500ms
Right
Sleep 300ms
Space
Sleep 500ms
Tab           # → audience
Sleep 500ms
Space
Sleep 500ms
Tab           # → author (auto-filled)
Sleep 900ms
Tab           # → commit (auto-filled)
Sleep 900ms
Tab           # → branch (auto-filled)
Sleep 900ms
Tab           # → submit
Sleep 600ms
Enter
Sleep 2500ms

# Cross over to the cut-release affordance.
Right
Sleep 1500ms

Type "q"
Sleep 500ms
