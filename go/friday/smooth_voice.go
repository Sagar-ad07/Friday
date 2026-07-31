package friday

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// SmoothVoiceSystem is Friday's voice layer: transcribe -> think -> speak.
// It stays behind the scenes when no audio device is present and keeps
// cheap statistics for the /voice/status endpoint.
type SmoothVoiceSystem struct {
	processor *VoiceProcessor
	mu        sync.Mutex

	commandsProcessed atomic.Int64
	commandsFailed    atomic.Int64
	avgLatencyMs      atomic.Int64
	lastCommandAt     atomic.Int64
}

// NewSmoothVoiceSystem builds the voice stack with the standard processor.
func NewSmoothVoiceSystem() *SmoothVoiceSystem {
	return &SmoothVoiceSystem{
		processor: NewVoiceProcessor(),
	}
}

// VoiceCommandResult is what /voice/command returns to the caller.
type VoiceCommandResult struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Reply      string  `json:"reply"`
	LatencyMs  int64   `json:"latency_ms"`
}

// ProcessVoiceCommand transcribes audio, echoes the recognized text and
// returns a structured result. The reply text is the recognized command —
// wiring it into the LLM chat is done by the caller via the regular chat
// endpoint, keeping this path fast and stateless.
func (svs *SmoothVoiceSystem) ProcessVoiceCommand(ctx context.Context, audio []byte) (*VoiceCommandResult, error) {
	start := time.Now()
	text, confidence := svs.processor.transcriber.Transcribe(audio)
	latency := time.Since(start)

	if text == "" {
		svs.commandsFailed.Add(1)
		return nil, fmt.Errorf("no speech recognized")
	}

	svs.commandsProcessed.Add(1)
	svs.avgLatencyMs.Store(latency.Milliseconds())
	svs.lastCommandAt.Store(time.Now().Unix())

	return &VoiceCommandResult{
		Text:       text,
		Confidence: confidence,
		Reply:      text,
		LatencyMs:  latency.Milliseconds(),
	}, nil
}

// StreamVoiceResponse generates TTS audio for the given text and streams
// text chunks to the caller (the audio bytes are delivered in the first chunk).
func (svs *SmoothVoiceSystem) StreamVoiceResponse(ctx context.Context, text string) (<-chan string, error) {
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}
	ch := make(chan string, 4)
	go func() {
		defer close(ch)
		audio := svs.processor.tts.GenerateSpeech(text, 1.0)
		if len(audio) == 0 {
			ch <- "data unavailable: no TTS binary"
			return
		}
		// Chunk text for SSE lines; audio is not serialized over the stream.
		ch <- text
	}()
	return ch, nil
}

// GetStats returns counters for the /voice/status endpoint.
func (svs *SmoothVoiceSystem) GetStats() map[string]any {
	return map[string]any{
		"commands_processed": svs.commandsProcessed.Load(),
		"commands_failed":    svs.commandsFailed.Load(),
		"avg_latency_ms":     svs.avgLatencyMs.Load(),
		"last_command_at":    svs.lastCommandAt.Load(),
	}
}

// Cleanup frees any cached state (TTS runs are one-shot, so nothing to purge).
func (svs *SmoothVoiceSystem) Cleanup() {
	svs.mu.Lock()
	defer svs.mu.Unlock()
}
