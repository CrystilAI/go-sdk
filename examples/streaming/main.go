package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/PayloopAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"google.golang.org/genai"
)

func main() {
	// Initialize Payloop client
	client, err := payloop.New(payloop.WithAPIKey(os.Getenv("PAYLOOP_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// Set attribution to track costs by user
	client, err = client.SetAttribution(payloop.Attribution{
		Parent: payloop.AttributionEntity{
			ID:   "user-streaming",
			Name: "Streaming User",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example 1: OpenAI Streaming
	fmt.Println("=== OpenAI Streaming ===")
	streamingOpenAI(ctx, client)

	// Example 2: Anthropic Streaming
	fmt.Println("\n=== Anthropic Streaming ===")
	streamingAnthropic(ctx, client)

	// Example 3: Google GenAI Streaming
	fmt.Println("\n=== Google GenAI Streaming ===")
	streamingGoogle(ctx, client)
}

func streamingOpenAI(ctx context.Context, client *payloop.Client) {
	// Register OpenAI client
	openaiClient := client.WrapOpenAIClient(ctx, os.Getenv("OPENAI_API_KEY"))

	// Create streaming request
	stream := openaiClient.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("Write a haiku about Go programming"),
		},
	})

	fmt.Print("Response: ")

	// Read the stream
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}

	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}

	fmt.Println() // New line after stream ends

	// Analytics are automatically sent when the stream closes!
	// The SDK accumulates chunks and reconstructs the complete response.
}

func streamingAnthropic(ctx context.Context, client *payloop.Client) {
	// Register Anthropic client
	anthropicClient := client.WrapAnthropicClient(ctx, os.Getenv("ANTHROPIC_API_KEY"))

	// Create streaming request
	stream := anthropicClient.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Write a haiku about Go programming")),
		},
	})

	fmt.Print("Response: ")

	// Read the stream
	for stream.Next() {
		event := stream.Current()
		// Handle content block delta events to extract text
		switch eventVariant := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			switch deltaVariant := eventVariant.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				fmt.Print(deltaVariant.Text)
			}
		}
	}

	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}

	fmt.Println() // New line after stream ends

	// Analytics are automatically sent when the stream closes!
	// The SDK accumulates chunks and reconstructs the complete response.
}

func streamingGoogle(ctx context.Context, client *payloop.Client) {
	// Register Google GenAI client
	googleClient, err := client.WrapGoogleClient(ctx, os.Getenv("GOOGLE_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Print("Response: ")

	// Create streaming request using range-over-function pattern
	for resp, err := range googleClient.Models.GenerateContentStream(
		ctx,
		"gemini-2.0-flash",
		[]*genai.Content{{Parts: []*genai.Part{{Text: "Write a haiku about Go programming"}}}},
		nil,
	) {
		if err != nil {
			log.Fatal(err)
		}
		// Print each chunk as it arrives
		fmt.Print(resp.Text())
	}

	fmt.Println() // New line after stream ends

	// Analytics are automatically sent when the stream closes!
	// The SDK accumulates chunks and reconstructs the complete response.
}
