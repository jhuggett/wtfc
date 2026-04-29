Output demos/claude.gif

Set Shell "bash"
Set FontSize 13
Set Width 1400
Set Height 1000
Set Padding 20
Set TypingSpeed 35ms

# Set up the demo project and cd into it before recording starts.
Hide
Type "demos/setup.sh /tmp/wtfc-claude >/dev/null && cd /tmp/wtfc-claude && clear"
Enter
Sleep 1500ms
Show
Sleep 400ms

# Launch Claude Code with the wtfc MCP server attached and the change
# tools pre-allowed so the demo doesn't pause on permission prompts.
Type `claude --mcp-config '{"mcpServers":{"wtfc":{"command":"wtfc","args":["mcp"]}}}' --allowedTools "mcp__wtfc__get_schema,mcp__wtfc__propose_change,mcp__wtfc__create_change"`
Enter
Sleep 3s

# Approve the "trust this folder" safety prompt.
Enter
Sleep 2500ms

# Turn 1: ask Claude to track the change.
Type "I just shipped a dark-mode toggle for the dashboard. Track it as a public feat in wtfc — propose it first so I can review."
Enter
Sleep 25s

# Turn 2: confirm, Claude commits via create_change.
Type "yep, ship it"
Enter
Sleep 20s

# Quit claude.
Ctrl+c
Sleep 400ms
Ctrl+c
Sleep 800ms
