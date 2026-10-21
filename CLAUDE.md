# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Maintaining this file:** Keep CLAUDE.md current. Whenever you make a change that touches anything described here — architecture (new provider, new transport behavior, new streaming reconstructor, new API endpoint), public surface (exports from the `crystil` package, `Client` methods, `Option` constructors), config defaults, env vars, build/test commands, supported Go versions, dependency upgrades that affect public behavior, or test conventions — update the relevant section in the same change. If you introduce a Python-parity divergence, document it under "Python SDK is the source of truth." Don't let the doc drift; future runs of Claude rely on it for context.
>
> **Maintaining `.claude/agents/`:** The subagent files (`crystil-provider-integrator.md`, `python-parity-auditor.md`, `streaming-reconstructor.md`, `sdk-test-author.md`) describe rules and patterns the codebase follows. When you make a change that invalidates one of those rules — for example, adding/removing a `Provider` constant, changing the `tracingTransport` flow, introducing a new SSE event shape, adding a Python-parity divergence, or changing how the LangChain callback handler attributes calls — update the relevant agent file in the same change. The provider tables, the "rules of engagement" lists, and the "known intentional divergences" list especially go stale fast. If you spawn an agent and notice its prompt is out of date, fix the prompt before continuing.

## Commands

```bash
go test ./...                                     # the SDK's tests (init() sets CRYSTIL_TEST_MODE=1 to block network)
go test -run TestIncludeStreamUsage ./...         # one test by name
go test ./api/...                                 # one package
go test -race ./...                               # race detector (collector goroutine + attribution mutation)
go -C providertest test ./...                     # tests that drive the real provider SDKs against fake servers
scripts/in-each-module.sh go test ./...           # the same command in every module
pre-commit run --all-files                        # gofmt/goimports, golangci-lint (.golangci.yml), go mod tidy -diff, go vet; every module
go -C examples run ./openai                       # examples make real calls with real API keys
```

The repo has three Go modules, and `./...` stops at a nested one, so `scripts/in-each-module.sh` runs a command in each: the SDK at the root, whose `go.mod` must list only what the SDK itself imports (today `github.com/google/uuid`, and `github.com/tmc/langchaingo` for `NewLangChainHandler`), and `examples/` and `providertest/`, each with its own `go.mod` and `replace github.com/CrystilAI/go-sdk => ../`. Every line in the SDK's `go.mod` takes part in choosing versions in a customer's build, so the provider SDKs live only in those two. Keep their versions in step with each other and with the "Supported versions" table in `README.md`, which states them as minimums. CI's `test-latest-sdks` job (`run-unit-tests.yml`) runs `providertest/` with each of those modules upgraded to its newest release, so a module added to the table goes in that job's list too.

`CRYSTIL_TEST_MODE` is read at the top of `collector.send` (`collector.go:66`) — when set to any non-empty value, the function returns `nil` immediately without marshalling or POSTing. `langchain_test.go:init()` sets it for the whole test binary, so streaming/transport/langchain tests don't have to set it per-case. If you add a test file that exercises a wrapped HTTP path and `langchain_test.go` is not in the same `go test` invocation, set the env yourself. `providertest/` clears it in `newCollector`, since its tests read what the SDK sends.

## Runtime + version

- SDK version: `const version` in `client.go`, copied into `config.version` and emitted in `meta.sdk.version` on every payload. When bumping, change `client.go` — `go.mod` has no version.
- `go.mod` declares `go 1.24`. The SDK uses generics and modern stdlib only; don't drop the floor without an audit of `iter.Seq2` / range-over-func usage in examples.

## Architecture

### The big departure from Python/JS: HTTP transport interception

This is the single most important thing to understand. **Python and JS monkey-patch caller-supplied LLM client objects in place. The Go SDK does not.** Instead:

