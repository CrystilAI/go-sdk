package crystil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func init() {
	// Enable test mode to prevent actual network calls
	os.Setenv("CRYSTIL_TEST_MODE", "1")
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		setup   func()
		cleanup func()
		wantErr bool
	}{
		{
			name:    "no api key and no env var",
			opts:    []Option{},
			wantErr: true,
		},
		{
			name:    "with api key option",
			opts:    []Option{WithAPIKey("test-key")},
			wantErr: false,
		},
		{
			name: "from environment variable",
			opts: []Option{},
			setup: func() {
				os.Setenv("CRYSTIL_API_KEY", "env-test-key")
			},
			cleanup: func() {
				os.Unsetenv("CRYSTIL_API_KEY")
			},
			wantErr: false,
		},
		{
			name:    "empty api key",
			opts:    []Option{WithAPIKey("")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			if tt.cleanup != nil {
				defer tt.cleanup()
			}

			client, err := New(tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				defer client.Close()
				if client.cfg.apiKey == "" {
					t.Error("Expected API key to be set")
				}
				if client.txID == "" {
					t.Error("Expected transaction ID to be set")
				}
			}
		})
	}
}

func TestNewURLs(t *testing.T) {
	tests := []struct {
		name          string
		envCollector  string
		envAPI        string
		opts          []Option
		wantCollector string
		wantAPI       string
	}{
		{
			name:          "defaults",
			wantCollector: "https://collector.crystil.com",
			wantAPI:       "https://api.crystil.com",
		},
		{
			name:          "from environment variables",
			envCollector:  "http://localhost:8002",
			envAPI:        "http://localhost:8000",
			wantCollector: "http://localhost:8002",
			wantAPI:       "http://localhost:8000",
		},
		{
			name:          "options win over environment variables",
			envCollector:  "http://localhost:8002",
			envAPI:        "http://localhost:8000",
			opts:          []Option{WithCollectorURL("http://collector.test"), WithAPIURL("http://api.test")},
			wantCollector: "http://collector.test",
			wantAPI:       "http://api.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// An empty value counts as unset, as in the JS and Ruby SDKs.
			t.Setenv("CRYSTIL_COLLECTOR_URL_BASE", tt.envCollector)
			t.Setenv("CRYSTIL_API_URL_BASE", tt.envAPI)

			client, err := New(append([]Option{WithAPIKey("test-key")}, tt.opts...)...)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			if client.collector.endpoint != tt.wantCollector+"/rec" {
				t.Errorf("collector endpoint = %q, want %q", client.collector.endpoint, tt.wantCollector+"/rec")
			}
			if client.cfg.apiURL != tt.wantAPI {
				t.Errorf("API URL = %q, want %q", client.cfg.apiURL, tt.wantAPI)
			}
		})
	}
}

func TestWithTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{
			name:    "valid timeout",
			timeout: 10 * time.Second,
			wantErr: false,
		},
		{
			name:    "zero timeout",
			timeout: 0,
			wantErr: true,
		},
		{
			name:    "negative timeout",
			timeout: -5 * time.Second,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(
				WithAPIKey("test-key"),
				WithTimeout(tt.timeout),
			)
			if (err != nil) != tt.wantErr {
				t.Errorf("WithTimeout() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewTransaction(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	originalTxID := client.txID

	newClient := client.NewTransaction()

	if newClient.txID == originalTxID {
		t.Error("Expected new transaction ID, got same ID")
	}

	if client.txID != originalTxID {
		t.Error("Original client's transaction ID was modified")
	}
}

func TestSetAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	johnDoe := "John Doe"
	acmeCorp := "Acme Corp"
	engineering := "Engineering"
	teamID := "team-456"

	tests := []struct {
		name    string
		attr    *Attribution
		wantErr bool
	}{
		{
			name: "valid parent only",
			attr: &Attribution{
				ParentID:   "user-123",
				ParentName: &johnDoe,
			},
			wantErr: false,
		},
		{
			name: "valid parent and subsidiary",
			attr: &Attribution{
				ParentID:       "org-123",
				ParentName:     &acmeCorp,
				SubsidiaryID:   &teamID,
				SubsidiaryName: &engineering,
			},
			wantErr: false,
		},
		{
			name: "missing parent ID",
			attr: &Attribution{
				ParentName: &johnDoe,
			},
			wantErr: true,
		},
		{
			name: "parent ID too long",
			attr: &Attribution{
				ParentID: string(make([]byte, 101)),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.SetAttribution(tt.attr)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetAttribution() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && client.attribution == nil {
				t.Error("Expected attribution to be set")
			}
		})
	}
}

func TestHTTPClient(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tests := []struct {
		name     string
		provider Provider
	}{
		{"OpenAI", ProviderOpenAI},
		{"Anthropic", ProviderAnthropic},
		{"Google", ProviderGoogle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := client.HTTPClient(tt.provider)
			if httpClient == nil {
				t.Errorf("Expected non-nil HTTP client for %s", tt.provider)
			}
			if httpClient.Transport == nil {
				t.Errorf("Expected non-nil transport for %s", tt.provider)
			}
		})
	}
}

