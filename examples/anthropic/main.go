package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/PayloopAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
	// Initialize Payloop client
	payloopClient, err := payloop.New(payloop.WithAPIKey(os.Getenv("PAYLOOP_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer payloopClient.Close()

	// Get wrapped HTTP client for Anthropic
	httpClient := payloopClient.HTTPClient(payloop.ProviderAnthropic)

	// Create Anthropic client with your own configuration
	anthropicClient := anthropic.NewClient(
		option.WithHTTPClient(httpClient),
		option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
	)

	// Set attribution to track costs by user (affects all future requests)
	err = payloopClient.SetAttribution(&payloop.Attribution{
		Parent: payloop.AttributionEntity{
			ID:   "user-456",
			Name: "Jane Smith",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Use Anthropic as normal - analytics tracked automatically
	message, err := anthropicClient.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("What is the capital of France?")),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print the first text block from the response
	for _, block := range message.Content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			fmt.Printf("Response: %s\n", textBlock.Text)
			break
		}
	}

	// Update attribution for a different user (affects same anthropicClient)
	err = payloopClient.SetAttribution(&payloop.Attribution{
		Parent: payloop.AttributionEntity{
			ID:   "user-789",
			Name: "Bob Johnson",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// This request will be attributed to Bob Johnson
	message2, err := anthropicClient.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("What is 2 + 2?")),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print the first text block from the response
	for _, block := range message2.Content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			fmt.Printf("Response: %s\n", textBlock.Text)
			break
		}
	}
}
