//go:build integration

package integration

import (
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestAnthropic(t *testing.T) {
	m := model("ANTHROPIC_MODEL", "claude-haiku-4-5-20251001")
	chat := anthropic.MessageNewParams{
		Model:     anthropic.Model(m),
		MaxTokens: 256,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt))},
	}
	tools := anthropic.MessageNewParams{
		Model:     anthropic.Model(m),
		MaxTokens: 256,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(toolPrompt))},
		Tools: []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
			Name:        weatherTool,
			Description: anthropic.String(weatherDescription),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: weatherProperties, Required: []string{"location"}},
		}}},
		ToolChoice: anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}},
	}

	cases := []struct {
		name   string
		params anthropic.MessageNewParams
		stream bool
	}{
		{"messages", chat, false},
		{"messages stream", chat, true},
		{"tools", tools, false},
		{"tools stream", tools, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, client, rec := start(t, "ANTHROPIC_API_KEY")
			ai := anthropic.NewClient(option.WithHTTPClient(client.HTTPClient(crystil.ProviderAnthropic)))

			var resp anthropic.Message
			if tc.stream {
				stream := ai.Messages.NewStreaming(ctx, tc.params)
				for stream.Next() {
					if err := resp.Accumulate(stream.Current()); err != nil {
						t.Fatal(err)
					}
				}
				if err := stream.Err(); err != nil {
					t.Fatal(err)
				}
				if err := stream.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				r, err := ai.Messages.New(ctx, tc.params)
				if err != nil {
					t.Fatal(err)
				}
				resp = *r
			}
			if len(resp.Content) == 0 {
				t.Fatal("model gave no answer")
			}

			p := rec.payload(t)
			checkPayload(t, p, string(crystil.ProviderAnthropic), m)
			if tc.stream {
				checkEventList(t, p)
			}
		})
	}
}
