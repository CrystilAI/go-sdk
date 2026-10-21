package main

import (
	"context"
	"fmt"
	"log"
	"os"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

func main() {
	// Initialize Crystil client
	crystilClient, err := crystil.New(crystil.WithAPIKey(os.Getenv("CRYSTIL_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	// Set attribution to track costs by user (affects all future requests)
	langchainUser := "LangChain User"
	err = crystilClient.SetAttribution(&crystil.Attribution{
		ParentID:   "user-langchain",
		ParentName: &langchainUser,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Give LangChain the Crystil HTTP client, as with any provider SDK.
	// Crystil sees that langchaingo sent the request and records it as LangChain's.
	llm, err := openai.New(
		openai.WithModel("gpt-4o-mini"),
		openai.WithToken(os.Getenv("OPENAI_API_KEY")),
		openai.WithHTTPClient(crystilClient.HTTPClient(crystil.ProviderOpenAI)),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Use LangChain as normal - analytics tracked automatically
	response, err := llms.GenerateFromSinglePrompt(
		ctx,
		llm,
		"What are the three laws of robotics?",
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", response)

	// Create a new transaction for the next request
	newTxClient := crystilClient.NewTransaction()

	llm2, err := openai.New(
		openai.WithModel("gpt-4o-mini"),
		openai.WithToken(os.Getenv("OPENAI_API_KEY")),
		openai.WithHTTPClient(newTxClient.HTTPClient(crystil.ProviderOpenAI)),
	)
	if err != nil {
		log.Fatal(err)
	}

	response2, err := llms.GenerateFromSinglePrompt(
		ctx,
		llm2,
		"Explain quantum computing in one sentence",
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", response2)

	// All LLM calls are automatically tracked through the Crystil HTTP client!
}