1. The caller creates a Crystil `Client` (`crystil.New(...)`).
2. They ask it for an `*http.Client` via `client.HTTPClient(provider)` — that returned client has a `tracingTransport` (`transport.go`) as its `Transport`.
3. The caller passes the wrapped `*http.Client` to their provider SDK constructor (`openai.NewClient(option.WithHTTPClient(httpClient))`, `anthropic.NewClient(option.WithHTTPClient(httpClient))`, `genai.NewClient(ctx, &genai.ClientConfig{HTTPClient: httpClient, ...})`).
4. Every HTTP request the provider SDK makes flows through `tracingTransport.RoundTrip`, which (a) reads and replays the request body, (b) detects streaming via `"stream": true` in the JSON body, (c) for non-streaming, reads and replays the response body and fires telemetry, (d) for streaming, replaces `resp.Body` with `streamingResponseBody` so chunks are buffered as the caller reads them and telemetry fires once, when the stream reaches its end or fails, or the caller closes it.

The consequence: **the SDK never needs to know the provider's Go method signatures.** Adding a new HTTP-based provider is just (1) adding a `Provider` constant in `provider.go`, (2) choosing the shape its streams are recorded in, in `streaming.go:reconstructResponse`, and (3) writing an example. The provider's Go SDK is never a dependency of the SDK module; only `examples/` and `providertest/` use it.

The cost: anything that doesn't go over HTTP from the provider SDK to the network — sub-stream callbacks, in-process LangChain runnables, mocked clients in user code — won't be captured by the transport. That's why LangChain has a separate callback-handler path (see below).

### `tracingTransport.RoundTrip` flow (`transport.go`)

```
RoundTrip(req)
  start = time.Now()
  reqBody = captureBody(req.Body)
  req = req.Clone(req.Context())                   // never change the caller's request
  reqBody = includeStreamUsage(...)                 // OpenAI chat streams only: ask for usage
  req.Body = NopCloser(reqBody)
  isStreaming = isStreamingRequest(reqBody)        // JSON-parses, checks "stream":true
             || path ends ":streamGenerateContent" // Gemini
  resp, err = t.base.RoundTrip(req)                // execute downstream
  if err: return nil, err                          // network failure → no payload
  if resp.StatusCode >= 400: return resp, nil      // failed call → no payload
  if isStreaming || Content-Type is text/event-stream:
    resp.Body = streamingResponseBody{reader: resp.Body, ...}
    if includeStreamUsage changed the request: resp.Body = usageChunkFilter{resp.Body}
  else:
    respBody, err = captureBody(resp.Body)
    if err: resp.Body = respBody, then err; return resp, nil  // no payload
    resp.Body = NopCloser(respBody)
    collector.SendAsync(ctx, buildPayload(reqBody, respBody, start, time.Now()))
  return resp, nil
```

A failed call sends no payload, as the Python SDK sends none: a response with a status of 400 or more, an unstreamed body whose read fails partway, and a stream with no chunks. Provider SDKs retry some failures, and each attempt passes through the transport, so recording them would count each attempt as a call with no tokens (`TestRetriedCallRecordedOnce`, `TestFailedCallNotRecorded`, `TestEmptyStreamNotRecorded`). A body whose read fails reaches the caller as it would without Crystil, the bytes that arrived and then the error: failing the round trip instead makes openai-go and anthropic-sdk-go send the call, already billed, again (`TestResponseCutShortNotRetried`).

A stream that fails after its first chunk is sent marked `failed`, with the reason in `exc`, as Python sends it. It fails on a read error while the request's context is live, or on the error event openai-go and anthropic-sdk-go stop reading at (`errorEvent`: OpenAI's top-level `error` that is not null, Anthropic's `type: "error"`). Neither SDK reads to the end after that, and their examples never close the stream, so it is sent at once (`TestFailedStreamRecorded`, `TestStreamErrorRecordedFailed`). The error event is left out of the response, as Python's SDKs raise it rather than return it, so a stream with only an error event sends nothing (`TestErrorOnlyStreamNotRecorded`). A read that fails after the context ended was the caller stopping the stream, not a failure (`TestCanceledStreamNotFailed`). Python fails neither the Responses API's `response.failed` and `type: "error"` events nor Gemini's error chunks, and neither does Go.

`buildPayload` reads `client.attribution` **dynamically** every call (not at `HTTPClient(...)` time). That's intentional — see `TestHTTPClientMutableAttribution` in `providertest/client_test.go`. A caller can `SetAttribution(...)` after the `*http.Client` is in flight and subsequent requests will reflect it.

