# Go SDK

How to work on the Crystil Go SDK: set it up, build it, test it and release it. To use the SDK in an application, see [README.md](README.md).

## Set up the development environment

1. Install Go, at least the version on the `go` line of `go.mod`:

   ```bash
   brew install go
   go version
   ```

2. Clone the repo next to the other Crystil repos. The parent directory can be anywhere (see [Repo Layout](../README/docs/local-environment/repo-layout.md)).

   ```bash
   git clone git@github.com:CrystilAI/go-sdk.git
   cd go-sdk
   go mod download
   ```

3. Install the pre-commit hook, which formats, lints and vets each commit:

   ```bash
   brew install pre-commit
   pre-commit install
   ```

The Python SDK in `../python-sdk` is the reference version. Before adding a feature, check how Python does it: payload shapes and environment variables should match.

## Build

```bash
go build ./...
```

This builds the SDK and every program under `examples/`. They are packages in the same module, so a broken example fails the build.

The pre-commit hook runs CI's other checks on each commit: formatting (`gofmt`, `goimports`), [golangci-lint](https://golangci-lint.run) with the settings in `.golangci.yml`, `go mod tidy` and `go vet`. Formatting and lint fixes are written to your files and fail the commit; stage them and commit again. To run the checks without committing:

```bash
pre-commit run --all-files
```

### Run an example

The examples make real LLM calls, which cost money, and send telemetry to the production collector (`https://collector.crystil.com`). To send it somewhere else, pass `crystil.WithCollectorURL(url)` to `crystil.New` in the example. Every example reads `CRYSTIL_API_KEY`. Most also need their provider's key:

| Example | Provider key |
|---|---|
| `openai`, `streaming`, `langchain`, `invocation` | `OPENAI_API_KEY` |
| `anthropic` | `ANTHROPIC_API_KEY` |
| `google` | `GOOGLE_API_KEY` |
| `workflow` | none |

```bash
export CRYSTIL_API_KEY=...
export OPENAI_API_KEY=...
go run ./examples/openai
```

## Run the automated tests

```bash
CRYSTIL_TEST_MODE=1 go test ./...
```

`CRYSTIL_TEST_MODE` stops the SDK from sending payloads to the collector. The root package's tests set it themselves, but set it on the command line as CI does, so every package is covered.

```bash
CRYSTIL_TEST_MODE=1 go test -race ./...                       # with the race detector
CRYSTIL_TEST_MODE=1 go test -run TestReconstructGoogle ./...  # one test by name
CRYSTIL_TEST_MODE=1 go test -v ./...                          # print each test
```

The tests call no LLM provider and no Crystil service: they use local `httptest` servers or call the code directly. This repo has no integration tests against real providers.

### CI

`.github/workflows/run-unit-tests.yml` runs on every pull request and every push to `main`:

- **Lint:** `gofmt`, `go vet`, golangci-lint and a check that `go.mod` is tidy.
- **Test:** `go build ./...` and `go test ./...` on each Go version in the workflow's matrix: the `go.mod` version and the newer releases Go still supports.

## Release to the public repository

Release with the `/run-release` skill in this repo ([`.claude/skills/run-release/SKILL.md`](.claude/skills/run-release/SKILL.md)). From the repo root, start Claude Code and run `/run-release`. It bumps `const version` in `client.go`, adds the `CHANGELOG.md` entry, opens the release PR, and publishes the GitHub Release. It stops to ask before merging and before publishing.

**A Go release cannot be undone.** The `vX.Y.Z` tag on this public repo is the release: once `proxy.golang.org` has fetched it, it serves that code forever. Never delete or move a tag. If a release is wrong, fix it on `main` and release the next version.

To release by hand, follow [SDK Deployments](../README/docs/deployments/sdks.md) in the README repo.
