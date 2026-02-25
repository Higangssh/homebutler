Output demo-tui.gif

Set Shell "bash"
Set FontSize 13
Set Width 1100
Set Height 700
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

# 1. TUI dashboard (hero)
Type "homebutler watch"
Sleep 200ms
Enter
Sleep 4s

# 2. System status
Type "homebutler status"
Sleep 200ms
Enter
Sleep 2s

# 3. Docker list
Type "homebutler docker list"
Sleep 200ms
Enter
Sleep 2s

# 4. Open ports
Type "homebutler ports"
Sleep 200ms
Enter
Sleep 2s

# 5. Alerts
Type "homebutler alerts"
Sleep 200ms
Enter
Sleep 3s
