package gcp

import "github.com/sanketsaurav/bastion/internal/shellquote"

// Quote and QuoteJoin are re-exported from internal/shellquote, which owns the
// quoting rules shared by remote exec and generated scripts.
func Quote(s string) string          { return shellquote.Quote(s) }
func QuoteJoin(argv []string) string { return shellquote.Join(argv) }
