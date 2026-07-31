"""
Friday Base - LLM layer (v2: chain-walking + retry + circuit breaker)
chat() walks the role's provider chain; each candidate retried; breaker isolates
failing providers. Local mode routes through Ollama (offline vision+text ready).
transcribe() = Sarvam Saaras V3 -> local faster-whisper -> Groq.
"""
import logging
import os
import time
from io import BytesIO
from typing import Dict, List, Tuple

from .config import config
from . import resilience

logger = logging.getLogger("Friday.LLM")
_client_cache = {}


def _get_client(provider):
    if provider in _client_cache:
        return _client_cache[provider]
    from openai import OpenAI
    if provider == "opencode":
        key = ""
    else:
        key = config.keys.get(provider, "")
    client = OpenAI(base_url=config.endpoint(provider), api_key=key)
    _client_cache[provider] = client
    return client


def chat(messages: List[Dict], role: str = "companion", temperature: float = 0.7,
         max_tokens: int = 1024, json_mode: bool = False) -> Tuple[str, str]:
    chain = config.live_chain(role)
    if not chain:
        raise RuntimeError(
            "No provider available for role '%s'. Check PROVIDER_MODE and API keys." % role)
    last_err = None
    # Local models are slower; give them more time and fewer retries to avoid
    # compounding a 60-120s call into a 3-4 minute stall.
    is_local = all(c.provider == "ollama" for c in chain) and config.provider_mode == "local"
    timeout = getattr(config, "llm_call_timeout", 45)
    # When the local Ollama model is in the chain (hybrid mode), give IT a generous
    # timeout so a warm local model actually answers tool-calls instead of tripping
    # the 45s cloud timeout and silently escalating. Cloud candidates keep 45s.
    effective_timeout = max(timeout, 180.0) if is_local else timeout
    max_attempts = 1 if is_local else config.max_retries
    for cand in chain:
        # Local Ollama gets the generous timeout; cloud providers stay at 45s so a
        # groq/deepseek stall fails over fast instead of hanging the turn.
        cand_timeout = effective_timeout if cand.provider == "ollama" else timeout
        if resilience.breaker.is_open(cand.provider):
            logger.warning("skip %s (breaker open)", cand.provider)
            continue
        for attempt in range(max_attempts):
            try:
                client = _get_client(cand.provider)
                kwargs = dict(model=cand.model, messages=messages,
                              temperature=temperature, max_tokens=max_tokens,
                              timeout=cand_timeout)
                # Keep the local model resident so a warm 9B never gets evicted
                # and force a 6.5GB re-load spike that OOMs Friday under 16GB RAM.
                if cand.provider == "ollama":
                    kwargs["keep_alive"] = "1h"
                if json_mode:
                    kwargs["response_format"] = {"type": "json_object"}
                _t0 = time.time()
                resp = client.chat.completions.create(**kwargs)
                _t1 = time.time()
                text = resp.choices[0].message.content or ""
                # Some models use a separate reasoning field instead of content.
                if not text.strip():
                    text = getattr(resp.choices[0].message, "reasoning_content", "") or ""
                if not text.strip():
                    text = getattr(resp.choices[0].message, "reasoning", "") or ""
                logger.info("[timing] llm %s/%s attempt=%d took=%.3fs provider=%s",
                            cand.provider, cand.model, attempt + 1, _t1 - _t0,
                            cand.provider)
                if text.strip():
                    resilience.breaker.record_success(cand.provider)
                    logger.info("LLM ok via %s/%s", cand.provider, cand.model)
                    return text, cand.provider
                # Empty content: treat as a soft failure and retry this candidate.
                last_err = RuntimeError("empty response from %s" % cand.provider)
                resilience.breaker.record_failure(cand.provider)
                logger.warning("LLM empty %s/%s try%d", cand.provider, cand.model, attempt + 1)
                time.sleep(0.3 * (attempt + 1))
                continue
            except Exception as e:
                last_err = e
                err_str = str(e).lower()
                # On timeout or rate limit, fail over immediately to the next candidate
                # so a slow local model never stalls the turn.
                if ("timeout" in err_str or "timed out" in err_str or
                        "429" in err_str or "rate limit" in err_str or
                        "too many requests" in err_str):
                    logger.warning("LLM failover %s/%s: %s", cand.provider, cand.model, e)
                    resilience.breaker.record_failure(cand.provider)
                    break
                resilience.breaker.record_failure(cand.provider)
                logger.warning("LLM fail %s/%s try%d: %s",
                               cand.provider, cand.model, attempt + 1, e)
                time.sleep(0.3 * (attempt + 1))
                continue
    # Local Ollama fallback: if the configured model is too slow or failed, try the fast
    # fallback model (e.g. qwen2.5:1.5b) so Friday still answers quickly.
    if (config.provider_mode == "local"
            and chain
            and all(c.provider == "ollama" for c in chain)
            and getattr(config, "ollama_model_fast", None)):
        fast_model = config.ollama_model_fast
        # Don't retry the same model we just tried.
        if fast_model == chain[0].model:
            raise RuntimeError("All providers failed for role '%s'. Last: %s" % (role, last_err))
        logger.info("Local fallback: trying fast model %s", fast_model)
        try:
            client = _get_client("ollama")
            kwargs = dict(model=fast_model, messages=messages,
                          temperature=temperature, max_tokens=max_tokens,
                          timeout=max(15, getattr(config, "llm_call_timeout", 45) // 2),
                          keep_alive="1h")
            if json_mode:
                kwargs["response_format"] = {"type": "json_object"}
            _t0 = time.time()
            resp = client.chat.completions.create(**kwargs)
            _t1 = time.time()
            text = resp.choices[0].message.content or ""
            if not text.strip():
                text = getattr(resp.choices[0].message, "reasoning", "") or ""
            logger.info("[timing] llm ollama/%s fallback took=%.3fs", fast_model, _t1 - _t0)
            if text.strip():
                logger.info("LLM ok via ollama/%s fallback", fast_model)
                return text, "ollama"
        except Exception as e:
            logger.warning("LLM fallback %s failed: %s", fast_model, e)
    raise RuntimeError("All providers failed for role '%s'. Last: %s" % (role, last_err))


def _correct_transcript(text: str, memory_context: str = "") -> str:
    # Transcript correction is an extra LLM call per voice turn. It is opt-in
    # (CORRECT_TRANSCRIPTS=true) so voice stays fast by default. When enabled we
    # make exactly ONE attempt on the first available provider and return.
    if not text:
        return text
    if not os.getenv("CORRECT_TRANSCRIPTS", "false").lower() == "true":
        return text

    providers = list(config.live_chain("companion")) or [config.role_chains.get("companion", [None])[0]]
    for cand in providers or []:
        if not cand:
            continue
        provider = cand.provider
        if not config.has_key(provider):
            continue
        try:
            context_section = ""
            if memory_context:
                context_section = (
                    f"\nUser memory context: {memory_context}\n"
                    "Use this context to infer likely intended words if a transcription "
                    "error is ambiguous.\n"
                )
            prompt = (
                f"Fix transcription errors in this English speech transcript. "
                f"Fix only obvious mistakes. "
                f"Context: user is talking to an AI assistant.{context_section}"
                f"Transcript: {text} Return ONLY corrected text."
            )
            corrected, _ = chat(
                [{"role": "user", "content": prompt}],
                role="companion",
                temperature=0.05,
                max_tokens=min(len(text) + 200, 1024),
            )
            if corrected:
                return corrected
        except Exception as e:
            logger.warning("LLM transcript correction via %s failed: %s", provider, e)
    return text


def _sarvam_transcribe(audio_bytes: bytes, filename: str = "audio.webm") -> Tuple[str, str, float]:
    try:
        import requests

        api_key = config.keys.get("sarvam", "")
        if not api_key:
            return "", "en", 0.0

        url = "https://api.sarvam.ai/speech-to-text"
        headers = {"api-subscription-key": api_key}
        payload = {
            "model": config.sarvam_stt_model,
            "language_code": "en-IN",
            "audio": {
                "source": {
                    "audio_url": None,
                }
            },
        }
        files = {
            "file": (filename, BytesIO(audio_bytes), "audio/webm"),
        }

        resp = requests.post(url, data={"model": config.sarvam_stt_model,
                                        "language_code": "en-IN"},
                             files=files, timeout=30)
        resp.raise_for_status()
        data = resp.json()

        text = (data.get("transcript") or data.get("text") or "").strip()
        lang = data.get("language_code", data.get("language", "en-IN"))
        if isinstance(lang, str):
            lang = lang.split("-")[0]
        confidence = float(data.get("confidence", 0.95))

        if text:
            logger.info("Sarvam STT (%s) OK: lang=%s conf=%.2f",
                        config.sarvam_stt_model, lang, confidence)
            text = _correct_transcript(text)
            return text, lang, confidence
    except Exception as e:
        logger.warning("Sarvam STT failed: %s", e)
    return "", "en", 0.0


def transcribe(audio_bytes: bytes, filename: str = "audio.webm") -> Tuple[str, str, float]:
    """Transcribe audio to text. Order: local faster-whisper -> Sarvam -> Groq.

    Local first because:
    1. No API key needed
    2. No network latency
    3. No quota limits
    4. Works offline
    """
    # 1) LOCAL faster-whisper (offline, no key needed) — try first for speed.
    local_ok = False
    local_text = ""
    local_lang = "en"
    try:
        from faster_whisper import WhisperModel
        import tempfile, os as _os, wave, io as _io
        
        # Save as WAV with proper headers for faster-whisper.
        wav_path = None
        try:
            wav_buf = _io.BytesIO()
            with wave.open(wav_buf, "wb") as wf:
                wf.setnchannels(1)
                wf.setsampwidth(2)
                wf.setframerate(16000)
                wf.writeframes(audio_bytes)
            wav_buf.seek(0)
            with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as f:
                f.write(wav_buf.read())
                wav_path = f.name
            
            model = WhisperModel("tiny", device="cpu", compute_type="int8")
            segs, info = model.transcribe(wav_path, beam_size=3)
            text = " ".join(s.text for s in segs).strip()
            lang = info.language or "en"
            local_text = text
            local_lang = lang
            local_ok = True
            if text:
                text = _correct_transcript(text)
                logger.info("Local STT OK: lang=%s text=%r", lang, text[:60])
                return text, lang, 0.9
            else:
                logger.debug("Local STT: empty result (no speech detected)")
        except Exception as e:
            logger.warning("Local STT failed: %s", e)
        finally:
            if wav_path and _os.path.exists(wav_path):
                try:
                    _os.unlink(wav_path)
                except Exception:
                    pass
    except Exception as e:
        logger.warning("Local STT import failed: %s", e)

    # If local STT ran successfully but returned empty, that's a valid result
    # (no speech in the audio). Don't spam cloud APIs with empty audio.
    if local_ok:
        return "", local_lang, 0.0

    # 2) Sarvam Saaras V3 (cloud, needs key).
    if config.has_key("sarvam"):
        text, lang, conf = _sarvam_transcribe(audio_bytes, filename)
        if text:
            return text, lang, conf

    # 3) Groq Whisper (cloud, needs key).
    if config.has_key("groq"):
        try:
            client = _get_client("groq")
            resp = client.audio.transcriptions.create(
                model="whisper-large-v3-turbo",
                file=(filename, BytesIO(audio_bytes)),
            )
            text = (resp.text or "").strip()
            lang = getattr(resp, "language", "en") or "en"
            if text:
                text = _correct_transcript(text)
                return text, lang, 0.8
        except Exception as e:
            logger.warning("Groq STT failed: %s", e)
    return "", "en", 0.0
