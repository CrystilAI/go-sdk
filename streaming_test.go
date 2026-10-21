package crystil

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/CrystilAI/go-sdk/internal"
)

func TestIsStreamingRequest(t *testing.T) {
	tests := []struct {
		name     string
		reqBody  map[string]interface{}
		expected bool
	}{
		{
			name: "streaming enabled",
			reqBody: map[string]interface{}{
				"model":  "gpt-4",
				"stream": true,
			},
			expected: true,
		},
		{
			name: "streaming disabled",
			reqBody: map[string]interface{}{
				"model":  "gpt-4",
				"stream": false,
			},
			expected: false,
		},
		{
			name: "no stream field",
			reqBody: map[string]interface{}{
				"model": "gpt-4",
			},
			expected: false,
		},
		{
			name:     "empty body",
			reqBody:  map[string]interface{}{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.reqBody)
			result := isStreamingRequest(body)
			if result != tt.expected {
				t.Errorf("isStreamingRequest() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsStreamingRequestInvalidJSON(t *testing.T) {
	result := isStreamingRequest([]byte("invalid json"))
	if result {
		t.Error("Expected isStreamingRequest to return false for invalid JSON")
	}
}

func TestStreamingResponseBodyRead(t *testing.T) {
	// Create a mock streaming response
	mockData := "data: {\"test\": \"chunk1\"}\n\ndata: {\"test\": \"chunk2\"}\n\n"
	reader := io.NopCloser(strings.NewReader(mockData))

	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	srb := newStreamingResponseBody(
		reader,
		client,
		ProviderOpenAI,
		[]byte(`{"model":"gpt-4","stream":true}`),
		time.Now(),
		context.Background(),
	)

	// Read the streaming data
	buf := make([]byte, 1024)
	n, err := srb.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() error = %v", err)
	}

	if n == 0 {
		t.Error("Expected to read some data")
	}

	// Verify data was buffered
	if srb.buffer.Len() == 0 {
		t.Error("Expected buffer to contain accumulated data")
	}

	// Close and verify analytics sent
	err = srb.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestIncludeStreamUsage(t *testing.T) {
	const chat = "/v1/chat/completions"
	tests := []struct {
		name string
		path string
		body string
		want string // "" when the body must not change
	}{
		{"chat stream", chat, `{"model":"m","stream":true}`,
			`{"model":"m","stream":true,"stream_options":{"include_usage":true}}`},
		{"keeps other stream options", chat, `{"stream":true,"stream_options":{"include_obfuscation":false}}`,
			`{"stream":true,"stream_options":{"include_obfuscation":false,"include_usage":true}}`},
		{"overrides include_usage false", chat, `{"stream":true,"stream_options":{"include_usage":false}}`,
			`{"stream":true,"stream_options":{"include_usage":true}}`},
		{"keeps large numbers exact", chat, `{"seed":9007199254740993,"stream":true}`,
			`{"seed":9007199254740993,"stream":true,"stream_options":{"include_usage":true}}`},
		{"already asks for usage", chat, `{"stream":true,"stream_options":{"include_usage":true}}`, ""},
		{"not a stream", chat, `{"model":"m"}`, ""},
		{"responses api stream", "/v1/responses", `{"model":"m","stream":true}`, ""},
		{"not json", chat, `stream`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := includeStreamUsage(tc.path, []byte(tc.body))
			if tc.want == "" {
				if changed || string(got) != tc.body {
					t.Errorf("includeStreamUsage() = %s, %v; want the body unchanged", got, changed)
				}
				return
			}
			if !changed || string(got) != tc.want {
				t.Errorf("includeStreamUsage() = %s, %v; want %s, true", got, changed, tc.want)
			}
		})
	}
}

// TestStreamedEventLists checks that Anthropic and Google streams are recorded as
// the list of events they sent, as the Python SDK records them: the backend reads
// a list as a stream and takes usage and tool calls from the events.
func TestStreamedEventLists(t *testing.T) {
	streams := []struct {
		name     string
		provider Provider
		path     string
		events   []string
	}{
		{"anthropic", ProviderAnthropic, "/v1/messages", []string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"Paris\"}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":5}}`,
			`{"type":"message_stop"}`,
		}},
		{"google", ProviderGoogle, "/v1beta/models/gemini-2.5-flash:streamGenerateContent", []string{
			`{"candidates":[{"content":{"parts":[{"text":"2"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":9,"totalTokenCount":9},"modelVersion":"gemini-2.5-flash"}`,
			`{"candidates":[{"content":{"parts":[{"text":" + 2 = 4"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":8,"totalTokenCount":17},"modelVersion":"gemini-2.5-flash"}`,
		}},
	}
	for _, tc := range streams {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, e := range tc.events {
					var event struct{ Type string }
					_ = json.Unmarshal([]byte(e), &event)
					if event.Type != "" {
						_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
					}
					_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
				}
			}))
			defer server.Close()

			client := newQueueClient()
			resp, err := client.HTTPClient(tc.provider).Post(server.URL+tc.path, "application/json", strings.NewReader(`{"stream":true}`))
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.ReadAll(resp.Body)

			var payload internal.Payload
			select {
			case payload = <-client.collector.queue:
			case <-time.After(time.Second):
				t.Fatal("no payload recorded")
			}
			got, _ := json.Marshal(payload.Conversation.Response)
			want, _ := json.Marshal(decodeAll(t, tc.events))
			if !bytes.Equal(got, want) {
				t.Errorf("response = %s\nwant the events as sent: %s", got, want)
			}
		})
	}
}

// A response that says it is an event stream reaches the caller as it arrives,
// though its request marked the stream in no way the transport reads, as
// openai-go's streamed transcriptions send stream as a multipart form field.
func TestEventStreamPassesThrough(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, "data: {\"type\":\"transcript.text.delta\",\"delta\":\"hi\"}\n\n")
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release) // runs first, so the server can close

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("stream", "true")
	_ = form.Close()
	httpClient := newQueueClient().HTTPClient(ProviderOpenAI)
	type result struct {
		line string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := httpClient.Post(server.URL+"/v1/audio/transcriptions", form.FormDataContentType(), &body)
		if err != nil {
			got <- result{err: err}
			return
		}
		defer resp.Body.Close()
		line, err := bufio.NewReader(resp.Body).ReadString('\n')
		got <- result{line, err}
	}()

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if !strings.HasPrefix(r.line, "data: ") {
			t.Errorf("first line = %q, want the first event", r.line)
		}
	case <-time.After(time.Second):
		t.Fatal("the caller got nothing while the stream was still open")
	}
}

