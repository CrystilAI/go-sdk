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
scripts/in-each-module.sh go build ./...
```

The repo has several Go modules: the SDK at the root, and `examples/`, `integration/` and `providertest/`, which use the provider SDKs. Each has its own `go.mod`, so the provider SDKs stay out of the SDK's: every line in that file takes part in choosing versions in a customer's build. A root `go build ./...` stops at the nested modules, so the script runs the command in each one. Inside a module, plain `./...` works.

The pre-commit hook runs CI's other checks in every module on each commit: formatting (`gofmt`, `goimports`), [golangci-lint](https://golangci-lint.run) with the settings in `.golangci.yml`, `go mod tidy` and `go vet`. Formatting and lint fixes are written to your files and fail the commit; stage them and commit again. To run the checks without committing:

```bash
pre-commit run --all-files
```

### Run an example

The examples are documentation: each shows a customer how to use part of the SDK, and CI builds them so a change can't break one unnoticed. They are not tests. To check a change against staging or a local collector, write or run an [integration test](#integration-tests).

The examples make real LLM calls, which cost money. They send telemetry to the collector at `CRYSTIL_COLLECTOR_URL_BASE`, or to production (`https://collector.crystil.com`) when it is unset. The `workflow` and `invocation` examples also call the API at `CRYSTIL_API_URL_BASE`, or `https://api.crystil.com`. Every example reads `CRYSTIL_API_KEY`, which must belong to the same environment. Most also need their provider's key:

| Example | Provider key |
|---|---|
| `openai`, `streaming`, `transactions`, `langchain`, `invocation` | `OPENAI_API_KEY` |
| `anthropic` | `ANTHROPIC_API_KEY` |
| `google`, `langchain-google` | `GOOGLE_API_KEY` |
| `google-vertex` | Google credentials (`GOOGLE_APPLICATION_CREDENTIALS`, or `gcloud auth application-default login`), and `GOOGLE_CLOUD_PROJECT` unless the credentials file names it |
| `workflow` | none |

```bash
export CRYSTIL_API_KEY=...
export OPENAI_API_KEY=...
go -C examples run ./openai
```

## Run the automated tests

```bash
CRYSTIL_TEST_MODE=1 go test ./...
```

`CRYSTIL_TEST_MODE` stops the SDK from sending payloads to the collector. The root package's tests set it themselves, but set it on the command line as CI does, so every package is covered.

```bash
CRYSTIL_TEST_MODE=1 go test -race ./...                       # with the race detector
CRYSTIL_TEST_MODE=1 go test -run TestIncludeStreamUsage ./... # one test by name
CRYSTIL_TEST_MODE=1 go test -v ./...                          # print each test
go -C providertest test ./...                                 # the tests that use the provider SDKs
CRYSTIL_TEST_MODE=1 scripts/in-each-module.sh go test -race ./...  # every module, as CI runs them
```

The tests in `providertest/` drive the real provider SDKs (openai-go, anthropic-sdk-go, genai, langchaingo) through the SDK and check what a fake collector receives; they clear `CRYSTIL_TEST_MODE` for it. None of these tests call an LLM provider or a Crystil service: they use local `httptest` servers or call the code directly. The integration tests below make the live calls.

### Integration tests

The tests in `integration/` call OpenAI, Anthropic, Gemini and Vertex AI through the SDK, and check that a real Crystil collector accepts each payload. Each run costs money, so they never run by default: they build only with the `integration` tag, and they skip unless `CRYSTIL_COLLECTOR_URL_BASE` names the collector. A provider whose key is unset is skipped. Copy `.env.example` to `.env` and fill in what you need; it lists every variable the tests read.

```bash
set -a; . ./.env; set +a                                                 # Go doesn't load .env itself
go -C integration test -tags integration -v -count=1 ./...              # all providers
go -C integration test -tags integration -v -count=1 -run TestGemini ./... # one provider
```

`-count=1` stops Go from reusing a cached result instead of calling the providers. With `CRYSTIL_DUMP_RESPONSES=1`, each payload is also written to `integration/output/`, with the Crystil API key masked.

To run them against staging on GitHub, start the **Run Integration Tests** workflow from the repo's Actions tab. It reads the `STAGING_*` secrets named in `.github/workflows/run-integration-tests.yaml`.

### CI

`.github/workflows/run-unit-tests.yml` runs on every pull request and every push to `main`:

- **Lint:** `gofmt`, `go vet`, golangci-lint and a check that each `go.mod` is tidy, in every module.
- **Test:** `go build ./...` and `go test -race ./...` in every module, on each Go version in the workflow's matrix: the `go.mod` version and the newer releases Go still supports.
- **Test (latest provider SDKs):** the `providertest/` tests again, with each provider SDK upgraded to its newest release, on the newest Go. The versions the modules pin are the minimums `README.md` states; this job checks the newest ones.

The integration tests run only when started by hand, as described above.

## Release to the public repository

Release with the `/run-release` skill in this repo ([`.claude/skills/run-release/SKILL.md`](.claude/skills/run-release/SKILL.md)). From the repo root, start Claude Code and run `/run-release`. It bumps `const version` in `client.go`, adds the `CHANGELOG.md` entry, opens the release PR, and publishes the GitHub Release. It stops to ask before merging and before publishing.

**A Go release cannot be undone.** The `vX.Y.Z` tag on this public repo is the release: once `proxy.golang.org` has fetched it, it serves that code forever. Never delete or move a tag. If a release is wrong, fix it on `main` and release the next version.

To release by hand, follow [SDK Deployments](../README/docs/deployments/sdks.md) in the README repo.
