// Package integration calls live LLM providers through the SDK and checks that the
// Crystil collector at CRYSTIL_COLLECTOR_URL_BASE accepts every payload. These tests
// cost money; see CONTRIBUTE.md for how to run them. They build only with the
// integration tag; this file keeps the package buildable without it.
package integration
