"""
Friday Base - Voice (TTS / STT)
Indian English-only natural voice using Sarvam Bulbul V3 as primary, with edge-tts fallback.
STT lives in friday/llm.transcribe (Sarvam Saaras V3 -> local faster-whisper -> Groq).
Wake word: "Friday"
"""
import asyncio
import base64
import logging
import threading
from typing import Optional

import requests
from .config import config

logger = logging.getLogger("Friday.Voice")


def _run_coro_in_thread(coro_factory):
    """Run a coroutine in a dedicated worker thread that owns its own fresh
    event loop. Safe to call whether or not an event loop is already running
    in the current (calling) thread (e.g. inside a FastAPI async endpoint)."""
    result = {}

    def worker():
        loop = asyncio.new_event_loop()
        try:
            asyncio.set_event_loop(loop)
            result["value"] = loop.run_until_complete(coro_factory())
        except Exception as e:
            result["error"] = e
        finally:
            loop.close()

    t = threading.Thread(target=worker, daemon=True)
    t.start()
    t.join()
    if "error" in result:
        raise result["error"]
    return result.get("value")


def _sarvam_speak(text: str) -> Optional[bytes]:
    try:
        api_key = config.keys.get("sarvam", "")
        if not api_key:
            return None

        url = "https://api.sarvam.ai/text-to-speech"
        headers = {"api-subscription-key": api_key, "Content-Type": "application/json"}
        payload = {
            "model": config.sarvam_tts_model,
            "text": text,
            "target_language_code": "en-IN",
        }

        resp = requests.post(url, headers=headers, json=payload, timeout=15)
        resp.raise_for_status()
        data = resp.json()
        b64_audio = data.get("audio_content") or data.get("audio")
        if b64_audio:
            logger.info("Sarvam TTS (%s) OK: lang=en-IN", config.sarvam_tts_model)
            return base64.b64decode(b64_audio)
    except Exception as e:
        logger.warning("Sarvam TTS failed (%s): %s", config.sarvam_tts_model, e)
    return None


def _naturalize(text: str) -> str:
    """Shape text for a WARM, human, conversational voice (not a flat
    news-reader). Adds natural pauses and softens robotic bits.
    Never changes words."""
    import re as _re
    # Pause after sentence enders (a real breath, not a click).
    t = _re.sub(r"([.!?])\s+", r"\1 <break time='420ms'/> ", text)
    # Lighter pause after commas / colons / semicolons.
    t = _re.sub(r"(,|;|:)\s+", r"\1 <break time='220ms'/> ", t)
    # Soften ALL-CAPS words (TTS screams them).
    t = _re.sub(r"\b([A-Z]{3,})\b", lambda m: m.group(1).title(), t)
    return t.strip()


def _edge_speak(text: str, natural: bool = True) -> Optional[bytes]:
    try:
        import edge_tts

        voice = getattr(config, "tts_voice_edge", config.tts_voice_en) or config.tts_voice_en
        # Warm, lively prosody so it sounds like a person talking, not
        # reading text. Slightly faster-than-flat + a touch of pitch.
        rate = "+6%" if natural else "+0%"
        pitch = getattr(config, "tts_pitch_edge", "+2Hz")

        async def _do():
            speak_text = _naturalize(text) if natural else text
            comm = edge_tts.Communicate(
                speak_text, voice,
                rate=rate, pitch=pitch,
            )
            data = b""
            async for chunk in comm.stream():
                if chunk["type"] == "audio":
                    data += chunk["data"]
            return data

        out = _run_coro_in_thread(lambda: _do())
        return out if out else None
    except Exception as e:
        logger.warning(f"edge_tts failed ({config.tts_voice_en}): {e}")
        return None


# Tracks which engine actually produced the last spoken reply, so /status and
# the UI can report it (and we can prove we never served the robotic gTTS path).
_last_engine = {"name": None}


def last_used_engine() -> Optional[str]:
    return _last_engine["name"]


def _set_engine(name: str, audio: bytes) -> bytes:
    _last_engine["name"] = name
    return audio


def synthesize(text: str, natural: bool = True) -> Optional[bytes]:
    """Synthesize natural Indian English speech.

    Hardened chain (API/quality safe):
      1. Sarvam Bulbul V3  (if key present AND returns valid audio)
      2. edge-tts en-IN-NeerjaNeural  (always natural prosody)
      3. gTTS ONLY if explicitly enabled via ALLOW_GTTS_VOICE (otherwise silence)

    `natural=True` enables SSML-ish pauses + a warmer voice profile.
    We NEVER fall through to a robotic voice silently: if both primary engines
    fail and gTTS is off, we return None (the caller stays silent) rather than
    producing the "text-reading" sound you reported.
    """
    if not text or not text.strip():
        return None

    # 1) Sarvam Bulbul (paid) — ONLY when explicitly enabled. Default OFF so
    #    the free edge-tts path is used and no paid API call is made.
    if getattr(config, "tts_paid_enabled", False) and config.has_key("sarvam"):
        audio = _sarvam_speak(text)
        if audio and len(audio) > 1000:
            return _set_engine("sarvam", audio)
        logger.warning("Sarvam returned no usable audio; falling back to edge-tts.")

    # 2) edge-tts — guaranteed natural Indian English voice.
    audio = _edge_speak(text, natural=natural)
    if audio:
        return _set_engine("edge-tts", audio)

    # 3) gTTS is opt-in only; otherwise stay silent (no robotic audio).
    if getattr(config, "allow_gtts_voice", False):
        audio = _gtts(text)
        if audio:
            return _set_engine("gtts", audio)

    logger.error("All TTS engines failed; replying silently to avoid robotic audio.")
    return None


def _gtts(text: str) -> Optional[bytes]:
    try:
        from gtts import gTTS
        import io
        buf = io.BytesIO()
        gTTS(text=text[:500], lang="en").write_to_fp(buf)
        return buf.getvalue()
    except Exception as e:
        logger.warning(f"gtts failed: {e}")
        return None


def audio_to_base64(audio: bytes) -> str:
    return base64.b64encode(audio).decode("utf-8")


def base64_to_audio(b64: str) -> bytes:
    return base64.b64decode(b64)


def detect_voice_activity(audio_bytes: bytes, threshold: float = 500.0) -> bool:
    """Simple energy-based voice activity detection.
    Returns True if voice is likely present in the audio data.
    """
    try:
        import math
        import struct
        if len(audio_bytes) < 48:
            return True
        data_start = 44
        samples = struct.unpack_from(
            '<' + 'h' * ((len(audio_bytes) - data_start) // 2),
            audio_bytes, data_start
        )
        if not samples:
            return True
        rms = math.sqrt(sum(s * s for s in samples) / len(samples))
        return rms > threshold
    except Exception:
        return True
