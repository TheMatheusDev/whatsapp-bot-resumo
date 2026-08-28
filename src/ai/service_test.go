package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type dummyLogger struct{}

func (d *dummyLogger) Debug(msg string, fields ...interface{}) {}
func (d *dummyLogger) Info(msg string, fields ...interface{})  {}
func (d *dummyLogger) Warn(msg string, fields ...interface{})  {}
func (d *dummyLogger) Error(msg string, fields ...interface{}) {}
func (d *dummyLogger) Fatal(msg string, fields ...interface{}) {}

func TestTranscribeWithInteractions_OutputText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-api-key" {
			t.Errorf("expected x-goog-api-key 'test-api-key', got %q", r.Header.Get("x-goog-api-key"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", r.Header.Get("Content-Type"))
		}

		var req interactionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if req.Model != "gemini-3.5-transcribe" {
			t.Errorf("expected model 'gemini-3.5-transcribe', got %q", req.Model)
		}
		if len(req.Input) != 1 || req.Input[0].Type != "audio" {
			t.Errorf("unexpected input payload: %+v", req.Input)
		}
		if req.GenerationConfig == nil || req.GenerationConfig.TranscriptionConfig == nil || req.GenerationConfig.TranscriptionConfig.Mode != "smart" {
			t.Errorf("expected smart transcription config, got %+v", req.GenerationConfig)
		}

		resp := interactionResponse{
			ID:         "interactions/test123",
			Status:     "completed",
			OutputText: "Olá, este é um áudio transcrito.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	svc := &Service{
		apiKey:          "test-api-key",
		modelTranscribe: "gemini-3.5-transcribe",
		logger:          &dummyLogger{},
		httpClient:      ts.Client(),
	}

	targetParsed, _ := url.Parse(ts.URL)
	svc.httpClient.Transport = &rewriteTransport{targetURL: targetParsed}

	text, err := svc.transcribeWithInteractions(context.Background(), []byte("fake-audio-bytes"), "audio/ogg", "gemini-3.5-transcribe")
	if err != nil {
		t.Fatalf("transcribeWithInteractions failed: %v", err)
	}

	if text != "Olá, este é um áudio transcrito." {
		t.Errorf("expected %q, got %q", "Olá, este é um áudio transcrito.", text)
	}
}

func TestTranscribeWithInteractions_StepsFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := interactionResponse{
			ID:     "interactions/test456",
			Status: "completed",
			Steps: []interactionStep{
				{
					ID:   "step_001",
					Type: "model_output",
					Content: []interactionContentPart{
						{Type: "text", Text: "Transcrição via steps do interaction."},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	svc := &Service{
		apiKey:          "test-api-key",
		modelTranscribe: "gemini-3.5-transcribe",
		logger:          &dummyLogger{},
		httpClient:      ts.Client(),
	}
	targetParsed, _ := url.Parse(ts.URL)
	svc.httpClient.Transport = &rewriteTransport{targetURL: targetParsed}

	text, err := svc.transcribeWithInteractions(context.Background(), []byte("fake-audio-bytes"), "audio/ogg", "gemini-3.5-transcribe")
	if err != nil {
		t.Fatalf("transcribeWithInteractions failed: %v", err)
	}

	if text != "Transcrição via steps do interaction." {
		t.Errorf("expected %q, got %q", "Transcrição via steps do interaction.", text)
	}
}

func TestTranscribeWithInteractions_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer ts.Close()

	svc := &Service{
		apiKey:          "test-api-key",
		modelTranscribe: "gemini-3.5-transcribe",
		logger:          &dummyLogger{},
		httpClient:      ts.Client(),
	}
	targetParsed, _ := url.Parse(ts.URL)
	svc.httpClient.Transport = &rewriteTransport{targetURL: targetParsed}

	_, err := svc.transcribeWithInteractions(context.Background(), []byte("fake-audio-bytes"), "audio/ogg", "gemini-3.5-transcribe")
	if err == nil {
		t.Fatalf("expected error on 500 status, got nil")
	}
}

type rewriteTransport struct {
	targetURL *url.URL
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = rt.targetURL.Scheme
	req.URL.Host = rt.targetURL.Host
	return http.DefaultTransport.RoundTrip(req)
}
