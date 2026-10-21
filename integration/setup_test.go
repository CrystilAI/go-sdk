//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/CrystilAI/go-sdk/internal"
)

const (
	systemPrompt = "Answer in as few words as you can."
	userPrompt   = "What is 2 + 2?"
	toolPrompt   = "What is the weather in San Francisco?"

	weatherTool        = "get_weather"
	weatherDescription = "Get the current weather for a city."
)

// weatherProperties is the JSON schema of the weather tool's one argument.
var weatherProperties = map[string]any{
	"location": map[string]any{"type": "string", "description": "The city, such as San Francisco."},
}

// TestMain gives the SDK the modules this test binary is built with, as a
// production binary records them; Go records none in test binaries.
func TestMain(m *testing.M) {
	deps, err := builtModules()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	internal.BuildDeps = func() []*debug.Module { return deps }
	os.Exit(m.Run())
}

// builtModules asks go list which modules it builds into this package's tests.
func builtModules() ([]*debug.Module, error) {
	out, err := exec.Command("go", "list", "-deps", "-test", "-tags", "integration", "-json=Module", ".").Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	type module struct {
		Path, Version string
		Main          bool
		Replace       *module
	}
	seen := map[string]bool{}
	var deps []*debug.Module
	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		var pkg struct{ Module *module }
		if err := dec.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("go list: %w", err)
		}
		m := pkg.Module
		if m == nil || m.Main || seen[m.Path] {
			continue
		}
		seen[m.Path] = true
		dep := &debug.Module{Path: m.Path, Version: m.Version}
		if m.Replace != nil {
			dep.Replace = &debug.Module{Path: m.Replace.Path, Version: m.Replace.Version}
		}
		deps = append(deps, dep)
	}
	return deps, nil
}

// model returns the environment variable's value, or def. It lets a run try
// another model without editing the tests.
func model(env, def string) string {
	if m := os.Getenv(env); m != "" {
		return m
	}
	return def
}

// recorder stands between the SDK and the real collector: it keeps each payload
// the SDK sends, forwards it, and keeps the collector's answer.
type recorder struct {
	client    *crystil.Client
	target    string
	closeOnce sync.Once

	mu   sync.Mutex
	sent []delivery
}

// delivery is one payload and the collector's answer to it.
type delivery struct {
	body   []byte
	status int
	answer []byte
	err    error
}

