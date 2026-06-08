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

// Package main demonstrates how to wire the adkrest.Server's SpanProcessor
// and LogProcessor into OpenTelemetry so the /debug/trace/* endpoints
// can return real data. See docs/tutorials/06-observability/02-debug-endpoint.md
// for the full tutorial.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/server/adkrest"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/geminitool"
)

func main() {
	ctx := context.Background()

	// 1. Construct adkrest.Server and pull its SpanProcessor / LogProcessor.
	restServer, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:    agent.NewSingleLoader(mustBuildAgent(ctx)),
		SessionService: session.InMemoryService(),
	})
	if err != nil {
		log.Fatalf("Failed to create REST API server: %v", err)
	}

	// 2. Wire OTel SDK: hook Server's Processor to TracerProvider / LoggerProvider.
	//    Using OTLP HTTP exporters as the default; you can swap to stdouttrace/stdoutlog
	//    for local debugging by `go get go.opentelemetry.io/otel/exporters/stdout/...`.
	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		log.Fatalf("trace exporter: %v", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(restServer.SpanProcessor()),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(traceExporter)),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(ctx)

	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		log.Fatalf("log exporter: %v", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(restServer.LogProcessor()),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)
	logglobal.SetLoggerProvider(lp)
	defer lp.Shutdown(ctx)

	// 3. Mount *Server to net/http.
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", restServer))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	log.Println("Listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func mustBuildAgent(ctx context.Context) agent.Agent {
	model, err := gemini.NewModel(ctx, "gemini-3.1-flash-lite", &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("gemini model: %v", err)
	}
	a, err := llmagent.New(llmagent.Config{
		Name:        "weather_time_agent",
		Model:       model,
		Description: "Answers questions about time and weather.",
		Instruction: "I can answer your questions about the time and weather in a city.",
		Tools:       []tool.Tool{geminitool.GoogleSearch{}},
	})
	if err != nil {
		log.Fatalf("llmagent: %v", err)
	}
	return a
}

// Silence unused-import warning for time.
var _ = time.Second