For a streamed OpenAI chat completion, `includeStreamUsage` sets `stream_options.include_usage` in the request, as the Python SDK does: without it OpenAI sends no usage, and the call is recorded with no tokens. OpenAI then ends the stream with one more chunk, which has usage and an empty `choices`. When the request did not already ask for it, `usageChunkFilter` hides that chunk from the caller, whose code may read `chunk.Choices[0]` without checking the length and panic; the recorded stream, read below the filter, keeps it (`TestStreamUsageChunkOnlyWhenAsked`, `TestUsageChunkFilter`). The Responses API rejects that option, so only paths ending in `/chat/completions` get it. `RoundTrip` changes the body on a copy of the request, as the `http.RoundTripper` contract requires: on a 307 or 308, `http.Client` sends the caller's own request again, which with the changed body would get no filter (`TestRedirectedStreamHidesUsageChunk`, `TestRoundTripLeavesRequestAlone`).

There is no retry on the LLM request itself. The 5xx retry transport in `collector.go:retryTransport` is wired only into the collector's own HTTP client, not the user-facing `tracingTransport`. **Don't add retry to `tracingTransport`** — the provider SDK owns retry semantics and double-retry produces duplicate LLM charges.

### Streaming reconstruction (`streaming.go`)

`streamingResponseBody` wraps `resp.Body`. `Read(p)` mirrors bytes to an internal buffer. When `Read` reaches the end of the stream or the stream fails (see "A stream that fails" above), or the caller closes it first, `send` parses the buffer as SSE (`data: {json}\n\n`), builds the response to record in `reconstructResponse`, and fires the payload; `sendOnce` makes that happen once. Sending at the end matters because openai-go and anthropic-sdk-go read a stream to its end but leave closing it to the caller, who often never does (`TestStreamSentOnce`).

A streamed `conversation.response` takes the shape the Python SDK records for that provider, because that is what the backend's extractors read. It is **not** the provider's unstreamed shape: the backend tells a stream apart by its shape (a list of events, or `query.stream` with merged chunks) and reads usage, tool calls and stop reasons from the stream's own events.

| Provider | Recorded `conversation.response` | What the backend reads from it |
|---|---|---|
| OpenAI Chat Completions | every chunk merged as Python's `merge_chunk` does (`mergeChunk`): lists appended, objects merged, other values replaced. So `choices` keeps one entry per chunk, each with its `delta`, and `usage` comes from the last chunk. Each delta gets a `content` key, null if the chunk had none. | each `choices[*].delta`: `content` with no default, then `role` and `tool_calls`; `usage` |
| OpenAI Responses API | the last event's `response`, the same object `responses.create` returns unstreamed | the Response, with `query.stream` marking it streamed |
| Anthropic | the list of events, as sent | a list means streamed; `message_start` gives the model, `message_delta` the output tokens and stop reason, the block deltas the text and tool input |
| Google GenAI | the list of chunks, as sent | a list means streamed; text and `functionCall` parts across chunks, and the last chunk's `usageMetadata`, the only one with full counts on Vertex |

`[DONE]` markers and non-`data:` lines (event headers, comments, blank lines) are silently dropped. An OpenAI stream whose events carry a `response` goes to `openAIResponse` instead of `openAIChatResponse`, since both APIs share `ProviderOpenAI`. Lines have no length limit: a Responses event holds the whole response on one line (`TestOpenAIResponsesStream`). `TestStreamedOpenAIChat` and `TestStreamedEventLists` pin these shapes.

Streaming detection checks the request body, the URL path and the response's `Content-Type`: `isStreamingRequest` JSON-parses `req.Body` and looks for `"stream": true` (OpenAI, Anthropic); `RoundTrip` also treats a URL path ending in `:streamGenerateContent` as a stream, because `google.golang.org/genai` sends no flag in the body, and a response whose `Content-Type` is `text/event-stream` (`isEventStream`), because some calls mark the stream only where the transport doesn't look, such as openai-go's streamed transcriptions, in a multipart form field. A stream that misses every check takes the non-streaming path, whose `captureBody` holds the whole stream back from the caller until it ends — `TestGeminiStreamPassesThrough` and `TestEventStreamPassesThrough` guard this.

