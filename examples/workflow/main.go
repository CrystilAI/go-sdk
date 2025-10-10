package main

import (
	"context"
	"fmt"
	"log"

	"github.com/PayloopAI/go-sdk"
)

func main() {
	payloopClient, err := payloop.New()
	if err != nil {
		log.Fatal(err)
	}
	defer payloopClient.Close()

	ctx := context.Background()

	workflows, err := payloopClient.Workflows().List(ctx)
	if err != nil {
		log.Fatal(err)
	}

	for _, workflow := range workflows {
		fmt.Printf("%+v\n", workflow)
	}

	//workflowClient := payloopClient.Workflow("workflow-uuid")
	//err = workflowClient.Update(ctx, "New Label")
	//if err != nil {
	//	log.Fatal(err)
	//}
}