// SSE lets a data line leave out the space after its colon, and the provider
// SDKs read such a stream, as an OpenAI-compatible server may send. Crystil too.
func TestSSEDataWithoutSpace(t *testing.T) {
	tests := []struct {
		provider Provider
		stream   string
		want     string
	}{
		{ProviderOpenAI, "data:{\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata:[DONE]\n\n",
			`{"choices":[{"delta":{"content":"hi"},"index":0}],"id":"c1"}`},
		{ProviderAnthropic, "event:message_stop\ndata:{\"type\":\"message_stop\"}\n\n", `[{"type":"message_stop"}]`},
	}
	for _, tt := range tests {
		client := newQueueClient()
		s := newStreamingResponseBody(io.NopCloser(strings.NewReader(tt.stream)), client, tt.provider,
			nil, time.Now(), context.Background())
		_, _ = io.ReadAll(s)
		select {
		case payload := <-client.collector.queue:
			if got, _ := json.Marshal(payload.Conversation.Response); string(got) != tt.want {
				t.Errorf("%s: response = %s, want %s", tt.provider, got, tt.want)
			}
		default:
			t.Errorf("%s: no payload recorded", tt.provider)
		}
	}
}

// A stream whose context has ended, as when the end user disconnects, is still
// recorded when it is closed. It was once dropped at random, so one try is not enough.
func TestStreamSentAfterContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 50; i++ {
		client := newQueueClient()
		s := newStreamingResponseBody(io.NopCloser(strings.NewReader("data: {}\n\n")), client, ProviderOpenAI,
			nil, time.Now(), ctx)
		if _, err := s.Read(make([]byte, 64)); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
		if got := len(client.collector.queue); got != 1 {
			t.Fatalf("try %d: %d payloads queued, want 1", i, got)
		}
	}
}

