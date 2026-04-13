# Demo recording for README
# Run: vhs docs/demo.tape → produces docs/demo.gif
# Note: this fires a real video-agent generation job each run.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1100
Set Height 560
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms
Set Padding 20

# Hidden setup — load API key
Hide
Type "export HEYGEN_API_KEY=$(cat ~/.heygen-api-key-prod)"
Enter
Type "clear"
Enter
Show

Sleep 500ms

# One command: prompt → AI video job
Type "heygen video-agent create --prompt 'A quick welcome from the HeyGen CLI' --human"
Sleep 300ms
Enter
Sleep 4000ms

# It lands in the queue
Type "heygen video list --limit 3 --human"
Sleep 300ms
Enter
Sleep 3500ms
