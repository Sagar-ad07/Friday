package friday

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// WhisperTranscriber - Open-source speech-to-text
type WhisperTranscriber struct {
	modelPath string
	language  string
}

// NewWhisperTranscriber creates a new Whisper transcriber
func NewWhisperTranscriber() *WhisperTranscriber {
	return &WhisperTranscriber{
		modelPath: filepath.Join(ProjectRoot, "data", "whisper_models"),
		language:  "en", // Default to English
	}
}

// Transcribe speech to text
func (wt *WhisperTranscriber) Transcribe(audio []byte) (string, float64) {
	outDir := filepath.Join(os.TempDir(), "friday_whisper")
	_ = os.MkdirAll(outDir, 0755)

	// Write the incoming audio so the whisper binary can read it
	inFile := filepath.Join(outDir, "input.wav")
	if err := os.WriteFile(inFile, audio, 0644); err != nil {
		log.Printf("[WHISPER] Failed to stage audio: %v", err)
		return "", 0.0
	}

	cmd := exec.Command("whisper", "--model", "base", "--language", wt.language, "--output_format", "txt", "--output_dir", outDir, inFile)
	if _, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[WHISPER] Failed to transcribe: %v", err)
		return "", 0.0
	}

	txtFile := filepath.Join(outDir, "input.txt")
	data, err := os.ReadFile(txtFile)
	if err != nil {
		return "", 0.0
	}

	return string(data), 0.9
}

// ListModels returns available Whisper models
func (wt *WhisperTranscriber) ListModels() ([]string, error) {
	return []string{"tiny", "base", "small", "medium", "large"}, nil
}

// SetLanguage sets the language for transcription
func (wt *WhisperTranscriber) SetLanguage(lang string) {
	wt.language = lang
}
