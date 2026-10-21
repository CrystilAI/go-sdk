# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Maintaining this file:** Keep CLAUDE.md current. Whenever you make a change that touches anything described here — architecture (new provider, new transport behavior, new streaming reconstructor, new API endpoint), public surface (exports from the `crystil` package, `Client` methods, `Option` constructors), config defaults, env vars, build/test commands, supported Go versions, dependency upgrades that affect public behavior, or test conventions — update the relevant section in the same change. If you introduce a Python-parity divergence, document it under "Python SDK is the source of truth." Don't let the doc drift; future runs of Claude rely on it for context.
>
> **Maintaining `.claude/agents/`:** The subagent files (`crystil-provider-integrator.md`, `python-parity-auditor.md`, `streaming-reconstructor.md`, `sdk-test-author.md`) describe rules and patterns the codebase follows. When you make a change that invalidates one of those rules — for example, adding/removing a `Provider` constant, changing the `tracingTransport` flow, introducing a new SSE event shape, adding a Python-parity divergence, or changing how the LangChain callback handler attributes calls — update the relevant agent file in the same change. The provider tables, the "rules of engagement" lists, and the "known intentional divergences" list especially go stale fast. If you spawn an agent and notice its prompt is out of date, fix the prompt before continuing.

## Commands

```bash
go test ./...                                     # all tests (init() sets CRYSTIL_TEST_MODE=1 to block network)
go test -run TestReconstructOpenAI ./...          # one test by name
go test ./api/...                                 # one package
go test -race ./...                               # race detector (collector goroutine + attribution mutation)
go vet ./...
pre-commit run --all-files                        # gofmt/goimports, golangci-lint (.golangci.yml), go mod tidy -diff, go vet
go build ./...
go run ./examples/openai                          # examples are main packages in this module; real API keys
```

`CRYSTIL_TEST_MODE` is read at the top of `collector.send` (`collector.go:66`) — when set to any non-empty value, the function returns `nil` immediately without marshalling or POSTing. `langchain_test.go:init()` sets it for the whole test binary, so streaming/transport/langchain tests don't have to set it per-case. If you add a test file that exercises a wrapped HTTP path and `langchain_test.go` is not in the same `go test` invocation, set the env yourself.

## Runtime + version

- SDK version: `const version` in `client.go`, copied into `config.version` and emitted in `meta.sdk.version` on every payload. When bumping, change `client.go` — `go.mod` has no version.
- `go.mod` declares `go 1.24`. The SDK uses generics and modern stdlib only; don't drop the floor without an audit of `iter.Seq2` / range-over-func usage in examples.

## Architecture

### The big departure from Python/JS: HTTP transport interception

This is the single most important thing to understand. **Python and JS monkey-patch caller-supplied LLM client objects in place. The Go SDK does not.** Instead:

1. The caller creates a Crystil `Client` (`crystil.New(...)`).
2. They ask it for an `*http.Client` via `client.HTTPClient(provider)` — that returned client has a `tracingTransport` (`transport.go`) as its `Transport`.
3. The caller passes the wrapped `*http.Client` to their provider SDK constructor (`openai.NewClient(option.WithHTTPClient(httpClient))`, `anthropic.NewClient(option.WithHTTPClient(httpClient))`, `genai.NewClient(ctx, &genai.ClientConfig{HTTPClient: httpClient, ...})`).
4. Every HTTP request the provider SDK makes flows through `tracingTransport.RoundTrip`, which (a) reads and replays the request body, (b) detects streaming via `"stream": true` in the JSON body, (c) for non-streaming, reads and replays the response body and fires telemetry, (d) for streaming, replaces `resp.Body` with `streamingResponseBody` so chunks are buffered as the caller reads them and telemetry fires on `Close()`.

The consequence: **the SDK never needs to know the provider's Go method signatures.** Adding a new HTTP-based provider is just (1) adding a `Provider` constant in `provider.go`, (2) routing its SSE reconstruction in `streaming.go:reconstructResponse`, and (3) writing an example. The provider's Go SDK isn't a build dependency unless we use it in tests or examples.

The cost: anything that doesn't go over HTTP from the provider SDK to the network — sub-stream callbacks, in-process LangChain runnables, mocked clients in user code — won't be captured by the transport. That's why LangChain has a separate callback-handler path (see below).

