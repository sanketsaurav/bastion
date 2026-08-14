// Package shellquote renders strings as safe POSIX shell words. It backs both
// remote command construction and generated-script literals: user-influenced
// values are never interpolated into shell without passing through here.
package shellquote

import (
	"regexp"
	"strings"
)

var safeRe = regexp.MustCompile(`^[A-Za-z0-9_%+,\-./:=@]+$`)

// Quote returns s as a single POSIX shell word: shell-safe strings pass
// through; everything else is single-quoted with embedded quotes escaped.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	if safeRe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join renders argv as one shell command line in which every argument is a
// single word — the shell cannot reinterpret metacharacters.
func Join(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = Quote(a)
	}
	return strings.Join(quoted, " ")
}
