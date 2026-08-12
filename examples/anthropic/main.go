package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/CrystilAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
	// Initialize Crystil client
	crystilClient, err := crystil.New(crystil.WithAPIKey(os.Getenv("CRYSTIL_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	// Get wrapped HTTP client for Anthropic
	httpClient := crystilClient.HTTPClient(crystil.ProviderAnthropic)

	// Create Anthropic client with your own configuration
	anthropicClient := anthropic.NewClient(
		option.WithHTTPClient(httpClient),
	)

	// Set attribution to track costs by user (affects all future requests)
	janeSmith := "Jane Smith"
	err = crystilClient.SetAttribution(&crystil.Attribution{
		ParentID:   "user-456",
		ParentName: &janeSmith,
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
	bobJohnson := "Bob Johnson"
	err = crystilClient.SetAttribution(&crystil.Attribution{
		ParentID:   "user-789",
		ParentName: &bobJohnson,
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