### `tracingTransport.RoundTrip` flow (`transport.go`)

```
RoundTrip(req)
  start = time.Now()
  reqBody = captureBody(req.Body); req.Body = NopCloser(reqBody)
  isStreaming = isStreamingRequest(reqBody)        // JSON-parses, checks "stream":true
  resp, err = t.base.RoundTrip(req)                // execute downstream
  if err: return nil, err                          // network failure → no payload
  if isStreaming:
    resp.Body = streamingResponseBody{reader: resp.Body, ...}
  else:
    respBody = captureBody(resp.Body); resp.Body = NopCloser(respBody)
    collector.SendAsync(ctx, buildPayload(reqBody, respBody, start, time.Now()))
  return resp, nil
```

`buildPayload` reads `client.attribution` **dynamically** every call (not at `HTTPClient(...)` time). That's intentional — see `TestHTTPClientMutableAttribution` in `client_test.go`. A caller can `SetAttribution(...)` after the `*http.Client` is in flight and subsequent requests will reflect it.

There is no retry on the LLM request itself. The 5xx retry transport in `collector.go:retryTransport` is wired only into the collector's own HTTP client, not the user-facing `tracingTransport`. **Don't add retry to `tracingTransport`** — the provider SDK owns retry semantics and double-retry produces duplicate LLM charges.

### Streaming reconstruction (`streaming.go`)

`streamingResponseBody` wraps `resp.Body`. `Read(p)` mirrors bytes to an internal buffer; `Close()` parses the buffer as SSE (`data: {json}\n\n`), routes to the per-provider reconstructor based on `s.provider`, and fires the payload.

The per-provider reconstructors (`reconstructOpenAI`, `reconstructAnthropic`, `reconstructGoogle`) merge chunk-deltas into a single response shape that **matches what the non-streaming endpoint returns for the same provider**. That shape match is what the backend extractor expects — diverging here means the backend won't recognize the call. When you touch a reconstructor, the contract is: streamed payload's `conversation.response` is structurally indistinguishable from a non-streamed `conversation.response` for the same provider.

| Provider | Event shape | Final shape after reconstruction |
|---|---|---|
| OpenAI | `data: {choices: [{delta: {role, content}}]}` per chunk, then `data: [DONE]` | `{choices: [{message: {role, content}, finish_reason, index}]}` |
| Anthropic | `message_start` then `content_block_delta` events with `delta: {text}` | message_start's `message` object + `content: [{type: "text", text: <accumulated>}]` |
| Google GenAI | `{candidates: [{content: {parts: [{text}]}}]}` per chunk | `{candidates: [{content: {parts: [{text: <accumulated>}], role: "model"}, finishReason: "STOP"}]}` |

`[DONE]` markers and non-`data:` lines (event headers, comments, blank lines) are silently dropped. Anthropic events are typed; route by `chunk["type"]` rather than positional assumptions.

Streaming detection is currently body-content-based: `isStreamingRequest` JSON-parses `req.Body` and looks for `"stream": true`. **Google's `GenerateContentStream` uses a different URL suffix (`:streamGenerateContent`), not a JSON flag.** If a Google streaming call doesn't carry `stream: true` in its body, it falls through to the non-streaming path — verify against the current `google.golang.org/genai` request shape before changing this. If we need URL-based detection, add it alongside the body check, don't replace it.

### LangChain is a separate path (`langchain.go`)

`langchaingo` (`github.com/tmc/langchaingo`) doesn't accept an `*http.Client` — it constructs HTTP clients internally per provider. So we can't intercept its calls via `HTTPClient(provider)`. Instead, `Client.NewLangChainHandler()` returns a `callbacks.Handler` implementation that fires telemetry from `HandleLLMEnd` / `HandleLLMGenerateContentEnd` / `HandleLLMError`. The caller passes this handler to `langchaingo`'s `WithCallback(...)` option on whatever LLM they construct.

