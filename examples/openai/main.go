package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/PayloopAI/go-sdk"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func main() {
	// Initialize Payloop client
	payloopClient, err := payloop.New(payloop.WithAPIKey(os.Getenv("PAYLOOP_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer payloopClient.Close()

	// Get wrapped HTTP client for OpenAI
	httpClient := payloopClient.HTTPClient(payloop.ProviderOpenAI)

	// Create OpenAI client with your own configuration
	openaiClient := openai.NewClient(
		option.WithHTTPClient(httpClient),
		option.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
	)

	// Set attribution to track costs by user (affects all future requests)
	err = payloopClient.SetAttribution(&payloop.Attribution{
		Parent: payloop.AttributionEntity{
			ID:   "user-123",
			Name: "John Doe",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Use OpenAI as normal - analytics tracked automatically
	resp, err := openaiClient.Chat.Completions.New(
		context.Background(),
		openai.ChatCompletionNewParams{
			Model: openai.ChatModelGPT4o,
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("What is the capital of France?"),
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", resp.Choices[0].Message.Content)

	// Update attribution for a different user (affects same openaiClient)
	err = payloopClient.SetAttribution(&payloop.Attribution{
		Parent: payloop.AttributionEntity{
			ID:   "user-456",
			Name: "Jane Smith",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// This request will be attributed to Jane Smith
	resp2, err := openaiClient.Chat.Completions.New(
		context.Background(),
		openai.ChatCompletionNewParams{
			Model: openai.ChatModelGPT4o,
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("What is 2 + 2?"),
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", resp2.Choices[0].Message.Content)
}
