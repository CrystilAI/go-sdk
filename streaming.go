package crystil

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/CrystilAI/go-sdk/internal"
)

// streamingResponseBody wraps a streaming response body to accumulate chunks for analytics.
// It reads transparently while building a complete response for tracking.
type streamingResponseBody struct {
	reader    io.ReadCloser
	mu        sync.Mutex // guards buffer, failure and checked: send can run while another goroutine reads
	buffer    *bytes.Buffer
	failure   string // why the stream failed, or ""
	checked   int    // how much of buffer findErrorEvent has looked at
	client    *Client
	provider  Provider
	reqBody   []byte
	startTime time.Time
	ctx       context.Context
	sendOnce  sync.Once
}

// newStreamingResponseBody creates a streaming wrapper.
func newStreamingResponseBody(
	reader io.ReadCloser,
	client *Client,
	provider Provider,
	reqBody []byte,
	startTime time.Time,
	ctx context.Context,
) *streamingResponseBody {
	return &streamingResponseBody{
		reader:    reader,
		buffer:    &bytes.Buffer{},
		client:    client,
		provider:  provider,
		reqBody:   reqBody,
		startTime: startTime,
		ctx:       ctx,
	}
}

// Read implements io.Reader, accumulating chunks as they're read. It sends
// analytics at the end of the stream, or when the stream fails: openai-go and
// anthropic-sdk-go read no further, and leave closing the body to the caller.
func (s *streamingResponseBody) Read(p []byte) (n int, err error) {
	n, err = s.reader.Read(p)
	failed := false
	if n > 0 {
		s.mu.Lock()
		s.buffer.Write(p[:n])
		failed = s.findErrorEvent()
		s.mu.Unlock()
	}
	// A read that fails while the context is live fails the stream, as the Python
	// SDK records it. One that fails after the context ended was stopped by the caller.
	if err != nil && err != io.EOF && s.ctx.Err() == nil {
		s.mu.Lock()
		if s.failure == "" {
			s.failure = err.Error()
		}
		s.mu.Unlock()
		failed = true
	}
	if err == io.EOF || failed {
		s.sendOnce.Do(s.send)
	}
	return n, err
}

// findErrorEvent looks at each whole event not looked at yet for the error
// event the provider SDK stops reading at, keeps the first one's message as the
// stream's failure, and reports whether the stream has failed. Call it with mu held.
func (s *streamingResponseBody) findErrorEvent() bool {
	for s.failure == "" {
		rest := s.buffer.Bytes()[s.checked:]
		end := eventEnd(rest)
		if end < 0 {
			break
		}
		for _, line := range bytes.Split(rest[:end], []byte("\n")) {
			data, ok := bytes.CutPrefix(bytes.TrimRight(line, "\r"), []byte("data:"))
			if !ok || !bytes.Contains(data, []byte(`"error"`)) {
				continue
			}
			var chunk map[string]interface{}
			if json.Unmarshal(data, &chunk) != nil {
				continue
			}
			if message, ok := errorEvent(s.provider, chunk); ok {
				s.failure = message
				break
			}
		}
		s.checked += end
	}
	return s.failure != ""
}

// errorEvent reports whether chunk is an error event the provider SDK stops
// reading at and raises, and returns its message: for OpenAI a chunk whose
// "error" is not null, for Anthropic one of type "error". Gemini's errors are chunks.
func errorEvent(provider Provider, chunk map[string]interface{}) (string, bool) {
	switch {
	case provider == ProviderOpenAI && chunk["error"] != nil:
	case provider == ProviderAnthropic && chunk["type"] == "error":
	default:
		return "", false
	}
	if e, ok := chunk["error"].(map[string]interface{}); ok {
		if message, ok := e["message"].(string); ok && message != "" {
			return message, true
		}
	}
	message, _ := json.Marshal(chunk["error"])
	return string(message), true
}

// Close implements io.Closer, sending analytics for a stream the caller
// stopped reading before its end. It may run while another goroutine reads.
func (s *streamingResponseBody) Close() error {
	defer s.reader.Close()
	s.sendOnce.Do(s.send)
	return nil
}

