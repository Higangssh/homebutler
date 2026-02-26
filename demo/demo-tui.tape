Output demo-tui.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 580
Set Theme "Catppuccin Mocha"
Set Padding 20
Set TypingSpeed 50ms

Hide
Type "alias homebutler='/tmp/hb-demo'"
Enter
Sleep 200ms
Type "clear"
Enter
Sleep 200ms
Show

# 1. TUI dashboard (hero, first impression)
Type "homebutler watch"
Sleep 200ms
Enter
Sleep 5s

# 2. Clear and show CLI commands
Type "clear"
Enter
Sleep 300ms

Type "homebutler status"
Sleep 200ms
Enter
Sleep 2500ms

Type "homebutler docker list"
Sleep 200ms
Enter
Sleep 2500ms

Type "homebutler alerts"
Sleep 200ms
Enter
Sleep 3s