// start skips the test unless CRYSTIL_COLLECTOR_URL_BASE and each of keys are set.
// It returns a Crystil client whose payloads reach that collector through a
// recorder, and a context with a new transaction and attribution for its calls.
func start(t *testing.T, keys ...string) (context.Context, *crystil.Client, *recorder) {
	t.Helper()
	target := os.Getenv("CRYSTIL_COLLECTOR_URL_BASE")
	if target == "" {
		t.Skip("CRYSTIL_COLLECTOR_URL_BASE is not set")
	}
	for _, key := range keys {
		if os.Getenv(key) == "" {
			t.Skipf("%s is not set", key)
		}
	}

	rec := &recorder{target: strings.TrimSuffix(target, "/")}
	server := httptest.NewServer(http.HandlerFunc(rec.forward))
	t.Cleanup(server.Close)

	client, err := crystil.New(crystil.WithCollectorURL(server.URL))
	if err != nil {
		t.Fatalf("crystil.New: %v (is CRYSTIL_API_KEY set?)", err)
	}
	rec.client = client
	t.Cleanup(rec.close)

	parentName, subsidiaryID, subsidiaryName := "Abc", "456", "Def"
	ctx, err := crystil.SetAttribution(crystil.NewTransaction(t.Context()), &crystil.Attribution{
		ParentID:       "123",
		ParentName:     &parentName,
		SubsidiaryID:   &subsidiaryID,
		SubsidiaryName: &subsidiaryName,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, client, rec
}

func (r *recorder) forward(w http.ResponseWriter, req *http.Request) {
	d := delivery{status: http.StatusBadGateway}
	d.body, d.err = io.ReadAll(req.Body)
	if d.err == nil {
		d.status, d.answer, d.err = r.send(req, d.body)
	}
	r.mu.Lock()
	r.sent = append(r.sent, d)
	r.mu.Unlock()
	w.WriteHeader(d.status)
}

// send posts the payload to the real collector.
func (r *recorder) send(req *http.Request, body []byte) (int, []byte, error) {
	out, err := http.NewRequestWithContext(req.Context(), req.Method, r.target+req.URL.Path, bytes.NewReader(body))
	if err != nil {
		return http.StatusBadGateway, nil, err
	}
	out.Header.Set("Content-Type", req.Header.Get("Content-Type"))
	resp, err := http.DefaultClient.Do(out)
	if err != nil {
		return http.StatusBadGateway, nil, err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 1000))
	return resp.StatusCode, answer, err
}

// close waits until the client has sent every payload it queued.
func (r *recorder) close() {
	r.closeOnce.Do(func() { _ = r.client.Close() })
}

// payload returns the one payload the client sent, failing the test unless the
// collector accepted it.
func (r *recorder) payload(t *testing.T) map[string]any {
	t.Helper()
	r.close()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.sent {
		if d.err != nil {
			t.Fatalf("forward payload to collector: %v", d.err)
		}
		if d.status < 200 || d.status > 299 {
			t.Fatalf("collector answered %d: %s", d.status, d.answer)
		}
	}
	if len(r.sent) != 1 {
		t.Fatalf("SDK sent %d payloads, want 1", len(r.sent))
	}

	var p map[string]any
	if err := json.Unmarshal(r.sent[0].body, &p); err != nil {
		t.Fatal(err)
	}
	dump(t, p)
	return p
}

// checkPayload checks the fields every payload has.
func checkPayload(t *testing.T, p map[string]any, title, model string) {
	t.Helper()
	want := map[string]any{
		"conversation.client.title": title,
		"conversation.query.model":  model,
		"meta.sdk.client":           "go",
		"meta.fnfg.status":          "succeeded",
		"attribution.parent.id":     "123",
	}
	for path, value := range want {
		if got := get(p, path); got != value {
			t.Errorf("%s = %v, want %v", path, got, value)
		}
	}
	if version, _ := get(p, "conversation.client.version").(string); version == "" {
		t.Error("conversation.client.version is empty")
	}
	switch response := get(p, "conversation.response").(type) {
	case map[string]any:
		if len(response) == 0 {
			t.Error("conversation.response is empty")
		}
	case []any:
		if len(response) == 0 {
			t.Error("conversation.response has no events")
		}
	default:
		t.Errorf("conversation.response = %#v, want an object or a list of events", response)
	}
	// The backend reads these with fromtimestamp, after the collector has accepted the payload.
	for _, path := range []string{"time.start", "time.end"} {
		if _, ok := get(p, path).(float64); !ok {
			t.Errorf("%s = %#v, want Unix seconds as a number", path, get(p, path))
		}
	}
	if uuid, _ := get(p, "tx.uuid").(string); uuid == "" {
		t.Error("tx.uuid is empty")
	}
}

// checkChatStream checks a streamed chat completion is recorded as the backend
// reads it: the chunks merged, each delta with a content key, and usage, which
// the SDK asks OpenAI for.
func checkChatStream(t *testing.T, p map[string]any) {
	t.Helper()
	if get(p, "conversation.response.usage") == nil {
		t.Error("conversation.response.usage is missing")
	}
	choices, _ := get(p, "conversation.response.choices").([]any)
	if len(choices) == 0 {
		t.Error("conversation.response.choices is empty")
	}
	for i, c := range choices {
		delta, _ := get(c.(map[string]any), "delta").(map[string]any)
		if _, ok := delta["content"]; !ok {
			t.Errorf("conversation.response.choices[%d].delta has no content key", i)
		}
	}
}

// checkEventList checks a stream is recorded as the list of its events, which
// the backend reads as a stream.
func checkEventList(t *testing.T, p map[string]any) {
	t.Helper()
	if events, _ := get(p, "conversation.response").([]any); len(events) == 0 {
		t.Errorf("conversation.response = %#v, want the list of stream events", get(p, "conversation.response"))
	}
}

// get returns the value at a dotted path in a decoded JSON object, or nil.
func get(m map[string]any, path string) any {
	var v any = m
	for _, key := range strings.Split(path, ".") {
		object, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = object[key]
	}
	return v
}

// dump writes the payload to output/ when CRYSTIL_DUMP_RESPONSES is set, as the
// Python SDK's tests do. The Crystil API key is masked.
func dump(t *testing.T, p map[string]any) {
	t.Helper()
	if os.Getenv("CRYSTIL_DUMP_RESPONSES") == "" {
		return
	}
	var masked map[string]any
	body, _ := json.Marshal(p)
	_ = json.Unmarshal(body, &masked)
	if api, ok := get(masked, "meta.api").(map[string]any); ok {
		api["key"] = "masked"
	}

	out, err := json.MarshalIndent(masked, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("output", strings.ReplaceAll(t.Name(), "/", "__")+".json")
	if err := os.MkdirAll("output", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}
