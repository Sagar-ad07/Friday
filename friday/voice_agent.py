"""
Friday Base - Voice Agent
Always-listening voice assistant with wake word detection, sleep/wake cycle,
and continuous background listening.

States:
  SLEEPING  - low-power listening, only checking for wake word
  LISTENING - captured wake word, recording full command
  PROCESSING - running LLM/tools, generating response
  SPEAKING  - playing audio response

Usage:
  agent = VoiceAgent()
  agent.start()
"""

from __future__ import annotations

import logging
import os
import queue
import signal
import struct
import threading
import time
from enum import Enum, auto
from typing import Optional

from . import llm, orchestrator, voice
from .config import config

logger = logging.getLogger("Friday.VoiceAgent")


class State(Enum):
    SLEEPING = auto()
    LISTENING = auto()
    PROCESSING = auto()
    SPEAKING = auto()


class VoiceAgent:
    """Thread-safe always-listening voice assistant."""

    def __init__(self):
        self._state = State.SLEEPING
        self._state_lock = threading.Lock()
        self._shutdown = threading.Event()
        self._last_speech_time = time.time()
        self._silence_timeout = self._get_silence_timeout()
        self._wake_word = (config.wake_word or "friday").lower()
        self._sample_rate = 16000
        self._chunk_size = 1024
        self._channels = 1
        self._format_bytes = 2  # 16-bit PCM
        self._silence_threshold = 500.0
        self._wake_rms_threshold = 400.0
        self._listen_seconds = 8
        self._background_tasks: list[dict] = []
        self._bg_lock = threading.Lock()
        self._notifications: list[str] = []
        self._notif_lock = threading.Lock()
        self._audio_stream = None
        self._pyaudio = None
        self._pyaudio_stream = None
        self._pyaudio_player = None

        self._try_import_pyaudio()

    def _try_import_pyaudio(self):
        try:
            import pyaudio  # noqa: F401
            self._pyaudio_available = True
        except ImportError:
            self._pyaudio_available = False
            logger.warning(
                "PyAudio not installed. Install with: pip install pyaudio. "
                "Voice agent will run in manual mode."
            )

    def _get_silence_timeout(self) -> int:
        try:
            return int(os.getenv("VOICE_SILENCE_TIMEOUT", "45"))
        except Exception:
            return 45

    @property
    def state(self) -> State:
        with self._state_lock:
            return self._state

    def _set_state(self, new_state: State) -> None:
        with self._state_lock:
            old_state = self._state
            self._state = new_state
        logger.debug("State transition: %s -> %s", old_state.name, new_state.name)

    def start(self) -> None:
        """Start the voice agent main loop. Blocks until shutdown."""
        self._register_signals()
        logger.info("Voice agent starting (wake word: %r, silence timeout: %ds)",
                    self._wake_word, self._silence_timeout)

        if not self._pyaudio_available:
            self._run_manual_mode()
            return

        self._init_audio()
        bg_thread = threading.Thread(target=self._background_loop, daemon=True, name="bg-loop")
        bg_thread.start()

        try:
            self._main_loop()
        finally:
            self._cleanup()

    def stop(self) -> None:
        """Request graceful shutdown."""
        self._shutdown.set()
        logger.info("Shutdown requested.")

    def _register_signals(self) -> None:
        def handler(signum, _frame):
            logger.info("Received signal %s, shutting down.", signum)
            self.stop()

        for sig in (signal.SIGINT, signal.SIGTERM):
            try:
                signal.signal(sig, handler)
            except (ValueError, OSError):
                pass

    def _init_audio(self) -> None:
        try:
            import pyaudio
            self._pyaudio = pyaudio.PyAudio()
            self._audio_stream = self._pyaudio.open(
                format=self._pyaudio.get_format_from_width(self._format_bytes),
                channels=self._channels,
                rate=self._sample_rate,
                input=True,
                frames_per_buffer=self._chunk_size,
                stream_callback=None,
            )
            self._audio_stream.start_stream()
            logger.info("Audio stream opened (%d Hz, %d ch, %d-bit)",
                        self._sample_rate, self._channels, self._format_bytes * 8)
        except Exception as e:
            logger.error("Failed to open audio stream: %s", e)
            self._pyaudio_available = False
            raise RuntimeError("Cannot initialise microphone.") from e

    def _cleanup(self) -> None:
        try:
            if self._audio_stream:
                self._audio_stream.stop_stream()
                self._audio_stream.close()
        except Exception:
            pass
        try:
            if self._pyaudio:
                self._pyaudio.terminate()
        except Exception:
            pass
        logger.info("Audio resources cleaned up.")

    def _main_loop(self) -> None:
        """Primary listening loop."""
        while not self._shutdown.is_set():
            current = self.state
            if current == State.SLEEPING:
                self._sleep_loop()
            elif current == State.LISTENING:
                self._listen_loop()
            elif current == State.PROCESSING:
                self._processing_loop()
            elif current == State.SPEAKING:
                self._speaking_loop()

    def _sleep_loop(self) -> None:
        """Low-power wake-word detection loop with barge-in support."""
        logger.debug("Entering SLEEPING loop.")
        while not self._shutdown.is_set() and self.state == State.SLEEPING:
            try:
                audio_chunk = self._read_chunk()
                text = self._transcribe_chunk(audio_chunk)
                if text and self._is_wake_word(text):
                    logger.info("Wake word detected: %r", text.strip())
                    self._set_state(State.LISTENING)
                    self._last_speech_time = time.time()
                    self.speak("Yes?")
                    return
                # Barge-in: if user says something substantial while we're sleeping,
                # treat it as if they called the wake word.
                if text and len(text.split()) >= 2:
                    logger.info("Barge-in detected: %r", text.strip())
                    self._set_state(State.LISTENING)
                    self._last_speech_time = time.time()
                    return
            except Exception as e:
                logger.debug("Sleep loop error: %s", e)
                time.sleep(0.05)

    def _listen_loop(self) -> None:
        """Capture full command after wake word with faster timeout."""
        logger.debug("Entering LISTENING loop (up to %ds).", self._listen_seconds)
        frames: list[bytes] = []
        silence_start: Optional[float] = None
        listen_deadline = time.time() + self._listen_seconds

        while not self._shutdown.is_set() and self.state == State.LISTENING:
            if time.time() > listen_deadline:
                logger.debug("Listening deadline reached.")
                break

            try:
                chunk = self._read_chunk()
                frames.append(chunk)

                rms = self._compute_rms(chunk)
                if rms < self._silence_threshold:
                    if silence_start is None:
                        silence_start = time.time()
                    elif time.time() - silence_start > 1.0:  # Faster silence detection
                        logger.debug("Silence detected after speech, ending capture.")
                        break
                else:
                    silence_start = None
                    self._last_speech_time = time.time()
            except Exception as e:
                logger.debug("Listen loop error: %s", e)
                time.sleep(0.05)

        if not frames:
            self._set_state(State.SLEEPING)
            return

        audio_data = b"".join(frames)
        if self._compute_rms(audio_data) > self._silence_threshold * 0.5:
            self._set_state(State.PROCESSING)
            self._process_and_respond(audio_data)
        else:
            logger.debug("No speech captured, returning to sleep.")
            self._set_state(State.SLEEPING)

    def _processing_loop(self) -> None:
        """Wait for background processing."""
        time.sleep(0.05)

    def _speaking_loop(self) -> None:
        """Monitor for barge-in while speaking. If user speaks, immediately
        stop and switch to LISTENING so conversation feels natural."""
        # Barge-in: listen while speaking and cut off if user talks.
        while not self._shutdown.is_set() and self.state == State.SPEAKING:
            try:
                chunk = self._read_chunk()
                rms = self._compute_rms(chunk)
                if rms > self._silence_threshold:
                    logger.info("Barge-in detected, cutting off speech.")
                    self._stop_audio()
                    self._set_state(State.LISTENING)
                    self._last_speech_time = time.time()
                    return
            except Exception:
                pass
            time.sleep(0.05)

    def _process_and_respond(self, audio_bytes: bytes) -> None:
        """Transcribe, run brain, speak response."""
        try:
            text = self._transcribe_audio(audio_bytes)
        except Exception as e:
            logger.error("Transcription failed: %s", e)
            text = ""

        if not text.strip():
            self._set_state(State.SPEAKING)
            self.speak("I didn't catch that.")
            self._last_speech_time = time.time()
            self._set_state(State.SLEEPING)
            return

        logger.info("Transcribed: %r", text)

        # Check if this is a research request
        research_topic = self._extract_research_topic(text)
        if research_topic:
            self._handle_research_request(research_topic)
            self._last_speech_time = time.time()
            self._set_state(State.SLEEPING)
            return

        try:
            result = orchestrator.handle_turn(text, lang="en")
            reply = result.get("reply") or "I'm not sure how to help with that."
        except Exception as e:
            logger.error("handle_turn failed: %s", e)
            reply = "Sorry, I encountered an error processing your request."

        self._set_state(State.SPEAKING)
        self.speak(reply)
        self._last_speech_time = time.time()
        self._set_state(State.SLEEPING)

    def _extract_research_topic(self, text: str) -> Optional[str]:
        """Extract research topic from voice command."""
        text_lower = text.lower()
        research_keywords = [
            "research", "search for", "find out about", "investigate",
            "look up", "dig into", "find information about", "report on",
            "tell me everything about", "what do you know about",
            "search", "dig", "find", "investigate"
        ]

        for keyword in research_keywords:
            if keyword in text_lower:
                idx = text_lower.find(keyword)
                topic = text[idx + len(keyword):].strip()
                for filler in ["about", "on", "for", "the", "a", "an"]:
                    if topic.lower().startswith(filler + " "):
                        topic = topic[len(filler) + 1:].strip()
                if topic and len(topic) > 2:
                    return topic
        return None

    def _handle_research_request(self, topic: str) -> None:
        """Start background research and acknowledge immediately."""
        self._set_state(State.SPEAKING)
        self.speak(f"Starting research on {topic}. I'll notify you when it's done.")

        def do_research():
            try:
                result = orchestrator.start_background_research(topic, depth="deep")
                logger.info("Research completed: %s", result)
            except Exception as e:
                logger.error("Background research failed: %s", e)

        threading.Thread(target=do_research, daemon=True).start()

    def _run_manual_mode(self) -> None:
        """Fallback when PyAudio is unavailable."""
        logger.info("Running in manual mode. Type 'exit' to quit.")
        print("Friday Voice Agent (manual mode)")
        print("  Type your command and press Enter. Ctrl+C to exit.")
        print()

        bg_thread = threading.Thread(target=self._background_loop, daemon=True, name="bg-loop")
        bg_thread.start()

        while not self._shutdown.is_set():
            try:
                text = input("You: ").strip()
            except (EOFError, KeyboardInterrupt):
                break

            if not text:
                continue
            if text.lower() in ("exit", "quit"):
                break

            if self.state == State.SLEEPING:
                if self._is_wake_word(text):
                    self._set_state(State.LISTENING)
                    text = text[len(self._wake_word):].strip()
                    if not text:
                        print("Friday: Yes?")
                        self._set_state(State.SLEEPING)
                        continue
                else:
                    continue

            self._set_state(State.PROCESSING)
            try:
                result = orchestrator.handle_turn(text, lang="en")
                reply = result.get("reply") or "Okay."
            except Exception as e:
                logger.error("handle_turn failed: %s", e)
                reply = "Error processing request."

            self._set_state(State.SPEAKING)
            print(f"Friday: {reply}")
            self._last_speech_time = time.time()
            self._set_state(State.SLEEPING)

        self.stop()

    def speak(self, text: str) -> None:
        """Synthesize and play audio. Non-blocking for background playback."""
        if not text or not text.strip():
            return

        logger.info("Speaking: %r", text[:120])
        audio = voice.synthesize(text)
        if not audio:
            logger.warning("TTS returned no audio.")
            return

        self._notifications.append(f"SPOKE: {text[:80]}")
        self._play_audio(audio)

    def _play_audio(self, audio_bytes: bytes) -> None:
        if not audio_bytes:
            return

        if self._pyaudio_available:
            try:
                self._play_with_pyaudio(audio_bytes)
            except Exception as e:
                logger.warning("PyAudio playback failed: %s", e)
                return

        self._play_fallback(audio_bytes)

    def _stop_audio(self) -> None:
        """Stop any ongoing audio playback (for barge-in)."""
        if hasattr(self, '_pyaudio_stream') and self._pyaudio_stream:
            try:
                self._pyaudio_stream.stop_stream()
                self._pyaudio_stream.close()
            except Exception:
                pass
            self._pyaudio_stream = None
        if hasattr(self, '_pyaudio_player') and self._pyaudio_player:
            try:
                self._pyaudio_player.terminate()
            except Exception:
                pass
            self._pyaudio_player = None

    def _play_with_pyaudio(self, audio_bytes: bytes) -> None:
        import pyaudio
        import wave
        import io

        wav_buf = io.BytesIO()
        with wave.open(wav_buf, "wb") as wf:
            wf.setnchannels(self._channels)
            wf.setsampwidth(self._format_bytes)
            wf.setframerate(self._sample_rate)
            wf.writeframes(audio_bytes)
        wav_buf.seek(0)

        with wave.open(wav_buf, "rb") as wf:
            p = pyaudio.PyAudio()
            self._pyaudio_player = p
            stream = p.open(
                format=p.get_format_from_width(wf.getsampwidth()),
                channels=wf.getnchannels(),
                rate=wf.getframerate(),
                output=True,
            )
            self._pyaudio_stream = stream
            data = wf.readframes(1024)
            while data and self.state == State.SPEAKING:
                stream.write(data)
                data = wf.readframes(1024)
            try:
                stream.stop_stream()
                stream.close()
            except Exception:
                pass
            p.terminate()
            self._pyaudio_stream = None
            self._pyaudio_player = None

    def _play_fallback(self, audio_bytes: bytes) -> None:
        try:
            import platform
            import subprocess
            import tempfile

            suffix = ".mp3" if audio_bytes[:3] == b"ID3" else ".wav"
            with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as f:
                f.write(audio_bytes)
                path = f.name

            system = platform.system()
            if system == "Windows":
                subprocess.run(["powershell", "-c", f"(New-Object Media.SoundPlayer '{path}').PlaySync();"],
                               capture_output=True)
            elif system == "Darwin":
                subprocess.run(["afplay", path], capture_output=True)
            else:
                subprocess.run(["aplay", path], capture_output=True)

            os.unlink(path)
        except Exception as e:
            logger.warning("Fallback playback failed: %s", e)

    def _read_chunk(self) -> bytes:
        try:
            return self._audio_stream.read(self._chunk_size, exception_on_overflow=False)
        except Exception:
            return b"\x00" * self._chunk_size * self._format_bytes

    @staticmethod
    def _compute_rms(data: bytes) -> float:
        if len(data) < 48:
            return 0.0
        try:
            samples = struct.unpack_from(
                "<" + "h" * ((len(data) - 44) // 2),
                data, 44
            )
            if not samples:
                return 0.0
            return (sum(s * s for s in samples) / len(samples)) ** 0.5
        except Exception:
            return 0.0

    def _is_wake_word(self, text: str) -> bool:
        if not text:
            return False
        words = text.lower().split()
        return self._wake_word in words

    def _transcribe_chunk(self, audio_chunk: bytes) -> str:
        try:
            text, _, _ = llm.transcribe(audio_chunk, filename="chunk.webm")
            return text.strip()
        except Exception:
            return ""

    def _transcribe_audio(self, audio_bytes: bytes) -> str:
        try:
            text, _, _ = llm.transcribe(audio_bytes, filename="command.webm")
            return text.strip()
        except Exception as e:
            logger.error("Transcription error: %s", e)
            return ""

    def _background_loop(self) -> None:
        """Periodic checks for completed tasks and notifications."""
        logger.debug("Background loop started.")
        while not self._shutdown.is_set():
            self._shutdown.wait(timeout=5)
            if self._shutdown.is_set():
                break
            self._check_completed_tasks()
            self._check_notifications()

    def _check_completed_tasks(self) -> None:
        with self._bg_lock:
            pending = [t for t in self._background_tasks if not t.get("done")]
            for task in pending:
                future = task.get("future")
                if future and future.done():
                    try:
                        result = future.result()
                        task["result"] = result
                        task["done"] = True
                        logger.info("Background task completed: %s", task.get("name"))
                    except Exception as e:
                        task["error"] = str(e)
                        task["done"] = True
                        logger.warning("Background task failed: %s: %s", task.get("name"), e)

    def _check_notifications(self) -> None:
        if self.state == State.SLEEPING:
            return
        # Placeholder for proactive notification logic
        # e.g. checking calendar, email, reminders, etc.
        pass

    def submit_background(self, name: str, func, *args, **kwargs) -> None:
        """Submit a function to run in background."""
        future = threading.Thread(
            target=self._bg_worker,
            args=(name, func, args, kwargs),
            daemon=True,
            name=f"bg-{name}",
        )
        with self._bg_lock:
            self._background_tasks.append({
                "name": name,
                "future": future,
                "done": False,
            })
        future.start()

    @staticmethod
    def _bg_worker(name: str, func, args: tuple, kwargs: dict) -> None:
        try:
            func(*args, **kwargs)
        except Exception as e:
            logger.warning("Background worker %s raised: %s", name, e)

    def sleep_timer_check(self) -> None:
        """Check if silence timeout exceeded; transition to SLEEPING if so."""
        if self.state in (State.LISTENING, State.SPEAKING):
            self._last_speech_time = time.time()
            return

        if self.state != State.SLEEPING:
            return

        elapsed = time.time() - self._last_speech_time
        if elapsed >= self._silence_timeout:
            logger.debug("Silence timeout (%ds) reached, staying in SLEEPING.", int(elapsed))


def start_voice_agent() -> None:
    """Entry point: create and run the voice agent."""
    agent = VoiceAgent()
    agent.start()


if __name__ == "__main__":
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    start_voice_agent()