// A request that ends after the client is closed, as during a server's shutdown,
// is dropped instead of crashing the program. Closing twice is safe too.
func TestRequestAfterClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","choices":[]}`)
	}))
	defer server.Close()

	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	httpClient := client.HTTPClient(ProviderOpenAI)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	resp, err := httpClient.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-4o-mini"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

// HTTPClient sets only the client's Transport: Crystil's, over http.DefaultTransport.
func TestHTTPClientSetsOnlyTransport(t *testing.T) {
	httpClient := newQueueClient().HTTPClient(ProviderOpenAI)
	if !reflect.DeepEqual(*httpClient, http.Client{Transport: httpClient.Transport}) {
		t.Errorf("HTTPClient() = %+v, want a client with only Transport set", httpClient)
	}
	transport, ok := httpClient.Transport.(*tracingTransport)
	if !ok || transport.base != http.DefaultTransport || transport.provider != ProviderOpenAI {
		t.Errorf("Transport = %#v, want Crystil's for openai, over http.DefaultTransport", httpClient.Transport)
	}
}

// roundTripFunc lets a function stand in for an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// WrapTransport records each call and sends it on through the base it is given,
// or http.DefaultTransport when that is nil.
func TestWrapTransport(t *testing.T) {
	client := newQueueClient()
	var sent *http.Request
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sent = req
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"c1"}`)), Request: req}, nil
	})

	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.WrapTransport(ProviderOpenAI, base).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if sent == nil {
		t.Error("base got no request")
	}
	select {
	case payload := <-client.collector.queue:
		if got := payload.Conversation.Query["model"]; got != "gpt-4o-mini" {
			t.Errorf("query model = %v, want gpt-4o-mini", got)
		}
	default:
		t.Error("no payload recorded")
	}

	if transport, _ := client.WrapTransport(ProviderOpenAI, nil).(*tracingTransport); transport == nil || transport.base != http.DefaultTransport {
		t.Errorf("WrapTransport(nil) base = %#v, want http.DefaultTransport", transport)
	}
}

// NewHTTPClient returns a copy of the client it is given, with the same settings,
// that records each call and sends it on through that client's transport. The
// client it is given stays as it was.
func TestNewHTTPClient(t *testing.T) {
	client := newQueueClient()
	var sent bool
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sent = true
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"c1"}`)), Request: req}, nil
	})
	given := &http.Client{Transport: base, Timeout: time.Minute}

	httpClient := client.NewHTTPClient(ProviderOpenAI, given)
	resp, err := httpClient.Post("https://api.openai.com/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-4o-mini"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !sent {
		t.Error("the given client's transport got no request")
	}
	if len(client.collector.queue) != 1 {
		t.Errorf("%d payloads recorded, want 1", len(client.collector.queue))
	}
	if httpClient.Timeout != time.Minute {
		t.Errorf("Timeout = %v, want the given client's", httpClient.Timeout)
	}
	if _, ok := given.Transport.(roundTripFunc); !ok {
		t.Errorf("given client's Transport = %#v, want it unchanged", given.Transport)
	}

	if got := client.NewHTTPClient(ProviderOpenAI, nil); !reflect.DeepEqual(got, client.HTTPClient(ProviderOpenAI)) {
		t.Errorf("NewHTTPClient(nil) = %+v, want what HTTPClient returns", got)
	}
}

// idleCloser records whether CloseIdleConnections reached it.
type idleCloser struct {
	http.RoundTripper
	closed bool
}

func (c *idleCloser) CloseIdleConnections() { c.closed = true }

// CloseIdleConnections on a client NewHTTPClient returns reaches the transport
// of the client it was given.
func TestCloseIdleConnections(t *testing.T) {
	base := &idleCloser{}
	newQueueClient().NewHTTPClient(ProviderOpenAI, &http.Client{Transport: base}).CloseIdleConnections()
	if !base.closed {
		t.Error("CloseIdleConnections did not reach the given client's transport")
	}
}
