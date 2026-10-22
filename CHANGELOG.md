# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2025-10-10

### Added
- Initial release of the Crystil Go SDK (published as `payloop` at the time)

## [0.1.1] - 2025-10-10

### Added
- `LICENSE`

## [0.1.2] - 2025-10-10

### Changed
- Updated `LICENSE`

## [0.1.3] - 2025-10-10

### Changed
- Updated the public interface

## [0.1.4] - 2025-10-10

### Changed
- Flattened `Attribution`

## [0.1.5] - 2025-10-10

### Changed
- Updated documentation and examples, including the LangChain example

## [0.2.0] - 2026-08-12

### Changed
- **BREAKING**: Renamed Payloop to Crystil. Migrating from `0.1.5` requires:
  - Update the module path: `go get github.com/CrystilAI/go-sdk` in place of the old path, and
    update every import.
  - The package name is now `crystil` (e.g. `payloop.New(...)` → `crystil.New(...)`).
  - Rename the `PAYLOOP_API_KEY` and `PAYLOOP_TEST_MODE` environment variables to
    `CRYSTIL_API_KEY` and `CRYSTIL_TEST_MODE`.
  - Default endpoints now resolve to `api.crystil.com` and `collector.crystil.com`.

### Fixed
- Restored the SDK's own import in the examples, which had been dropped rather than renamed and
  left all seven failing to compile

## [0.2.1] - 2026-08-14

### Added
- Automated module publishing on release: publishing a GitHub Release now gates on formatting,
  vet, build, and tests, then warms `proxy.golang.org` and `index.golang.org` so the version is
  fetchable and listed on pkg.go.dev immediately

## [0.3.0] - 2026-10-22

### Removed
- **BREAKING**: `Client.NewLangChainHandler`. Pass `crystilClient.HTTPClient(provider)` to
  langchaingo's `WithHTTPClient` instead.
- **BREAKING**: `Client.SetAttribution` and `Client.NewTransaction`. Call
  `ctx, err = crystil.SetAttribution(ctx, attr)` or `ctx = crystil.NewTransaction(ctx)`, and pass
  `ctx` to the provider call.
- **BREAKING**: `ErrInvalidClient` and `ErrAlreadyRegistered`. Nothing returned them, so a check
  against either was always false; delete it.

### Added
- `Client.NewHTTPClient` and `Client.WrapTransport`, for a library that needs its own HTTP client,
  such as Google's on Vertex AI, or takes a transport.

### Changed
- Streamed OpenAI chat completions set `stream_options.include_usage`, so they are recorded with
  tokens. The extra usage chunk is hidden from a caller that did not ask for it.
- A response with status 400 or more, and a stream with no chunks, are no longer recorded.
- A stream that fails after its first chunk is recorded as failed, with the reason, as soon as it
  fails.
- `SetAttribution` rejects a `SubsidiaryName` without a `SubsidiaryID`, and counts its
  100-character caps in characters, not bytes. Its errors match `ErrInvalidAttribution` with
  `errors.Is`.

### Fixed
- Streams are recorded when read to their end, not only when closed, in the shapes the backend
  reads.
- Gemini streams, and event streams the request does not flag, reach the caller as they arrive.
- A response body cut short made openai-go and anthropic-sdk-go send the call again.
- The backend rejected every Go payload, whose times were strings: they are now Unix seconds.
- A stream that is neither read to its end nor closed, as langchaingo's `googleai` leaves some, is
  recorded when its request's context ends.
- A request that ended after `Client.Close` panicked.
- A payload with no attribution sends `attribution` as `null`, as the other SDKs do; the backend
  rejected one without the key.
- In an OpenAI tool loop, the backend rejected each call after the first, whose assistant message
  has no `content`; it is now recorded with `content: null`.
