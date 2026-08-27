package engine

// Shell integration (SPEC.md §8.5): a managed ~/.config/bastion/shell.sh
// plus exactly one delimited source line appended to ~/.bashrc — bastion
// never edits any other region of shell startup files.

// ShellTarget is where the managed integration file lives on the box.
const ShellTarget = "~/.config/bastion/shell.sh"

// HushloginTarget is the empty managed file that silences the
// distribution's MOTD and sshd's last-login line (host.shell.motd: quiet).
// Its existence is the mechanism; the content stays empty.
const HushloginTarget = "~/.hushlogin"

// shellLineMarker is the delimiter that makes the .bashrc line findable and
// the append idempotent.
const shellLineMarker = "# bastion:shell-integration"

// shellLine is the single line added to ~/.bashrc. $HOME stays literal —
// the guard makes removal of shell.sh safe without editing .bashrc again.
const shellLine = `[ -f "$HOME/.config/bastion/shell.sh" ] && . "$HOME/.config/bastion/shell.sh"  ` + shellLineMarker

// shellContent renders shell.sh. The prompt mirrors Ubuntu's stock colored
// PS1 with the alias in place of the login username; the alias charset is
// validated to be safe inside the single-quoted assignment.
func shellContent(prompt string) []byte {
	return []byte(`# Managed by bastion — edit the box definition, not this file.
PS1='\[\e[01;32m\]` + prompt + `@\h\[\e[00m\]:\[\e[01;34m\]\w\[\e[00m\]\$ '
`)
}
