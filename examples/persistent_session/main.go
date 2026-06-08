// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package main demonstrates a multi-turn chat agent with in-memory session persistence.
// See docs/tutorials/01-getting-started/03-persistent-session.md for the tutorial.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()

	model, err := gemini.NewModel(ctx, "gemini-3.1-flash-lite", &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        "chat_assistant",
		Model:       model,
		Description: "A helpful chat assistant.",
		Instruction: "You are a helpful assistant. Remember the conversation context.",
		Tools:       []tool.Tool{geminitool.GoogleSearch{}},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	sessSvc := session.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:        "tutorial",
		Agent:          a,
		SessionService: sessSvc,
	})
	if err != nil {
		log.Fatalf("Failed to create runner: %v", err)
	}

	userID := "u1"
	sessionID := "s1"

	fmt.Println("Chat started. Type 'quit' to exit.")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := scanner.Text()
		if input == "quit" {
			break
		}
		msg := genai.NewContentFromText(input, genai.RoleUser)
		for event, err := range r.Run(ctx, userID, sessionID, msg, agent.RunConfig{}) {
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				break
			}
			if event.LLMResponse.Content == nil {
				continue
			}
			if event.LLMResponse.Partial {
				continue
			}
			for _, p := range event.LLMResponse.Content.Parts {
				fmt.Print(p.Text)
			}
		}
		fmt.Println()
	}
}
