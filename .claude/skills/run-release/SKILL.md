---
name: run-release
description: Cut a release of the Crystil Go SDK (module github.com/CrystilAI/go-sdk) — bump the version constant in client.go, update CHANGELOG.md, open the release PR, then publish the GitHub Release whose tag IS the published module. Use when asked to release, cut a release, ship a version, bump the version, or publish the module.
---

# Cut a release of the Crystil Go SDK

**A Go module is its git tag.** The moment `vX.Y.Z` exists on this repo,
`go get github.com/CrystilAI/go-sdk@vX.Y.Z` works. There is no registry upload, no token, no OIDC.
[`publish-go-module.yaml`](../../../.github/workflows/publish-go-module.yaml) runs on a published
Release to do the two things still worth automating: gate on checks, and warm `proxy.golang.org`
and `index.golang.org` so the version is fetchable and listed on pkg.go.dev immediately.

**This is the least forgiving of the four SDKs.** Its checks run *after* the tag exists, so they
cannot prevent a bad release — they only tell you one happened. `proxy.golang.org` caches module
content **immutably** and `sum.golang.org` records its hash permanently, so a tag pointing at
broken code is public forever, and that number can never be reused: re-tagging it either serves
the original cached content or trips a checksum-mismatch error in users' builds. **The only remedy
is to burn the number and release the next one.** Get it right before publishing the Release.

## Repo facts

| | |
|---|---|
| Version file | `client.go` — `const version = "..."` |
| Also | the git tag itself; the workflow fails the release if the two disagree |
| Module path | `github.com/CrystilAI/go-sdk` (`go.mod`) |
| Changelog | `CHANGELOG.md`, Keep a Changelog format, **oldest entry first — append new entries at the bottom** |
| Publish workflow | `.github/workflows/publish-go-module.yaml`, environment `go-module` (no secrets) |
| Listing | https://pkg.go.dev/github.com/CrystilAI/go-sdk |

The workflow also enforces that the tagged commit is an **ancestor of `main`** — a release cut
from a side branch would publish unreviewed code under a number that can never be reclaimed.

> **Releasing v2.0.0 or later** additionally requires the major-version suffix: `go.mod` must
> declare `module github.com/CrystilAI/go-sdk/v2` and every import path in the repo, examples, and
> README must gain the `/v2`. Without that, the tag resolves for nobody. Flag this to the user and
> treat it as its own change, not part of a routine release.

## Step 0 — Preflight

Do not skip. Report each result to the user before continuing.

```bash
gh auth status
git status --porcelain                      # must be empty
git rev-parse --abbrev-ref HEAD             # must be main
git fetch origin && git status -sb | head -1  # must not be behind
grep -n 'const version' client.go
git tag --sort=-v:refname | head -5
```

Resolve the target version:

- If the user gave `X.Y.Z`, use it.
- If they gave `patch` / `minor` / `major`, compute it from the current `version` constant.
- If they gave nothing, ask which of the three they want, showing the resulting number.

**Abort if `vX.Y.Z` already exists**, locally or on the proxy:

```bash
git tag -l vX.Y.Z
GOPROXY=https://proxy.golang.org go list -m github.com/CrystilAI/go-sdk@vX.Y.Z
```

If the proxy knows the version, it is gone permanently — pick the next number.

## Step 1 — Bump the version constant

```go
// client.go
const version = "X.Y.Z"
```

That constant is what outgoing telemetry reports (`config.version` → payload `Version`). The tag
is the other half; the workflow cross-checks them.

## Step 2 — Update the CHANGELOG

Draft entries from the commits since the last release:

```bash
git log --oneline --no-merges $(git describe --tags --abbrev=0)..HEAD
```

**Append** a new section at the **bottom** of `CHANGELOG.md` (this file is oldest-first):

```markdown
## [X.Y.Z] - YYYY-MM-DD

### Added
- ...

### Fixed
- ...
```

Use today's real date. Group under Added / Changed / Fixed / Removed / Deprecated / Security —
only the headings you actually need. Write what a *consumer of the module* would care about, not
commit subjects; skip internal CI and refactor churn. Flag breaking changes with **BREAKING** —
and remember that in Go a breaking change to an exported symbol is a `/v2` module-path change, not
just a major version number.

If a `## [X.Y.Z]` section **already exists** — the version was bumped in an earlier PR but never
released — add to that section and reset its date to today rather than creating a second one.