Consequences:
- `conversation.client.title` is hardcoded `"langchain"` (not the underlying provider), since the callback doesn't know which provider answered.
- `query` for `HandleLLMEnd` is empty `{}` (the `result map[string]any` is response-only) and `query` for `HandleLLMGenerateContentEnd` is a synthetic single-message `{messages: [{role: "user", content: ""}]}` (langchaingo's callback doesn't pass the prompts through to End). These are real shape gaps versus what direct-transport calls report; document them rather than fudging.
- `startTime` is keyed on `client.txID` in a per-handler map. Concurrent LLM calls on the same `Client` will collide on that key — Start times overwrite each other. **Don't use one handler across concurrent LLM calls.** Construct a fresh handler per transaction (`client.NewTransaction().NewLangChainHandler()`).

### `payload.conversation.client` shape (`internal/payload.go`)

```go
type ClientInfo struct {
    Provider string `json:"provider,omitempty"`  // currently always ""
    Title    string `json:"title"`               // string(provider) for direct, "langchain" for callbacks
    Version  string `json:"version,omitempty"`   // currently always ""
}
```

Two intentional emptiness traps to be aware of:
- `Provider` is always `""` today. There's no framework distinction yet (Python/JS use `"langchain"` / `"ai_sdk"` here). If you add a framework wrapper that should be reported under `provider`, change the wiring in `transport.go:buildPayload` and `langchain.go` — don't pun on `Title`.
- `Version` is always `""`. Go's HTTP-level interception means we don't have a reference to the provider SDK object the way Python/JS do, so we can't read its version. If we need to report it, the caller has to surface it through a new option on `HTTPClient(...)` or via a separate config field; don't try to sniff it from `User-Agent` headers on the request (provider SDKs change those frequently and `User-Agent` can carry multiple tokens).

The Crystil SDK's own language/version lives in `meta.sdk` (`{client: "go", version: <const version>}`).

### Async collector (`collector.go`)

`collector` runs a single worker goroutine that consumes from a buffered channel (`queue chan internal.Payload`, capacity 100). `SendAsync` does a non-blocking send: if the channel is full, the payload is **dropped silently** to keep the user-facing request path non-blocking. That's the design — telemetry must never block LLM calls.

`Close()` closes the queue channel and waits on `done` for the worker to drain. Callers should `defer crystilClient.Close()` to ensure in-flight payloads are sent before process exit. **Don't add a graceful-shutdown timeout to `Close()` without thought** — a send can take about 15s, so a backlog of 100 dropped or hung payloads is bounded but not bounded *tight*.

The collector's HTTP client uses `retryTransport`, which tries again after a 5xx, sleeping 1s, 2s, 4s and 8s between tries, up to 5 tries. The client's timeout (`WithTimeout`, 5s by default) and the worker's 10s context end it sooner, but the sleep does not watch them: against a collector answering 503, that is 3 tries over about 7s by default, and 4 over about 15s with a timeout of 10s or more. Network errors (`err != nil` from RoundTrip) are not retried — they return immediately. That mismatch is intentional: a network error usually means we can't reach the collector at all, so retrying just blocks the worker; a 5xx is recoverable.

### Control-plane API client (`api/`)

`api.Client` is a separate HTTP client (not the user-facing wrapped one) for `/v1/-/...` endpoints — workflow management and invocation summaries. It uses `Authorization: Bearer <apiKey>`, where the collector uses no auth header (the API key rides in `meta.api.key` in the payload body). That asymmetry mirrors Python and JS.

`Client.Workflows()` / `Client.Workflow(uuid)` construct fresh `api.Client` instances per call. There's no caching — each control-plane call creates a new `http.Client` with the configured timeout. That's fine because these are user-driven, not in-loop hot paths.

## Python SDK is the source of truth

`../python-sdk` is the reference implementation. When adding a feature, check `../python-sdk/crystil/` first — payload shapes match, names map Python `snake_case` ↔ Go `PascalCase` for exports / `camelCase` for fields, env vars match (`CRYSTIL_API_KEY`, `CRYSTIL_TEST_MODE`).

Known intentional divergences:

