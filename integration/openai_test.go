//go:build integration

package integration

import (
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

func TestOpenAI(t *testing.T) {
	m := model("OPENAI_MODEL", openai.ChatModelGPT4oMini)
	chat := openai.ChatCompletionNewParams{
		Model: m,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	}
	tools := openai.ChatCompletionNewParams{
		Model:    m,
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage(toolPrompt)},
		Tools: []openai.ChatCompletionToolUnionParam{openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        weatherTool,
			Description: openai.String(weatherDescription),
			Parameters:  openai.FunctionParameters{"type": "object", "properties": weatherProperties, "required": []string{"location"}},
		})},
		ToolChoice: openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("required")},
	}

	cases := []struct {
		name   string
		params openai.ChatCompletionNewParams
		stream bool
	}{
		{"chat", chat, false},
		{"chat stream", chat, true},
		{"tools", tools, false},
		{"tools stream", tools, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, client, rec := start(t, "OPENAI_API_KEY")
			ai := openai.NewClient(option.WithHTTPClient(client.HTTPClient(crystil.ProviderOpenAI)))

			var resp openai.ChatCompletion
			if tc.stream {
				stream := ai.Chat.Completions.NewStreaming(ctx, tc.params)
				var acc openai.ChatCompletionAccumulator
				for stream.Next() {
					acc.AddChunk(stream.Current())
				}
				if err := stream.Err(); err != nil {
					t.Fatal(err)
				}
				if err := stream.Close(); err != nil {
					t.Fatal(err)
				}
				resp = acc.ChatCompletion
			} else {
				r, err := ai.Chat.Completions.New(ctx, tc.params)
				if err != nil {
					t.Fatal(err)
				}
				resp = *r
			}
			if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" && len(resp.Choices[0].Message.ToolCalls) == 0 {
				t.Fatal("model gave no answer")
			}

			p := rec.payload(t)
			checkPayload(t, p, string(crystil.ProviderOpenAI), m)
			if tc.stream {
				checkChatStream(t, p)
			}
		})
	}
}

func TestOpenAIResponses(t *testing.T) {
	m := model("OPENAI_MODEL", openai.ChatModelGPT4oMini)
	plain := responses.ResponseNewParams{
		Model:        m,
		Instructions: openai.String(systemPrompt),
		Input:        responses.ResponseNewParamsInputUnion{OfString: openai.String(userPrompt)},
	}
	tools := responses.ResponseNewParams{
		Model: m,
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(toolPrompt)},
		Tools: []responses.ToolUnionParam{{OfFunction: &responses.FunctionToolParam{
			Name:        weatherTool,
			Description: openai.String(weatherDescription),
			Parameters:  map[string]any{"type": "object", "properties": weatherProperties, "required": []string{"location"}},
			Strict:      openai.Bool(false),
		}}},
		ToolChoice: responses.ResponseNewParamsToolChoiceUnion{OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsRequired)},
	}

	cases := []struct {
		name   string
		params responses.ResponseNewParams
		stream bool
	}{
		{"responses", plain, false},
		{"responses stream", plain, true},
		{"tools", tools, false},
		{"tools stream", tools, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, client, rec := start(t, "OPENAI_API_KEY")
			ai := openai.NewClient(option.WithHTTPClient(client.HTTPClient(crystil.ProviderOpenAI)))

			var resp responses.Response
			if tc.stream {
				stream := ai.Responses.NewStreaming(ctx, tc.params)
				for stream.Next() {
					if event := stream.Current(); event.Type == "response.completed" {
						resp = event.Response
					}
				}
				if err := stream.Err(); err != nil {
					t.Fatal(err)
				}
				if err := stream.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				r, err := ai.Responses.New(ctx, tc.params)
				if err != nil {
					t.Fatal(err)
				}
				resp = *r
			}
			answered := resp.OutputText() != ""
			for _, item := range resp.Output {
				answered = answered || item.Type == "function_call"
			}
			if !answered {
				t.Fatal("model gave no answer")
			}

			p := rec.payload(t)
			checkPayload(t, p, string(crystil.ProviderOpenAI), m)
			// A stream must record its final Response, which carries the token usage.
			if status := get(p, "conversation.response.status"); status != "completed" {
				t.Errorf("conversation.response.status = %v, want completed", status)
			}
			if get(p, "conversation.response.usage") == nil {
				t.Error("conversation.response.usage is missing")
			}
		})
	}
}
