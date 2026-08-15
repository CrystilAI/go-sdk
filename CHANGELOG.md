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