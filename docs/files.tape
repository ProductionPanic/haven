# Regenerate the file manager demo:  vhs docs/files.tape
# Same setup as docs/demo.tape (HAVEN_DB seeded by docs/demo-hosts.sh);
# needs ssh key access to test@productionpanic.com.
Output docs/files.gif

Set Shell "bash"
Set FontFamily "SpaceMono Nerd Font"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 80ms

Hide
Type `ssh -o BatchMode=yes test@productionpanic.com rm -f my-folder/release-notes.md my-folder/robots.txt; R=$PWD; rm -rf /tmp/acme-shop && mkdir /tmp/acme-shop && cd /tmp/acme-shop && sh "$R/docs/demo-files.sh" && clear` Enter
Wait
Show

Type "haven files test-site" Sleep 400ms Enter
Sleep 3s

# mark two local files
Down Sleep 400ms
Down Sleep 400ms
Space Sleep 400ms
Space Sleep 800ms

# open my-folder on the host
Tab Sleep 600ms
Down@300ms 4 Sleep 400ms
Enter Sleep 1.2s

# copy the marked files across
Tab Sleep 600ms
Type "c" Sleep 2.5s
Tab Sleep 1.5s

# back up, into httpdocs, view index.html
Backspace Sleep 1s
Up@300ms 2 Sleep 300ms
Enter Sleep 1s
Down Sleep 300ms
Enter Sleep 2s
Down@150ms 8 Sleep 1.5s
Type "q" Sleep 800ms

Type "q"
Sleep 500ms
