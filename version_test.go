package crystil

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"

	"github.com/CrystilAI/go-sdk/internal"
)

func TestModuleVersion(t *testing.T) {
	const module = "github.com/openai/openai-go"
	tests := []struct {
		name string
		deps []*debug.Module
		want string
	}{
		{"no major version suffix", []*debug.Module{{Path: module, Version: "v1.12.0"}}, "1.12.0"},
		{"major version suffix", []*debug.Module{{Path: module + "/v3", Version: "v3.2.0"}}, "3.2.0"},
		{"replaced by another version", []*debug.Module{
			{Path: module + "/v3", Version: "v3.2.0", Replace: &debug.Module{Path: module + "/v3", Version: "v3.3.0"}},
		}, "3.3.0"},
		{"replaced by a local copy", []*debug.Module{
			{Path: module + "/v3", Version: "v3.2.0", Replace: &debug.Module{Path: "../openai-go"}},
		}, ""},
		{"two major versions", []*debug.Module{
			{Path: module + "/v2", Version: "v2.7.1"},
			{Path: module + "/v3", Version: "v3.2.0"},
		}, ""},
		{"similar path", []*debug.Module{
			{Path: module + "-extra", Version: "v1.0.0"},
			{Path: module + "/vendor", Version: "v1.0.0"},
		}, ""},
		{"missing", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleVersion(tc.deps, module); got != tc.want {
				t.Errorf("moduleVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSDKVersion(t *testing.T) {
	saved := internal.BuildDeps
	t.Cleanup(func() { internal.BuildDeps = saved })
	internal.BuildDeps = func() []*debug.Module {
		return []*debug.Module{
			{Path: "github.com/openai/openai-go/v3", Version: "v3.2.0"},
			{Path: "github.com/anthropics/anthropic-sdk-go", Version: "v1.13.0"},
			{Path: "google.golang.org/genai", Version: "v1.29.0"},
		}
	}
	tests := []struct {
		name     string
		provider Provider
		header   http.Header
		want     string
	}{
		{"openai-go", ProviderOpenAI, http.Header{"X-Stainless-Package-Version": {"3.2.0"}}, "3.2.0"},
		{"anthropic-sdk-go", ProviderAnthropic, http.Header{"X-Stainless-Package-Version": {"1.13.0"}}, "1.13.0"},
		{"genai", ProviderGoogle, http.Header{"X-Goog-Api-Client": {"google-genai-sdk/1.29.0 gl-go/go1.24.0"}}, "1.29.0"},
		{"another library", ProviderOpenAI, http.Header{"User-Agent": {"Go-http-client/1.1"}}, ""},
		{"another SDK version", ProviderOpenAI, http.Header{"X-Stainless-Package-Version": {"1.13.0"}}, ""},
		{"another Google library", ProviderGoogle, http.Header{"X-Goog-Api-Client": {"gl-go/go1.24.0 gccl/0.9.0"}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sdkVersion(tc.provider, tc.header); got != tc.want {
				t.Errorf("sdkVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLangChainPayloadNamesVersion checks that LangChain payloads report
// langchaingo's version, since langchaingo calls providers with its own clients.
func TestLangChainPayloadNamesVersion(t *testing.T) {
	saved := internal.BuildDeps
	t.Cleanup(func() { internal.BuildDeps = saved })
	internal.BuildDeps = func() []*debug.Module {
		return []*debug.Module{{Path: "github.com/tmc/langchaingo", Version: "v0.1.13"}}
	}

	ctx := context.Background()
	calls := []struct {
		name string
		call func(h *langChainHandler)
	}{
		{"end", func(h *langChainHandler) { h.HandleLLMEnd(ctx, map[string]any{"text": "hi"}) }},
		{"generate content end", func(h *langChainHandler) {
			h.HandleLLMGenerateContentEnd(ctx, &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "hi"}}})
		}},
		{"error", func(h *langChainHandler) { h.HandleLLMError(ctx, errors.New("rate limited")) }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			client := newQueueClient()
			tc.call(client.NewLangChainHandler().(*langChainHandler))
			select {
			case payload := <-client.collector.queue:
				if got := payload.Conversation.Client.Version; got != "0.1.13" {
					t.Errorf("conversation.client.version = %q, want %q", got, "0.1.13")
				}
			default:
				t.Fatal("no payload sent")
			}
		})
	}
}

// A program can build in two major versions of an SDK, as during a migration;
// the request header names the one that sent it.
func TestSDKVersionTwoMajorVersions(t *testing.T) {
	saved := internal.BuildDeps
	t.Cleanup(func() { internal.BuildDeps = saved })
	internal.BuildDeps = func() []*debug.Module {
		return []*debug.Module{
			{Path: "github.com/openai/openai-go", Version: "v1.12.0"},
			{Path: "github.com/openai/openai-go/v3", Version: "v3.2.0"},
		}
	}
	for header, want := range map[string]string{"1.12.0": "1.12.0", "3.2.0": "3.2.0", "2.0.0": ""} {
		if got := sdkVersion(ProviderOpenAI, http.Header{"X-Stainless-Package-Version": {header}}); got != want {
			t.Errorf("sdkVersion() for a request from %s = %q, want %q", header, got, want)
		}
	}
}

// TestOtherLibraryReportsNoVersion checks that a request another library sends
// through HTTPClient reports no version, even with the official SDK built in.
func TestOtherLibraryReportsNoVersion(t *testing.T) {
	saved := internal.BuildDeps
	t.Cleanup(func() { internal.BuildDeps = saved })
	internal.BuildDeps = func() []*debug.Module {
		return []*debug.Module{{Path: "github.com/openai/openai-go/v3", Version: "v3.2.0"}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","choices":[]}`)
	}))
	defer server.Close()

	client := newQueueClient()
	resp, err := client.HTTPClient(ProviderOpenAI).Post(
		server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-4o-mini","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	select {
	case payload := <-client.collector.queue:
		if got := payload.Conversation.Client.Version; got != "" {
			t.Errorf("conversation.client.version = %q, want none", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no payload recorded")
	}
}
