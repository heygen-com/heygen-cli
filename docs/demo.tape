# Demo recording for README — agent-flavored: schema introspection + JSON pipe
# Run: HEYGEN_API_KEY=<key> vhs docs/demo.tape → produces docs/demo.gif
# Note: this fires a real video-agent generation job each run.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1100
Set Height 600
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Padding 20

# Hidden setup — fail fast if HEYGEN_API_KEY isn't set in the parent env
Hide
Type "export HEYGEN_API_KEY=${HEYGEN_API_KEY:?set HEYGEN_API_KEY before running vhs}"
Enter
Type "clear"
Enter
Show

Sleep 500ms

# Introspect the request shape — no auth, no API call
Type "heygen video-agent create --request-schema | jq '.properties | keys'"
Sleep 300ms
Enter
Sleep 3000ms

# Real call — JSON on stdout, piped straight into jq
Type "heygen video-agent create --prompt 'Welcome to the HeyGen CLI' | jq '.data | {status, video_id}'"
Sleep 300ms
Enter
Sleep 4000ms