## Step 3 — Branch, commit, open the PR

```bash
git checkout -b release/vX.Y.Z
git add client.go CHANGELOG.md
git commit -m "Release vX.Y.Z"
git push -u origin release/vX.Y.Z
gh pr create --title "Release vX.Y.Z" --body "Bumps the version constant to X.Y.Z and adds the CHANGELOG entry."
```

## Step 4 — Run the checks, then wait for CI

Run locally exactly what the release workflow will run. Here this matters more than in the other
SDKs: the workflow's copy of these checks runs *after* the tag is already public, so **this local
run is the only one that can still prevent a bad release.**

```bash
gofmt -l .                       # must print nothing
go vet ./...
go build ./...                   # includes ./examples/... — where renames surface first
CRYSTIL_TEST_MODE=1 go test ./...
```

Then wait for the PR's own checks, if any are configured:

```bash
gh pr checks --watch
```

If anything fails, fix it on the release branch and push — you are still fully reversible here.

## GATE 1 — stop before merging

Show the user the diff (`git diff origin/main...HEAD`) and the check results, and **ask for
explicit confirmation to merge**. Do not merge on your own initiative.

## Step 5 — Merge

```bash
gh pr merge --merge --delete-branch
git checkout main && git pull
```

`--merge` is not a preference: the `Main Rules` ruleset on this repo sets
`allowed_merge_methods: ["merge"]`, so `--squash` and `--rebase` are rejected outright.

That ruleset also requires **1 approving review** and configures **no bypass actors**, and GitHub
does not let you approve your own PR. So a release PR you opened cannot be merged by you alone —
someone else has to review it. If nobody is available, `gh pr merge --auto --merge` queues the
merge for the moment approval lands. Do not reach for `--admin`; bypassing a review requirement is
the user's decision to make, not yours.

## GATE 2 — stop before publishing

This is the last reversible moment, and it is more final here than anywhere else: publishing the
Release creates the tag, and the tag *is* the release. Restate the version, confirm the local
checks in step 4 all passed, and confirm that **the number cannot be reused under any
circumstances.** Wait for an explicit yes.

## Step 6 — Publish the GitHub Release

```bash
gh release create vX.Y.Z --target main --title "vX.Y.Z" \
  --notes "$(awk '/^## \[X\.Y\.Z\]/{f=1;next} f&&/^## \[/{exit} f' CHANGELOG.md)"
```

The leading `v` is required, and the rest must equal the `version` constant exactly or the
workflow fails. `--target main` is what satisfies the workflow's ancestor-of-main check.

Use the `awk` extraction above, **not** `sed -n '/^## \[X.Y.Z\]/,/^## \[/p' | sed '$d'`. Because
this changelog is oldest-first, the version you are releasing is the last section in the file, so
the `sed` form runs to EOF and `sed '$d'` then eats its final line. `awk` stops at the next header
or EOF and drops nothing.

Do not use `--notes-from-tag`: `gh` creates a lightweight tag, so that flag falls back to the
commit message and the notes come out reading "Merge pull request #NN".

(The GitHub UI works too: *Releases → Draft a new release*, tag `vX.Y.Z` against `main`, then
**Publish release**. Saving a draft creates no tag and triggers nothing; only publishing does.)

## Step 7 — Watch and verify

```bash
gh run watch "$(gh run list --workflow=publish-go-module.yaml --limit 1 --json databaseId -q '.[0].databaseId')"
```

The final workflow step resolves the module from `proxy.golang.org` out of an empty throwaway
module — the real path a user takes, including checksum verification against `sum.golang.org`. A
green run means the version is genuinely fetchable. Confirm independently:

```bash
cd "$(mktemp -d)" && go mod init publishcheck >/dev/null \
  && GOPROXY=https://proxy.golang.org go get github.com/CrystilAI/go-sdk@vX.Y.Z
```

Then check https://pkg.go.dev/github.com/CrystilAI/go-sdk (listing can lag by a few minutes).

## If the publish run fails

Unlike the other SDKs, **there is nothing to roll back and no number to recover.** The tag exists,
so the version is published by definition, and the workflow has already asked the proxy for it.
Deleting the tag does not unpublish anything the proxy has cached, and re-pointing it later breaks
users' checksums.

Fix forward: correct the problem on `main` and release the **next** version. Leave the bad tag in
place — do not re-tag it.