package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/CrystilAI/go-sdk"
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

	// Create Crystil callback handler for LangChain
	handler := crystilClient.NewLangChainHandler()

	// Create LangChain LLM with Crystil callback
	llm, err := openai.New(
		openai.WithCallback(handler),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Use LangChain as normal - analytics tracked automatically via callbacks
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
	handler2 := newTxClient.NewLangChainHandler()

	llm2, err := openai.New(
		openai.WithModel("gpt-4o-mini"),
		openai.WithToken(os.Getenv("OPENAI_API_KEY")),
		openai.WithCallback(handler2),
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

	// All LLM calls are automatically tracked through the callback handler!
}
