package main

import (
	"context"
	"log"
	"time"

	"github.com/PayloopAI/go-sdk"
	"github.com/PayloopAI/go-sdk/api"
)

func main() {
	payloopClient, err := payloop.New()
	if err != nil {
		log.Fatal(err)
	}
	defer payloopClient.Close()

	ctx := context.Background()

	workflowClient := payloopClient.Workflow("[Workflow UUID]")
	summary, summaryErr := workflowClient.Invocation().Summary(ctx, api.SummaryOptions{
		DateStart: time.Date(2011, 2, 23, 0, 0, 0, 0, time.UTC),
	})
	if summaryErr != nil {
		log.Fatal(err)
	}

	log.Printf("summary: %+v", summary)
}
