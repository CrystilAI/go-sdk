package providertest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// openai-go leaves content out of an assistant message that has only tool calls,
// as ToParam builds one to send back. The backend reads each message's content
// with no default, so the next turn of a tool loop still records one.
func TestToolLoopQueryHasContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[`+
			`{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[`+
			`{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()

	c := newCollector(t)
	ai := openai.NewClient(option.WithHTTPClient(c.client.HTTPClient(crystil.ProviderOpenAI)),
		option.WithBaseURL(server.URL), option.WithAPIKey("test-key"))
	params := openai.ChatCompletionNewParams{
		Model:    "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("weather in Paris?")},
	}
	completion, err := ai.Chat.Completions.New(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	params.Messages = append(params.Messages, completion.Choices[0].Message.ToParam(), openai.ToolMessage("sunny", "call_1"))
	if _, err := ai.Chat.Completions.New(context.Background(), params); err != nil {
		t.Fatal(err)
	}

	got := c.payloads()
	if len(got) != 2 {
		t.Fatalf("collector got %d payloads, want one for each turn", len(got))
	}
	messages, _ := get(got[1], "conversation.query.messages").([]any)
	if len(messages) != 3 {
		t.Fatalf("second turn's query.messages = %v, want the user, assistant and tool messages", messages)
	}
	for i, m := range messages {
		if _, ok := m.(map[string]any)["content"]; !ok {
			t.Errorf("query.messages[%d] = %v has no content key, which the backend reads with no default", i, m)
		}
	}
}
