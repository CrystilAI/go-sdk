package providertest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/httptransport"
	crystil "github.com/CrystilAI/go-sdk"
	"google.golang.org/genai"
)

// staticToken lets a Vertex genai client run without Google credentials.
type staticToken struct{}

func (staticToken) Token(context.Context) (*auth.Token, error) {
	return &auth.Token{Value: "test-token"}, nil
}

func TestGoogleQueryNamesModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const body = `{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"}}]}`
		if strings.HasSuffix(r.URL.Path, ":countTokens") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"totalTokens":1}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: " + body + "\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	backends := []struct {
		name   string
		config genai.ClientConfig
	}{
		{"Gemini API", genai.ClientConfig{Backend: genai.BackendGeminiAPI, APIKey: "test-key"}},
		{"Vertex", genai.ClientConfig{
			Backend:     genai.BackendVertexAI,
			Project:     "test-project",
			Location:    "us-central1",
			Credentials: auth.NewCredentials(&auth.CredentialsOptions{TokenProvider: staticToken{}}),
		}},
	}
	// Only generate calls name the model: the backend would record any other call
	// that names one, such as countTokens, as a call to that model.
	calls := []struct {
		name  string
		model any
		call  func(ctx context.Context, models *genai.Models, contents []*genai.Content) error
	}{
		{"generate", "gemini-2.0-flash", func(ctx context.Context, models *genai.Models, contents []*genai.Content) error {
			_, err := models.GenerateContent(ctx, "gemini-2.0-flash", contents, nil)
			return err
		}},
		{"stream", "gemini-2.0-flash", func(ctx context.Context, models *genai.Models, contents []*genai.Content) error {
			for _, err := range models.GenerateContentStream(ctx, "gemini-2.0-flash", contents, nil) {
				if err != nil {
					return err
				}
			}
			return nil
		}},
		{"count tokens", nil, func(ctx context.Context, models *genai.Models, contents []*genai.Content) error {
			_, err := models.CountTokens(ctx, "gemini-2.0-flash", contents, nil)
			return err
		}},
	}

	for _, backend := range backends {
		for _, call := range calls {
			t.Run(backend.name+" "+call.name, func(t *testing.T) {
				ctx := context.Background()
				c := newCollector(t)

				config := backend.config
				config.HTTPClient = c.client.HTTPClient(crystil.ProviderGoogle)
				config.HTTPOptions = genai.HTTPOptions{BaseURL: server.URL}
				googleClient, err := genai.NewClient(ctx, &config)
				if err != nil {
					t.Fatal(err)
				}

				contents := []*genai.Content{{Parts: []*genai.Part{{Text: "hi"}}}}
				if err := call.call(ctx, googleClient.Models, contents); err != nil {
					t.Fatal(err)
				}

				if got := get(c.payload(t), "conversation.query.model"); got != call.model {
					t.Errorf("query model = %v, want %v", got, call.model)
				}
			})
		}
	}
}

func TestGeminiStreamPassesThrough(t *testing.T) {
	// The server holds the stream open after the first chunk until the test has it.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const chunk = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":%q}],\"role\":\"model\"}}]}\n\n"
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, chunk, "chunk1 ")
		w.(http.Flusher).Flush()
		<-release
		_, _ = fmt.Fprintf(w, chunk, "chunk2")
	}))
	defer server.Close()
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()

	ctx := context.Background()
	c := newCollector(t)
	googleClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:     genai.BackendGeminiAPI,
		APIKey:      "test-key",
		HTTPClient:  c.client.HTTPClient(crystil.ProviderGoogle),
		HTTPOptions: genai.HTTPOptions{BaseURL: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}

	first := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		contents := []*genai.Content{{Parts: []*genai.Part{{Text: "hi"}}}}
		for resp, err := range googleClient.Models.GenerateContentStream(ctx, "gemini-2.0-flash", contents, nil) {
			if err != nil {
				done <- err
				return
			}
			select {
			case first <- resp.Text():
			default:
			}
		}
		done <- nil
	}()

	select {
	case text := <-first:
		if text != "chunk1 " {
			t.Errorf("first chunk = %q, want %q", text, "chunk1 ")
		}
	case <-time.After(time.Second):
		t.Fatal("first chunk not passed through before the stream ended")
	}
	releaseOnce()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	var chunks []struct {
		Candidates []struct {
			Content struct{ Parts []struct{ Text string } }
		}
	}
	data, _ := json.Marshal(get(c.payload(t), "conversation.response"))
	_ = json.Unmarshal(data, &chunks)
	var texts []string
	for _, chunk := range chunks {
		for _, candidate := range chunk.Candidates {
			for _, part := range candidate.Content.Parts {
				texts = append(texts, part.Text)
			}
		}
	}
	if strings.Join(texts, "|") != "chunk1 |chunk2" {
		t.Errorf("recorded response = %s, want both chunks, in order", data)
	}
}

// TestVertexWithGoogleAuth gives genai Crystil's copy of Google's auth client, as
// examples/google-vertex does: genai adds no credentials to a client it is
// given, so the call must reach Vertex with that client's, and be recorded.
func TestVertexWithGoogleAuth(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"}}]}`))
	}))
	defer server.Close()

	ctx := context.Background()
	c := newCollector(t)
	httpClient, err := httptransport.NewClient(&httptransport.Options{
		Credentials: auth.NewCredentials(&auth.CredentialsOptions{TokenProvider: staticToken{}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	googleClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:     genai.BackendVertexAI,
		Project:     "test-project",
		Location:    "us-central1",
		HTTPClient:  c.client.NewHTTPClient(crystil.ProviderGoogle, httpClient),
		HTTPOptions: genai.HTTPOptions{BaseURL: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}

	contents := []*genai.Content{{Parts: []*genai.Part{{Text: "hi"}}}}
	if _, err := googleClient.Models.GenerateContent(ctx, "gemini-2.0-flash", contents, nil); err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer test-token" {
		t.Errorf("Authorization = %q, want the token from Google's auth transport", authorization)
	}
	if got := get(c.payload(t), "conversation.query.model"); got != "gemini-2.0-flash" {
		t.Errorf("query model = %v, want gemini-2.0-flash", got)
	}
}
