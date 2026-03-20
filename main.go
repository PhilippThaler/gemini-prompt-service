package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/genai"
)

type Request struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
}

type Response struct {
	Timestamp time.Time `json:"timestamp"`
	Text      string    `json:"text"`
}

func run() error {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY not set in environment")
	}
	defaultPort := os.Getenv("DEFAULT_PORT")
	if defaultPort == "" {
		defaultPort = "8090"
	}

	ctx := context.Background()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		slog.Error("couldn't create genai client", "error", err)
	}

	mux := newServer(*client)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", defaultPort),
		Handler: mux,
	}

	go func() {
		slog.Info(fmt.Sprintf("Server started at http://localhost:%s", defaultPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	slog.Info("Server exiting")
	return nil
}

func generateGeminiResponse(ctx context.Context, client *genai.Client, model, prompt string) (Response, error) {
	result, err := client.Models.GenerateContent(ctx, model, genai.Text(prompt), nil)
	if err != nil {
		return Response{}, err
	}

	var responseText strings.Builder
	for _, candidate := range result.Candidates {
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				responseText.WriteString(part.Text)
			}
		}
	}
	return Response{
		Timestamp: time.Now(),
		Text:      responseText.String(),
	}, nil
}

func newServer(client genai.Client) http.Handler {
	mux := http.NewServeMux()

	model := os.Getenv("GEMINI_DEFAULT_MODEL")
	prompt := os.Getenv("GEMINI_DEFAULT_PROMPT")

	if model == "" {
		model = "gemini-2.5-flash"
	}
	if prompt == "" {
		seed := time.Now().Format(time.RFC3339Nano)
		prompt = fmt.Sprintf("Write a unique, short poem (under 500 chars). Use the seed '%s' to ensure a completely original theme and structure every time.", seed)
	}

	mux.HandleFunc("GET /new", func(w http.ResponseWriter, r *http.Request) {
		resp, err := generateGeminiResponse(r.Context(), &client, model, prompt)
		if err != nil {
			slog.Error("Couldn't generate Gemini Response", "error", err, "prompt", prompt)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("Json encoding failed", "error", err)
		}
	})

	mux.HandleFunc("POST /new", func(w http.ResponseWriter, r *http.Request) {
		var req Request

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Error("Failed to decode request", "error", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Prompt == "" {
			http.Error(w, "Prompt is required", http.StatusBadRequest)
			return
		}

		activeModel := model
		if req.Model != "" {
			activeModel = req.Model
		}

		resp, err := generateGeminiResponse(r.Context(), &client, activeModel, req.Prompt)
		if err != nil {
			slog.Error("Couldn't generate Gemini Response", "error", err, "prompt", req.Prompt)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("Json encoding failed", "error", err)
		}
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	return mux
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