SSE `data:` lines are read with or without the space after the colon, as the SSE format and the provider SDKs allow (`TestSSEDataWithoutSpace`).

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

`collector` runs a single worker goroutine that consumes from a buffered channel (`queue chan internal.Payload`, capacity 100). `SendAsync` does a non-blocking send: if the channel is full, the payload is **dropped silently** to keep the user-facing request path non-blocking. That's the design — telemetry must never block LLM calls. It takes no context: a call whose context has ended, as when the end user disconnects, still happened and is still recorded.

`Close()` closes the queue channel and waits on `done` for the worker to drain. Callers should `defer crystilClient.Close()` to ensure in-flight payloads are sent before process exit. **Don't add a graceful-shutdown timeout to `Close()` without thought** — a send can take about 15s, so a backlog of 100 dropped or hung payloads is bounded but not bounded *tight*.

The collector's HTTP client uses `retryTransport`, which tries again after a 5xx, sleeping 1s, 2s, 4s and 8s between tries, up to 5 tries. The client's timeout (`WithTimeout`, 5s by default) and the worker's 10s context end it sooner, but the sleep does not watch them: against a collector answering 503, that is 3 tries over about 7s by default, and 4 over about 15s with a timeout of 10s or more. Network errors (`err != nil` from RoundTrip) are not retried — they return immediately. That mismatch is intentional: a network error usually means we can't reach the collector at all, so retrying just blocks the worker; a 5xx is recoverable.

### Control-plane API client (`api/`)

`api.Client` is a separate HTTP client (not the user-facing wrapped one) for `/v1/-/...` endpoints — workflow management and invocation summaries. It uses `Authorization: Bearer <apiKey>`, where the collector uses no auth header (the API key rides in `meta.api.key` in the payload body). That asymmetry mirrors Python and JS.

`Client.Workflows()` / `Client.Workflow(uuid)` construct fresh `api.Client` instances per call. There's no caching — each control-plane call creates a new `http.Client` with the configured timeout. That's fine because these are user-driven, not in-loop hot paths.

## Python SDK is the source of truth

`../python-sdk` is the reference implementation. When adding a feature, check `../python-sdk/crystil/` first — payload shapes match, names map Python `snake_case` ↔ Go `PascalCase` for exports / `camelCase` for fields, env vars match (`CRYSTIL_API_KEY`, `CRYSTIL_COLLECTOR_URL_BASE`, `CRYSTIL_API_URL_BASE`, `CRYSTIL_TEST_MODE`). `New` takes each URL from its option, then its environment variable, then the production default, as the Ruby SDK does; an empty variable counts as unset.

Known intentional divergences:

- **Architecture: transport interception, not monkey-patching.** Python/JS mutate the caller's LLM client; Go gives the caller a wrapped `*http.Client`. This is a deliberate Go-idiomatic choice (it's how `http.RoundTripper` is meant to be used) and there is no plan to back-port monkey-patching. Don't introduce `register(client)` style wrappers in Go.
- **No sentinel/relevance-intercept support yet.** Python has `raise_if_irrelevant` and JS has `raiseIfIrrelevant`; Go has neither. If/when we add it, the natural place is a pre-RoundTrip hook in `tracingTransport` that issues the intercept POST (via `api.Client`-style transport) before letting the wrapped request through. Don't add it inside `collector` — collector is fire-and-forget by contract.
- **Attribution caps at 100 chars** (matching Python). JS caps at 255. The validator is in `attribution.go:validate`.
- **The usage chunk Crystil asks for is hidden from the caller.** Python and JS also set `stream_options.include_usage`, and pass the extra chunk on. They wrap only the official clients; Go's transport reaches every library on the HTTP client, including ones whose stream loops read `Choices[0]` without checking, so `usageChunkFilter` hides the chunk when the caller did not ask for it.
- **No Vercel AI SDK / Groq / AISDK integrations.** Those are JS-only. Python has `pydantic_ai`; Go doesn't.
- **No `conversation.client.version` reporting.** See "shape" section above — the transport doesn't have a handle on the provider SDK object.
- **`conversation.client.provider` is always `""`**, including for the LangChain callback path (which sets `title: "langchain"`). Python/JS distinguish framework-vs-provider here; Go currently doesn't.
- **Two separate code paths for telemetry capture (transport + callback handler) instead of one.** Python/JS have one `Invoke*`-driven path because they monkey-patch and can intercept anywhere; Go needs the callback handler because `langchaingo` doesn't expose its HTTP client. Keep the two paths' payload shapes aligned via `internal.Payload` — they currently emit the same struct.
- **Test mode skips the POST entirely (no console log).** Python/JS log the payload to stdout in test mode so tests can spy on it; Go just returns `nil` from `collector.send`. For payload-shape assertions in the root package, build the client with `newQueueClient()` (`transport_test.go`), whose collector has no worker, and read the payload from `client.collector.queue` — see `TestStreamedEventLists`; in `providertest/`, read what its fake collector received. Don't bolt on a stdout-log mode just to mimic Python/JS.

