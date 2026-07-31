package friday

import (
	"context"
	"time"
)

// Transcriber interface
type Transcriber interface {
	Transcribe(audio []byte) (string, float64)
}

// TTSInterface interface
type TTSInterface interface {
	GenerateSpeech(text string, confidence float64) []byte
}

// LLMInterface interface
type LLMInterface interface {
	Chat(ctx context.Context, messages []Message, role Role) (*ChatCompletionResponse, error)
}

// VoiceResponse represents a voice response
type VoiceResponse struct {
	Text           string        `json:"text"`
	Confidence     float64       `json:"confidence"`
	ProcessingTime time.Duration `json:"processing_time"`
	Audio          []byte        `json:"audio,omitempty"`
}
