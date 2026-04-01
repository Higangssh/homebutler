Output "./assets/install-demo.gif"

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 580
Set Theme "Catppuccin Mocha"
Set Padding 20
Set TypingSpeed 50ms

Hide
Type "clear"
Enter
Sleep 200ms
Show

# 1. List available apps
Type "homebutler install list"
Sleep 200ms
Enter
Sleep 3s

# 2. Install uptime-kuma
Type "homebutler install uptime-kuma"
Sleep 200ms
Enter
Sleep 12s

# 3. Check status
Type "homebutler install status uptime-kuma"
Sleep 200ms
Enter
Sleep 2500ms

# 4. Uninstall
Type "homebutler install uninstall uptime-kuma"
Sleep 200ms
Enter
Sleep 4s
