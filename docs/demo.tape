# Demo recording for README
# Run: vhs docs/demo.tape → produces docs/demo.gif

Output docs/demo.gif

Set Shell "bash"
Set FontSize 15
Set Width 1100
Set Height 600
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set Padding 20

# Hidden setup
Hide
Type "export PATH=/home/ubuntu/workspaces/heygen-cli/bin:$PATH"
Enter
Type "export HEYGEN_API_KEY=$(cat ~/.heygen-api-key-prod)"
Enter
Type "clear"
Enter
Show

Sleep 500ms

Type "heygen --help"
Sleep 300ms
Enter
Sleep 2500ms

Type "clear"
Enter
Sleep 200ms

Type "# Real API call — list recent videos"
Enter
Type "heygen video list --limit 3 --human"
Sleep 300ms
Enter
Sleep 3500ms
