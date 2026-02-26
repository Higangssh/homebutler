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

# 1. System status
Type "homebutler status"
Sleep 200ms
Enter
Sleep 2500ms

# 2. Docker list
Type "homebutler docker list"
Sleep 200ms
Enter
Sleep 2500ms

# 3. Alerts
Type "homebutler alerts"
Sleep 200ms
Enter
Sleep 2500ms

# 4. Clear and show TUI (hero, last)
Type "clear"
Enter
Sleep 300ms
Type "homebutler watch"
Sleep 200ms
Enter
Sleep 5s
