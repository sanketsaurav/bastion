# Contributing

Thanks for your interest in bastion.

## Development

```console
$ make check        # gofmt + go vet + go test — must be clean before a PR
$ go run honnef.co/go/tools/cmd/staticcheck@latest ./...
```

No GCP access is needed to develop: provider behavior runs against a fake
process runner, generated bash programs are golden-tested and executed under
real bash where semantics matter, and `go test ./...` covers everything CI
runs. Regenerate goldens with `go test ./internal/engine -update` and review
the diff like code.

Real-GCP verification is manual for now: point a definition at a disposable
Ubuntu 24.04 VM in a project you own and drive `doctor` → `plan` → `apply`.
Never test against infrastructure you cannot afford to lose.

## Design ground rules

[SPEC.md](SPEC.md) is decision-complete and wins over code and over the
long-form draft in `docs/original-spec.md`. Changes that alter behavior
should update the spec in the same PR. The invariants in SPEC §11 —
private-by-default, strict argv quoting, no broad deletions, secrets never
in digests or logs — are not up for relaxation for convenience.

## Commit style

Imperative subject with an area prefix (`engine:`, `doctor:`, `config:`),
body explaining the why. If a change came out of a live failure, say what
was observed.
