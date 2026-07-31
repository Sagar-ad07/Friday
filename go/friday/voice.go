package friday

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	voiceMu      sync.Mutex
	voiceEnabled bool
)

func init() {
	voiceEnabled = getEnvBool("ENABLE_VOICE_AGENT", false)
}

// speakScript generates + plays natural speech via edge-tts.
// Uses en-US-AriaNeural — the most human-like conversational voice.
const speakScript = `import asyncio, edge_tts, subprocess, tempfile, os, sys
async def speak():
    text = sys.argv[1]
    voice = sys.argv[2] if len(sys.argv) > 2 else "en-US-AriaNeural"
    rate = sys.argv[3] if len(sys.argv) > 3 else "+0%"
    pitch = sys.argv[4] if len(sys.argv) > 4 else "-1Hz"
    tmp = tempfile.mktemp(suffix=".mp3")
    comm = edge_tts.Communicate(text, voice, rate=rate, pitch=pitch)
    await comm.save(tmp)
    subprocess.run(["ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", tmp])
    os.unlink(tmp)
asyncio.run(speak())`

// Speak converts text to speech and plays it with a natural human voice.
func Speak(text string) {
	if !voiceEnabled || text == "" {
		return
	}

	go func() {
		defer func() { _ = recover() }()

		voiceMu.Lock()
		defer voiceMu.Unlock()

		voice := getEnv("TTS_VOICE_EDGE", "en-US-AriaNeural")
		rate := getEnv("TTS_RATE", "+0%")
		pitch := getEnv("TTS_PITCH_EDGE", "-1Hz")

		scriptPath := filepath.Join(os.TempDir(), "friday_speak.py")
		os.WriteFile(scriptPath, []byte(speakScript), 0644)

		cmd := exec.Command("python", scriptPath, text, voice, rate, pitch)
		err := cmd.Run()
		if err == nil {
			return
		}

		// Fallback: Windows SAPI
		sapiCmd := exec.Command("powershell", "-Command",
			"Add-Type -AssemblyName System.Speech; "+
				"$s = New-Object System.Speech.Synthesis.SpeechSynthesizer; "+
				"$s.Rate = 1; "+
				"$s.Speak('"+text+"')")
		sapiCmd.Run()
	}()
}

// SpeakAlert speaks critical/warning alerts aloud.
func SpeakAlert(severity, title, body string) {
	switch severity {
	case "critical":
		Speak(title + ". " + body)
	case "warning":
		Speak(title)
	case "success":
		Speak(title)
	}
}