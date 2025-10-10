package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/PayloopAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicOption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	openaiOption "github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"
)

func main() {
	// Initialize Payloop client
	payloopClient, err := payloop.New(payloop.WithAPIKey(os.Getenv("PAYLOOP_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer payloopClient.Close()

	// Set attribution to track costs by user
	streamingUser := "Streaming User"
	err = payloopClient.SetAttribution(&payloop.Attribution{
		ParentID:   "user-streaming",
		ParentName: &streamingUser,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example 1: OpenAI Streaming
	fmt.Println("=== OpenAI Streaming ===")
	streamingOpenAI(ctx, payloopClient)

	// Example 2: Anthropic Streaming
	fmt.Println("\n=== Anthropic Streaming ===")
	streamingAnthropic(ctx, payloopClient)

	// Example 3: Google GenAI Streaming
	fmt.Println("\n=== Google GenAI Streaming ===")
	streamingGoogle(ctx, payloopClient)
}

func streamingOpenAI(ctx context.Context, payloopClient *payloop.Client) {
	// Get wrapped HTTP client for OpenAI
	httpClient := payloopClient.HTTPClient(payloop.ProviderOpenAI)

	// Create OpenAI client with your own configuration
	openaiClient := openai.NewClient(
		openaiOption.WithHTTPClient(httpClient),
		openaiOption.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
	)

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

func streamingAnthropic(ctx context.Context, payloopClient *payloop.Client) {
	// Get wrapped HTTP client for Anthropic
	httpClient := payloopClient.HTTPClient(payloop.ProviderAnthropic)

	// Create Anthropic client with your own configuration
	anthropicClient := anthropic.NewClient(
		anthropicOption.WithHTTPClient(httpClient),
		anthropicOption.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
	)

	stream := anthropicClient.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Write a haiku about Go programming")),
		},
	})

	fmt.Print("Response: ")
	for stream.Next() {
		event := stream.Current()
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

	fmt.Println()
}

func streamingGoogle(ctx context.Context, payloopClient *payloop.Client) {
	// Get wrapped HTTP client for Google
	httpClient := payloopClient.HTTPClient(payloop.ProviderGoogle)

	// Create Google GenAI client with your own configuration
	googleClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		HTTPClient: httpClient,
		APIKey:     os.Getenv("GOOGLE_API_KEY"),
		Backend:    genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Print("Response: ")
	for resp, err := range googleClient.Models.GenerateContentStream(
		ctx,
		"gemini-2.0-flash",
		[]*genai.Content{{Parts: []*genai.Part{{Text: "Write a haiku about Go programming"}}}},
		nil,
	) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(resp.Text())
	}

	fmt.Println()
}
