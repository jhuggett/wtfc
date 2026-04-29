Output demos/cli.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 720
Set Padding 20
Set TypingSpeed 35ms
Set PlaybackSpeed 1.0

# Start in a clean directory.
Hide
Type "rm -rf /tmp/wtfc-cli && mkdir /tmp/wtfc-cli && cd /tmp/wtfc-cli && clear"
Enter
Show
Sleep 800ms

# 1. Scaffold the project.
Type "wtfc init"
Enter
Sleep 1500ms

# 2. Track a couple of changes.
Type `wtfc change --field summary="add user profile page" --field type=feat --field audience=public`
Enter
Sleep 1200ms

Type `wtfc change --field summary="fix login redirect loop" --field type=fix --field audience=public`
Enter
Sleep 1200ms

Type `wtfc change --field summary="bump grpc to 1.62" --field type=chore --field audience=internal`
Enter
Sleep 1200ms

# 3. List what's pending.
Type "wtfc list"
Enter
Sleep 2500ms

# 4. Cut a release.
Type `wtfc release v0.1.0 --field notes="first public cut" --field audience=public`
Enter
Sleep 2000ms

# 5. Show history.
Type "wtfc history"
Enter
Sleep 2500ms