// send builds the analytics payload from the stream read so far and sends it.
// Call it through sendOnce: a stream sends one payload.
func (s *streamingResponseBody) send() {
	// Reconstruct complete response from SSE chunks. A stream with none is not
	// recorded, as the Python SDK records none: it holds no call to count.
	response := s.reconstructResponse()
	if response == nil {
		return
	}

	s.mu.Lock()
	var exc *string
	if s.failure != "" {
		failure := s.failure
		exc = &failure
	}
	s.mu.Unlock()
	status := "succeeded"
	if exc != nil {
		status = "failed"
	}

	// Parse request
	var query map[string]interface{}
	if len(s.reqBody) > 0 {
		_ = json.Unmarshal(s.reqBody, &query)
	}

	// Build attribution for payload (read from client dynamically)
	var attr interface{}
	if s.client.attribution != nil {
		parentMap := map[string]interface{}{
			"id": s.client.attribution.ParentID,
		}
		if s.client.attribution.ParentName != nil {
			parentMap["name"] = *s.client.attribution.ParentName
		}

		attr = map[string]interface{}{
			"parent": parentMap,
		}

		if s.client.attribution.SubsidiaryID != nil {
			subsidiaryMap := map[string]interface{}{
				"id": *s.client.attribution.SubsidiaryID,
			}
			if s.client.attribution.SubsidiaryName != nil {
				subsidiaryMap["name"] = *s.client.attribution.SubsidiaryName
			}
			attr.(map[string]interface{})["subsidiary"] = subsidiaryMap
		}
	}

	// Build and send analytics payload
	payload := internal.Payload{
		Attribution: attr,
		Conversation: internal.Conversation{
			Client: internal.ClientInfo{
				Provider: "",
				Title:    string(s.provider),
				Version:  "",
			},
			Query:    query,
			Response: response,
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: s.client.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    exc,
				Status: status,
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: s.client.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: s.startTime,
			End:   time.Now(),
		},
		Tx: internal.Transaction{
			UUID: s.client.txID,
		},
	}

	s.client.collector.SendAsync(payload)
}

// reconstructResponse parses the SSE chunks into the response to record, in the
// shape the Python SDK records for each provider, which is what the backend reads.
// It returns nil for a stream with no chunks.
func (s *streamingResponseBody) reconstructResponse() interface{} {
	s.mu.Lock()
	content := s.buffer.String()
	s.mu.Unlock()
	scanner := bufio.NewScanner(strings.NewReader(content))
	// A Responses API event holds the whole Response on one line, which can pass
	// the scanner's default 64 KiB limit.
	scanner.Buffer(nil, len(content)+1)

	var chunks []map[string]interface{}
	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {json}", where the space is optional
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			data = strings.TrimPrefix(data, " ")

			// Skip [DONE] markers
			if data == "[DONE]" {
				continue
			}

			// Parse chunk. The provider SDK raises an error event instead of returning
			// it, so it is no chunk of the call, as the Python SDK records it.
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				if _, isError := errorEvent(s.provider, chunk); !isError {
					chunks = append(chunks, chunk)
				}
			}
		}
	}

	if len(chunks) == 0 {
		return nil
	}

	if s.provider == ProviderOpenAI {
		if response := openAIResponse(chunks); response != nil {
			return response
		}
		return openAIChatResponse(chunks)
	}
	// Anthropic and Google: the events as sent. The backend reads a list as a
	// stream, and takes usage, stop reasons and tool calls from the events.
	return chunks
}

// openAIResponse returns the Response object a Responses API stream ends with,
// or nil for a Chat Completions stream. Each "response.*" event that carries the
// Response holds a newer copy, so the last one wins.
func openAIResponse(chunks []map[string]interface{}) map[string]interface{} {
	var response map[string]interface{}
	for _, chunk := range chunks {
		eventType, _ := chunk["type"].(string)
		if r, ok := chunk["response"].(map[string]interface{}); ok && strings.HasPrefix(eventType, "response.") {
			response = r
		}
	}
	return response
}

// openAIChatResponse merges a chat completion stream's chunks, so choices keeps
// each chunk's delta and usage comes from the last chunk. Each delta gets a
// content key, null if the chunk had none: the backend reads it without a default.
func openAIChatResponse(chunks []map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	for _, chunk := range chunks {
		mergeChunk(merged, chunk)
	}
	choices, _ := merged["choices"].([]interface{})
	for _, c := range choices {
		choice, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			delta = map[string]interface{}{}
			choice["delta"] = delta
		}
		if _, ok := delta["content"]; !ok {
			delta["content"] = nil
		}
	}
	return merged
}

// mergeChunk merges chunk into data as the Python SDK's merge_chunk does: lists
// are appended, objects merged, and anything else replaced.
func mergeChunk(data, chunk map[string]interface{}) map[string]interface{} {
	for key, value := range chunk {
		switch have := data[key].(type) {
		case []interface{}:
			if more, ok := value.([]interface{}); ok {
				data[key] = append(have, more...)
				continue
			}
		case map[string]interface{}:
			if more, ok := value.(map[string]interface{}); ok {
				mergeChunk(have, more)
				continue
			}
		}
		data[key] = ownCopy(value)
	}
	return data
}

// ownCopy copies a list or object, so a later merge never changes a chunk.
func ownCopy(value interface{}) interface{} {
	switch v := value.(type) {
	case []interface{}:
		return append([]interface{}(nil), v...)
	case map[string]interface{}:
		return mergeChunk(map[string]interface{}{}, v)
	}
	return value
}
