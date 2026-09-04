# Regenerate the demo with: vhs .vhs/demo.tape
#   brew install vhs
#
# Set COURSE to a folder of recordings before running:
#
#   COURSE=~/Desktop/"My Course" vhs .vhs/demo.tape
#
# Use a folder you are happy to show: the recording captures real filenames.

Output docs/demo.gif

Set Shell zsh
Set FontSize 15
Set Width 1180
Set Height 760
Set Padding 22
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms

Hide
Type "export COURSE=${COURSE:?set COURSE to a course folder}" Enter
Type "clear" Enter
Show

Type "coursekit scan $COURSE" Sleep 400ms Enter
Sleep 3s

Type "coursekit doctor $COURSE --profile udemy" Sleep 400ms Enter
Sleep 5s

Type "coursekit rename $COURSE" Sleep 400ms Enter
Sleep 4s

# The interactive browser.
Type "coursekit $COURSE" Sleep 400ms Enter
Sleep 2s
Down Sleep 500ms
Enter Sleep 1500ms
Down Down Sleep 600ms
Type "s" Sleep 1500ms
Type "?" Sleep 3s
Escape Sleep 800ms
Type "q"
Sleep 1s
