package gcp

import "testing"

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"simple":        "simple",
		"with space":    "'with space'",
		"don't":         `'don'\''t'`,
		"a;rm -rf /":    `'a;rm -rf /'`,
		"$(whoami)":     `'$(whoami)'`,
		"`id`":          "'`id`'",
		"a&&b":          "'a&&b'",
		"":              "''",
		"path/to/file":  "path/to/file",
		"--flag=value":  "--flag=value",
		"tmux ls; exit": "'tmux ls; exit'",
		"*":             "'*'",
		">out":          "'>out'",
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestQuoteJoin(t *testing.T) {
	got := QuoteJoin([]string{"tmux", "new-session", "-s", "my session"})
	want := `tmux new-session -s 'my session'`
	if got != want {
		t.Errorf("QuoteJoin = %q, want %q", got, want)
	}
}
