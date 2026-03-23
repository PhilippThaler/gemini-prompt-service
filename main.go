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
	Prompt             string `json:"prompt,omitempty"`
	Model              string `json:"model,omitempty"`
	SystemInstructions string `json:"system_instructions,omitempty"`
}

type Response struct {
	Timestamp time.Time `json:"timestamp"`
	Text      string    `json:"text"`
}

type ModerationResponse struct {
	Timestamp  time.Time `json:"timestamp"`
	IsApproved bool      `json:"is_approved"`
	Reason     string    `json:"reason"`
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
		return fmt.Errorf("couldn't create genai client: %w", err)
	}

	mux := newServer(client)

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

func generateGeminiResponse(ctx context.Context, client *genai.Client, model, prompt string, config *genai.GenerateContentConfig) (Response, error) {
	result, err := client.Models.GenerateContent(ctx, model, genai.Text(prompt), config)
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

func generateGeminiModerationResponse(ctx context.Context, client *genai.Client, model, prompt string) (ModerationResponse, error) {
	const moderationPrompt = `You are an automated content moderation API. Your job is to analyze user-submitted text and determine if it violates community guidelines. A post violates guidelines if it contains: 1. Explicit hate speech, racism, or targeted harassment. 2. Severe threats of physical violence. 3. Obvious spam or blatant advertising (e.g., 'Buy cheap pills here: [link]'). You must ignore mild profanity, sarcasm, unpopular opinions, or weird fiction.`
	temp := float32(0.0)
	config := genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: moderationPrompt}}},
		Temperature:       &temp,
		ResponseMIMEType:  "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"is_approved": {Type: genai.TypeBoolean},
				"reason":      {Type: genai.TypeString},
			},
			Required: []string{"is_approved", "reason"},
		},
	}

	result, err := client.Models.GenerateContent(ctx, model, genai.Text(prompt), &config)
	if err != nil {
		return ModerationResponse{}, err
	}

	var responseText strings.Builder
	for _, candidate := range result.Candidates {
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				responseText.WriteString(part.Text)
			}
		}
	}

	rawJSON := responseText.String()

	var aiResult struct {
		IsApproved bool   `json:"is_approved"`
		Reason     string `json:"reason"`
	}

	if err := json.Unmarshal([]byte(rawJSON), &aiResult); err != nil {
		return ModerationResponse{}, fmt.Errorf("failed to parse AI response: %w. Raw output: %s", err, rawJSON)
	}
	return ModerationResponse{
		Timestamp:  time.Now(),
		IsApproved: aiResult.IsApproved,
		Reason:     aiResult.Reason,
	}, nil

}

func newServer(client *genai.Client) http.Handler {
	mux := http.NewServeMux()

	defaultModel := os.Getenv("GEMINI_DEFAULT_MODEL")
	if defaultModel == "" {
		defaultModel = "gemini-2.5-flash"
	}
	defaultPrompt := os.Getenv("GEMINI_DEFAULT_PROMPT")
	defaultInstructions := os.Getenv("GEMINI_DEFAULT_INSTRUCTIONS")

	mux.HandleFunc("GET /new", func(w http.ResponseWriter, r *http.Request) {
		activePrompt := defaultPrompt
		if activePrompt == "" {
			seed := time.Now().Format(time.RFC3339Nano)
			activePrompt = fmt.Sprintf("Write a unique, short poem (under 500 chars). Use the seed '%s' to ensure a completely original theme and structure every time.", seed)
		}

		var config *genai.GenerateContentConfig
		if defaultInstructions != "" {
			config = &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: defaultInstructions}}},
			}
		}

		resp, err := generateGeminiResponse(r.Context(), client, defaultModel, activePrompt, config)
		if err != nil {
			slog.Error("Couldn't generate Gemini Response", "error", err, "prompt", activePrompt)
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

		activeModel := defaultModel
		if req.Model != "" {
			activeModel = req.Model
		}

		activeInstructions := defaultInstructions
		if req.SystemInstructions != "" {
			activeInstructions = req.SystemInstructions
		}

		var config *genai.GenerateContentConfig
		if activeInstructions != "" {
			config = &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: activeInstructions}}},
			}
		}

		resp, err := generateGeminiResponse(r.Context(), client, activeModel, req.Prompt, config)
		if err != nil {
			slog.Error("Couldn't generate Gemini Response", "error", err, "prompt", req.Prompt, "instructions", req.SystemInstructions)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("Json encoding failed", "error", err)
		}
	})

	mux.HandleFunc("POST /moderate", func(w http.ResponseWriter, r *http.Request) {
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

		activeModel := defaultModel
		if req.Model != "" {
			activeModel = req.Model
		}

		resp, err := generateGeminiModerationResponse(r.Context(), client, activeModel, req.Prompt)
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
