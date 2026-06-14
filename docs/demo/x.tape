# Demo tape for x. Rendered with ascii-gif (github.com/tamnd/ascii-gif),
# which supplies the window chrome and theme; this file is just the action.
#
#   ascii-gif render docs/demo/x.tape -o docs/static/demo.gif
#
# x must be on PATH inside the recording shell.

Hide
Type "export PS1='$ ' PATH=/tmp/demobin:$PATH"
Enter
Type "clear"
Enter
Show

Sleep 500ms
Type "x tweet 20 --fields author,created,likes,replies,text -o table"
Sleep 800ms
Enter
Sleep 3s

Type "x timeline nasa -n 4 --fields author,likes,rt,text -o table"
Sleep 800ms
Enter
Sleep 3.5s

Type "x user nasa -o jsonl | jq '{name, handle: .username, followers: .metrics.followers}'"
Sleep 800ms
Enter
Sleep 4s
