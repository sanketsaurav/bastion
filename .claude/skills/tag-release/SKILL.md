---
name: tag-release
description: Cut a new bastion release - pick the version, write the changelog, tag, push, and watch the release workflow. Use when the user wants to release, ship, publish, or tag a new version.
---

# Cut a bastion release

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which runs
goreleaser: darwin/linux × amd64/arm64 builds with the version stamped into
`internal/version.Version` from the tag, tar.gz archives, `checksums.txt`
signed keyless via cosign, a GitHub release whose notes are this tag's
`CHANGELOG.md` section (fallback: a generated commit list), and the
homebrew cask pushed to `sanketsaurav/homebrew-tap`.

There are **no version files to bump** — the tag is the single source of
version truth (`internal/version.Version` stays `0.1.0-dev` for dev
builds). The judgment work is preflight, the version, and the changelog;
the changelog section must be committed to `master` **before** tagging,
because CI reads it at the tag to build the release notes.

## 1. Preflight

Stop and report (don't tag) if any of these fail:

```sh
git rev-parse --abbrev-ref HEAD     # must be master
git status --porcelain              # must be empty
git fetch origin && git rev-list --count master..origin/master   # must be 0
make check
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run github.com/goreleaser/goreleaser/v2@latest check
gh secret list | grep -q HOMEBREW_TAP_TOKEN   # tap publish needs it
```

## 2. Pick the version

```sh
last=$(git describe --tags --abbrev=0 2>/dev/null) || last=""
git log ${last:+$last..}HEAD --oneline
```

Pre-1.0 conventions: **patch** for fixes and internal-only changes, **minor**
for features, new schema surface, or anything behavior-breaking (new
required fields, changed defaults, changed remote layout). Propose a version
with one-line reasoning; if the changes argue for either, ask the user.

## 3. Write the changelog

Update `CHANGELOG.md` (create it with a `# Changelog` header if missing),
inserting a new section at the top. The header format must be exactly
`## vX.Y.Z - YYYY-MM-DD` — CI matches on the `## vX.Y.Z ` prefix:

```markdown
## vX.Y.Z - YYYY-MM-DD

### Features
- …

### Fixes
- …
```

Write user-facing prose from the actual changes (read the diffs when commit
subjects aren't enough) — not raw commit subjects. Omit empty sections; fold
notable internals into a short `### Internal` section only when worth
telling users. Call out anything operators must act on (schema changes, new
doctor requirements, changed remote state layout) in `### Upgrade notes`.
For the first release (no prior tag), write release highlights rather than
a history.

## 4. Commit, tag, confirm, push

```sh
git add -A
git commit -m "Release vX.Y.Z"
git tag -a "vX.Y.Z" -m "vX.Y.Z"
```

No AI/tool attribution anywhere — commit, tag, or changelog.

**Confirm with the user before pushing** — show the version and the
changelog section, and note that pushing publishes a GitHub release and
rewrites the tap cask. Then:

```sh
git push origin master "vX.Y.Z"
```

## 5. Watch the workflow and verify

```sh
gh run list --limit 2                        # the release run for the tag
gh run watch <release-run-id> --exit-status
gh release view "vX.Y.Z"                     # 4 archives + checksums.txt + .sig + .pem; notes = the changelog section
```

Verify the artifacts are actually consumable, then report the release URL:

```sh
dir=$(mktemp -d)
gh release download "vX.Y.Z" -p 'checksums.txt*' -p '*darwin_arm64*' -D "$dir"
(cd "$dir" && cosign verify-blob \
   --certificate checksums.txt.pem --signature checksums.txt.sig \
   --certificate-identity-regexp 'github.com/sanketsaurav/bastion' \
   --certificate-oidc-issuer https://token.actions.githubusercontent.com \
   checksums.txt \
 && shasum -a 256 -c checksums.txt --ignore-missing \
 && tar -xzf bastion_*_darwin_arm64.tar.gz && ./bastion version)   # prints X.Y.Z
gh api repos/sanketsaurav/homebrew-tap/contents/Casks/bastion.rb --jq .content \
  | base64 -d | grep -m1 "version \"X.Y.Z\""   # tap cask on the new version
```

If the release notes came out as a commit list, the changelog header didn't
match `## vX.Y.Z ` (the run logs say so). If they came out **empty**, the
goreleaser changelog got disabled — it must stay enabled (`use: git`) or
`--release-notes` is ignored. Either way, fix on master and re-sync just
the notes — don't re-tag:

```sh
awk -v v="## vX.Y.Z " 'index($0, v) == 1 {f=1; next} /^## v/ {f=0} f' CHANGELOG.md > /tmp/notes.md
gh release edit "vX.Y.Z" --notes-file /tmp/notes.md
```

## Failure modes

- **Workflow fails before the release exists**: fix on master, delete and
  re-push the tag (`git tag -d vX.Y.Z && git push --delete origin vX.Y.Z`,
  re-tag, re-push).
- **Partial release** (release created, a later step failed — e.g. the tap
  push): goreleaser is not resumable per-step. Delete the GitHub release
  but keep the tag (`gh release delete vX.Y.Z --yes`), then re-run the
  failed workflow run; it rebuilds the same tag and republishes everything,
  and the tap commit is an idempotent overwrite.
- **Tap push failed**: the usual cause is a missing or expired
  `HOMEBREW_TAP_TOKEN` (fine-grained PAT, contents:write on
  `sanketsaurav/homebrew-tap`). Fix the secret, then handle as a partial
  release above.
- **Once a version is announced or plausibly downloaded**, prefer
  fix-forward: cut a patch release instead of re-cutting the same version —
  a changed archive behind an existing version number breaks checksum
  verifiers.