## Test conventions

- **`CRYSTIL_TEST_MODE=1` is set in `langchain_test.go:init()`** and applies to the whole package's test binary. As long as a new test lives in `package crystil` (the root), it inherits the env. If you add a test in a sub-package (`api/`, `internal/`), set it in that package's own `init()` or `TestMain`.
- **Don't make real HTTP calls in unit tests.** With `CRYSTIL_TEST_MODE` set, the collector won't POST. For tests that need to exercise `tracingTransport.RoundTrip`, build an `http.Client` whose base transport is a `httptest.Server` (preferred) or a stub `RoundTripper` that returns canned responses. The existing tests largely test components in isolation — `includeStreamUsage`, for one, is called directly, not through HTTP. That pattern is fine when the unit under test doesn't depend on the network path; use `httptest` when it does.
- **Streaming tests construct `streamingResponseBody` directly with `io.NopCloser(strings.NewReader(...))`** as the reader. See `TestStreamingResponseBodyRead`. Reading to the end fires `SendAsync`, which `CRYSTIL_TEST_MODE` makes a no-op; the assertion is on observable state (buffer contents) not on what got sent.
- **Race detector.** The collector mutates `c.attribution` via `SetAttribution` while `tracingTransport.buildPayload` reads it. Currently neither side locks — there's an implicit happens-before via the user-code ordering, but a multi-goroutine test would race. If you add concurrency, also add the necessary `sync.RWMutex` and prove it under `go test -race`.
- **Tests that use a provider SDK go in `providertest/`.** The root package's tests may not import openai-go, anthropic-sdk-go or genai, or those modules land in the SDK's `go.mod`. A `providertest/` test builds the real SDK client on the HTTP client of `newCollector(t)`, points it at an `httptest.Server`, and checks the payloads its fake collector received, as JSON: it is outside the SDK package, so `newQueueClient` and other internals are out of reach. See `TestStreamedOpenAIChat`.
- **Examples are not tests.** `examples/*/main.go` are `package main` programs in the `examples/` module, with no test files. `scripts/in-each-module.sh go build ./...` builds them, as CI does, to catch an example broken by a library change.

## Specialized subagents

`.claude/agents/` contains scoped subagents. Use them when the task matches:

- **`crystil-provider-integrator`** (model: `opus`) — add a new HTTP-based LLM provider (new `Provider` constant, streaming-reconstruction routing, example). Knows the transport-interception model and why "register the client" isn't the Go API.
- **`python-parity-auditor`** (model: `sonnet`) — diff a Go feature/payload against `../python-sdk`. Returns gap list.
- **`streaming-reconstructor`** (model: `opus`) — fix or add SSE chunk reconstruction for a provider. Knows the OpenAI/Anthropic/Google event shapes and the stream shapes the backend reads.
- **`sdk-test-author`** (model: `sonnet`) — write Go tests following the package's `init()`-driven test-mode pattern and `httptest`-or-direct-construction conventions.
