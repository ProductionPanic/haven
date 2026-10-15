# Regenerate the README demo:  vhs docs/demo.tape
# Expects `haven` on $PATH and HAVEN_DB pointing at a demo database
# with fictional hosts (see docs/demo-hosts.sh).
Output docs/demo.gif

Set Shell "bash"
Set FontFamily "SpaceMono Nerd Font"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 80ms

Hide
Type "clear" Enter
Show

Type "haven" Sleep 400ms Enter
Sleep 1.5s

# browse the list, detail panel follows
Down@300ms 4
Sleep 1s
Up@300ms 4
Sleep 800ms

# live filter
Type "/" Sleep 300ms
Type "k3s" Sleep 1.2s
Escape Sleep 300ms
Escape Sleep 800ms

# group by server
Type "g" Sleep 1.5s
Type "g" Sleep 800ms

# cycle tags
Type "t" Sleep 1s
Type "t" Sleep 1.2s

# key bindings
Type "?" Sleep 2s
Type "?" Sleep 800ms

Type "q"
Sleep 500ms