// The usage chunk includeStreamUsage asked for is hidden from the caller whole,
// however the stream is split into reads and whichever line ends it uses. Other
// chunks with no choices, such as Azure's content filter results, pass.
func TestUsageChunkFilter(t *testing.T) {
	const (
		filter  = `data: {"choices":[],"prompt_filter_results":[{"prompt_index":0}]}`
		content = `data: {"choices":[{"index":0,"delta":{"content":"hi"}}],"usage":null}`
		usage   = `data: {"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`
		done    = `data: [DONE]`
	)
	for _, lineEnd := range []string{"\n", "\r\n"} {
		event := func(line string) string { return line + lineEnd + lineEnd }
		stream := event(filter) + event(content) + event(usage) + event(done)
		f := &usageChunkFilter{body: io.NopCloser(iotest.OneByteReader(strings.NewReader(stream)))}
		got, err := io.ReadAll(iotest.OneByteReader(f))
		if err != nil {
			t.Fatal(err)
		}
		if want := event(filter) + event(content) + event(done); string(got) != want {
			t.Errorf("caller read %q, want the stream without the usage chunk: %q", got, want)
		}
	}
}

// A stream with no chunks, as when it fails before its first, is not recorded,
// as the Python SDK records none: the backend would store a call with no tokens.
func TestEmptyStreamNotRecorded(t *testing.T) {
	for _, provider := range []Provider{ProviderOpenAI, ProviderAnthropic, ProviderGoogle} {
		client := newQueueClient()
		s := newStreamingResponseBody(io.NopCloser(strings.NewReader(": keep-alive\n\n")), client, provider,
			nil, time.Now(), context.Background())
		_, _ = io.ReadAll(s)
		_ = s.Close()
		if got := len(client.collector.queue); got != 0 {
			t.Errorf("%s: %d payloads queued, want none", provider, got)
		}
	}
}

// A stream that fails partway, on the error event its provider SDK stops at or on
// a read error, is recorded as failed, as the Python SDK records it. The SDK reads
// no further, and the caller may never close it, so it is sent at once.
func TestFailedStreamRecorded(t *testing.T) {
	const (
		openAIChunk    = "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n"
		anthropicStart = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-haiku-4-5\"}}\n\n"
	)
	tests := []struct {
		name     string
		provider Provider
		stream   string
		drop     bool // the connection drops after the stream; else it stays open
		wantExc  string
	}{
		{"openai error event", ProviderOpenAI,
			openAIChunk + "data: {\"error\":{\"message\":\"boom\",\"type\":\"server_error\"}}\n\n", false, "boom"},
		{"anthropic error event", ProviderAnthropic, anthropicStart +
			"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n", false, "Overloaded"},
		{"connection dropped", ProviderOpenAI, openAIChunk, true, "unexpected EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newQueueClient()
			body := io.NopCloser(io.MultiReader(strings.NewReader(tt.stream), errorReader{io.ErrUnexpectedEOF}))
			if !tt.drop {
				reader, writer := io.Pipe()
				defer writer.Close()
				go func() { _, _ = io.WriteString(writer, tt.stream) }()
				body = reader
			}
			s := newStreamingResponseBody(body, client, tt.provider, nil, time.Now(), context.Background())
			if tt.drop {
				_, _ = io.ReadAll(s)
			} else if _, err := io.ReadFull(s, make([]byte, len(tt.stream))); err != nil {
				t.Fatal(err)
			}

			select {
			case payload := <-client.collector.queue:
				if status, exc := payload.Meta.FNFG.Status, payload.Meta.FNFG.Exc; status != "failed" || exc == nil || *exc != tt.wantExc {
					t.Errorf("status = %q, exc = %v; want failed, with exc %q", status, exc, tt.wantExc)
				}
				if got, _ := json.Marshal(payload.Conversation.Response); bytes.Contains(got, []byte(`"error"`)) {
					t.Errorf("response = %s, want the chunks without the error event", got)
				}
			case <-time.After(time.Second):
				t.Fatal("no payload recorded for a stream that failed and was neither read to its end nor closed")
			}
		})
	}
}

