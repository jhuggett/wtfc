#!/bin/bash
# Drives `wtfc mcp` with two JSON-RPC tool calls and pretty-prints the
# replies. Used by demos/mcp.tape so the GIF demonstrates the server's
# protocol without a real agent host.

set -euo pipefail

PROJECT="${1:-/tmp/wtfc-mcp}"
INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"demo","version":"0"}}}'
INITD='{"jsonrpc":"2.0","method":"notifications/initialized"}'

show_call() {
  local label="$1" req="$2"
  printf "\033[33m→ %s\033[0m\n" "$label"
  {
    echo "$INIT"
    echo "$INITD"
    echo "$req"
    sleep 0.4
  } | wtfc mcp 2>/dev/null | tail -1 | jq -C '.result.structuredContent // .result' | head -25
  echo
}

show_call "get_schema" '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_schema","arguments":{"project_path":"'"$PROJECT"'"}}}'

show_call "list_pending_changes" '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_pending_changes","arguments":{"project_path":"'"$PROJECT"'"}}}'

show_call "propose_change" '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"propose_change","arguments":{"project_path":"'"$PROJECT"'","values":{"summary":"add CSV export","type":"feat","audience":["public"]}}}}'