- **Architecture: transport interception, not monkey-patching.** Python/JS mutate the caller's LLM client; Go gives the caller a wrapped `*http.Client`. This is a deliberate Go-idiomatic choice (it's how `http.RoundTripper` is meant to be used) and there is no plan to back-port monkey-patching. Don't introduce `register(client)` style wrappers in Go.
- **No sentinel/relevance-intercept support yet.** Python has `raise_if_irrelevant` and JS has `raiseIfIrrelevant`; Go has neither. If/when we add it, the natural place is a pre-RoundTrip hook in `tracingTransport` that issues the intercept POST (via `api.Client`-style transport) before letting the wrapped request through. Don't add it inside `collector` — collector is fire-and-forget by contract.
- **Attribution caps at 100 chars** (matching Python). JS caps at 255. The validator is in `attribution.go:validate`.
- **No Vercel AI SDK / Groq / AISDK integrations.** Those are JS-only. Python has `pydantic_ai`; Go doesn't.
- **No `conversation.client.version` reporting.** See "shape" section above — the transport doesn't have a handle on the provider SDK object.
- **`conversation.client.provider` is always `""`**, including for the LangChain callback path (which sets `title: "langchain"`). Python/JS distinguish framework-vs-provider here; Go currently doesn't.
- **Two separate code paths for telemetry capture (transport + callback handler) instead of one.** Python/JS have one `Invoke*`-driven path because they monkey-patch and can intercept anywhere; Go needs the callback handler because `langchaingo` doesn't expose its HTTP client. Keep the two paths' payload shapes aligned via `internal.Payload` — they currently emit the same struct.
- **Test mode skips the POST entirely (no console log).** Python/JS log the payload to stdout in test mode so tests can spy on it; Go just returns `nil` from `collector.send`. Tests assert behavior through other observable state (buffers, start-time maps, etc.) rather than payload contents. If we need payload-shape assertions, the right move is to factor `buildPayload` so tests can call it directly — don't bolt on a stdout-log mode just to mimic Python/JS.

## Test conventions

- **`CRYSTIL_TEST_MODE=1` is set in `langchain_test.go:init()`** and applies to the whole package's test binary. As long as a new test lives in `package crystil` (the root), it inherits the env. If you add a test in a sub-package (`api/`, `internal/`), set it in that package's own `init()` or `TestMain`.
- **Don't make real HTTP calls in unit tests.** With `CRYSTIL_TEST_MODE` set, the collector won't POST. For tests that need to exercise `tracingTransport.RoundTrip`, build an `http.Client` whose base transport is a `httptest.Server` (preferred) or a stub `RoundTripper` that returns canned responses. The existing tests largely test components in isolation — `reconstructOpenAI` is tested by constructing `streamingResponseBody` directly and calling the method, not by driving HTTP. That pattern is fine when the unit under test doesn't depend on the network path; use `httptest` when it does.
- **Streaming tests construct `streamingResponseBody` directly with `io.NopCloser(strings.NewReader(...))`** as the reader. See `TestStreamingResponseBodyRead`. The `Close()` method fires `SendAsync` which is then no-op'd by `CRYSTIL_TEST_MODE`; the assertion is on observable state (buffer contents) not on what got sent.
- **Race detector.** The collector mutates `c.attribution` via `SetAttribution` while `tracingTransport.buildPayload` reads it. Currently neither side locks — there's an implicit happens-before via the user-code ordering, but a multi-goroutine test would race. If you add concurrency, also add the necessary `sync.RWMutex` and prove it under `go test -race`.
- **Examples are not tests.** `examples/*/main.go` are `package main` programs in this module with no test files, so `go test ./...` doesn't run them. `go build ./...` does build them, and CI runs it to catch an example broken by a library change.

## Specialized subagents

`.claude/agents/` contains scoped subagents. Use them when the task matches:

- **`crystil-provider-integrator`** (model: `opus`) — add a new HTTP-based LLM provider (new `Provider` constant, streaming-reconstruction routing, example). Knows the transport-interception model and why "register the client" isn't the Go API.
- **`python-parity-auditor`** (model: `sonnet`) — diff a Go feature/payload against `../python-sdk`. Returns gap list.
- **`streaming-reconstructor`** (model: `opus`) — fix or add SSE chunk reconstruction for a provider. Knows the OpenAI/Anthropic/Google event shapes and the streaming-payload-must-match-non-streaming-payload contract.
- **`sdk-test-author`** (model: `sonnet`) — write Go tests following the package's `init()`-driven test-mode pattern and `httptest`-or-direct-construction conventions.
