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

	crystil "github.com/CrystilAI/go-sdk"
	"google.golang.org/genai"
)

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
