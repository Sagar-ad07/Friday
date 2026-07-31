package friday

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// VITSTTS - Open-source Text-to-Speech
type VITSTTS struct {
	model   string
	voice   string
	emotion string
}

// NewVITSTTS creates a new VITS TTS
func NewVITSTTS() *VITSTTS {
	return &VITSTTS{
		model:   "vits-medium",
		voice:   "default",
		emotion: "neutral",
	}
}

// speechOutFile returns the wav path inside the OS temp dir
func speechOutFile() string {
	return filepath.Join(os.TempDir(), "friday_speech.wav")
}

// GenerateSpeech generates speech from text
func (tts *VITSTTS) GenerateSpeech(text string, confidence float64) []byte {
	out := speechOutFile()
	cmd := exec.Command("tts", "--text", text, "--model", tts.model, "--voice", tts.voice, "--emotion", tts.emotion, "--output_format", "wav", "--output_file", out)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[TTS] Failed to generate speech: %v", err)
		log.Printf("[TTS] Output: %s", string(output))
		return []byte{}
	}

	data, err := os.ReadFile(out)
	if err != nil {
		log.Printf("[TTS] Failed to read generated audio: %v", err)
		return []byte{}
	}

	return data
}

// StreamSpeech streams speech in chunks
func (tts *VITSTTS) StreamSpeech(text string) (<-chan []byte, error) {
	ch := make(chan []byte, 100)

	go func() {
		defer close(ch)

		out := speechOutFile()
		cmd := exec.Command("tts", "--text", text, "--model", tts.model, "--voice", tts.voice, "--emotion", tts.emotion, "--output_format", "wav", "--output_file", out)
		if _, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[TTS] Failed to generate speech: %v", err)
			return
		}

		data, err := os.ReadFile(out)
		if err != nil {
			return
		}

		ch <- data
	}()

	return ch, nil
}

// SetVoice sets the voice for TTS
func (tts *VITSTTS) SetVoice(voice string) {
	tts.voice = voice
}

// SetEmotion sets the emotion for TTS
func (tts *VITSTTS) SetEmotion(emotion string) {
	tts.emotion = emotion
}
