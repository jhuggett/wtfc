Output demos/mcp.gif

Set Shell "bash"
Set FontSize 13
Set Width 1200
Set Height 900
Set Padding 20
Set TypingSpeed 30ms

Hide
Type "demos/setup.sh /tmp/wtfc-mcp >/dev/null && cd /Users/jhuggett/Documents/Sandbox/wtfc && clear"
Enter
Sleep 1500ms
Show
Sleep 800ms

Type "# wtfc mcp speaks JSON-RPC over stdio. Here's what an agent host sees."
Enter
Sleep 1500ms

Type "demos/mcp-demo.sh /tmp/wtfc-mcp"
Enter
Sleep 8s