// A stream whose only event is an error is not recorded, as the Python SDK
// records none: it holds no chunk the call returned.
func TestErrorOnlyStreamNotRecorded(t *testing.T) {
	streams := map[Provider]string{
		ProviderOpenAI:    "data: {\"error\":{\"message\":\"boom\"}}\n\n",
		ProviderAnthropic: "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n",
	}
	for provider, stream := range streams {
		client := newQueueClient()
		s := newStreamingResponseBody(io.NopCloser(strings.NewReader(stream)), client, provider,
			nil, time.Now(), context.Background())
		_, _ = io.ReadAll(s)
		_ = s.Close()
		if got := len(client.collector.queue); got != 0 {
			t.Errorf("%s: %d payloads queued, want none", provider, got)
		}
	}
}

// A stream the caller stops by ending its context did not fail: it is recorded
// as succeeded, as the Python SDK records a stream the caller abandons.
func TestCanceledStreamNotFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := newQueueClient()
	body := io.NopCloser(io.MultiReader(
		strings.NewReader("data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n"),
		cancelThenFail{cancel},
	))
	s := newStreamingResponseBody(body, client, ProviderOpenAI, nil, time.Now(), ctx)
	_, _ = io.ReadAll(s)
	_ = s.Close()

	select {
	case payload := <-client.collector.queue:
		if status := payload.Meta.FNFG.Status; status != "succeeded" {
			t.Errorf("status = %q, want succeeded for a stream the caller stopped", status)
		}
	case <-time.After(time.Second):
		t.Fatal("no payload recorded")
	}
}

// cancelThenFail ends the context, then fails the read as net/http does then.
type cancelThenFail struct{ cancel context.CancelFunc }

func (r cancelThenFail) Read([]byte) (int, error) {
	r.cancel()
	return 0, context.Canceled
}

// Closing a stream while another goroutine reads it, which net/http's own
// response body allows, must not race on the buffer: go test -race catches it.
func TestStreamClosedWhileRead(t *testing.T) {
	reader, writer := io.Pipe()
	go func() {
		for {
			if _, err := io.WriteString(writer, "data: {}\n\n"); err != nil {
				return
			}
		}
	}()
	s := newStreamingResponseBody(reader, newQueueClient(), ProviderOpenAI, nil, time.Now(),
		context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64)
		for {
			if _, err := s.Read(buf); err != nil {
				return
			}
		}
	}()

	time.Sleep(time.Millisecond)
	_ = s.Close() // closes reader, which ends both goroutines
	<-done
}

// decodeAll decodes each JSON text.
func decodeAll(t *testing.T, texts []string) []interface{} {
	t.Helper()
	var values []interface{}
	for _, text := range texts {
		var v interface{}
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			t.Fatal(err)
		}
		values = append(values, v)
	}
	return values
}

func TestCaptureBody(t *testing.T) {
	testData := "test body content"
	body := io.NopCloser(bytes.NewReader([]byte(testData)))

	captured, err := captureBody(body)
	if err != nil {
		t.Fatalf("captureBody() error = %v", err)
	}

	if string(captured) != testData {
		t.Errorf("Expected '%s', got '%s'", testData, string(captured))
	}
}

func TestCaptureBodyNil(t *testing.T) {
	captured, err := captureBody(nil)
	if err != nil {
		t.Fatalf("captureBody(nil) error = %v", err)
	}

	if captured != nil {
		t.Error("Expected nil for nil body")
	}
}
