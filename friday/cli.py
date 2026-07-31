"""
Friday Base - Work Mode (CLI)

Start Friday and work with him directly, like a live chat:
  python run.py --chat          # text REPL
  python run.py --chat --voice   # mic in + spoken replies

Voice input needs:  pip install sounddevice soundfile numpy
(falls back to typing cleanly if those are absent).

Learning loop (Phase 3 seed):
  - After each turn, tool failures/errors are auto-captured as lessons
    into memory (surfaced next session via system_context).
  - In-chat commands:
      !teach <fact>   remember something durable
      !forget <text>  forget a matching fact
      !lessons        show captured lessons
      !facts           show remembered facts
      !limit <n>     set agentic max steps for long tasks (default 12)
      !help            this
      !quit / !exit   leave
"""
import argparse
import logging
import os
import sys
import tempfile
import time

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(name)s] %(message)s")
logger = logging.getLogger("Friday.CLI")


def _banner():
    print("\n  Friday - work mode")
    print("  Text chat with your personal agent. Type !help for commands.\n")


def _print_help():
    print(
        "Commands:\n"
        "  !teach <fact>   remember something durable\n"
        "  !forget <text>  forget a matching fact\n"
        "  !lessons        show captured lessons\n"
        "  !facts           show remembered facts\n"
        "  !limit <n>      max agentic steps for long tasks (default 12)\n"
        "  !help            this\n"
        "  !quit / !exit   leave\n"
    )


# ── Long-running task ceiling ───────────────────────────────────────────────
# When YOU command a big task, Friday may take more steps. Default 12 (still
# guarded by the per-turn deadline so he never hangs).
_MAX_STEPS = [12]


def _capture_lesson(memory, steps):
    """Auto-learn: a tool step that errored becomes a lesson."""
    if not memory:
        return
    try:
        for s in (steps or []):
            result = (s or {}).get("result", "")
            if isinstance(result, str) and (
                "error" in result.lower() or "blocked" in result.lower()
                or "timed out" in result.lower() or "refused" in result.lower()
            ):
                action = s.get("action") or "action"
                memory.learn_lesson(
                    f"tool '{action}' failed on a prior request",
                    f"result was: {result[:160]}",
                )
    except Exception:
        pass


def _record(seconds: int = 6) -> bytes:
    """Record one utterance from the default mic. Returns wav bytes, or b''."""
    try:
        import sounddevice as sd
        import numpy as np
        import soundfile as sf
    except Exception as e:
        print(f"(mic libs missing: {e}. pip install sounddevice soundfile numpy)")
        return b""
    try:
        sd.query_devices(kind="input")
    except Exception:
        print("(no microphone found)")
        return b""
    print("Listening... (speak, then wait)")
    try:
        sr = 16000
        rec = sd.rec(int(seconds * sr), samplerate=sr, channels=1, dtype="int16")
        sd.wait()
        buf = tempfile.NamedTemporaryFile(suffix=".wav", delete=False)
        sf.write(buf.name, rec, sr)
        path = buf.name
        with open(path, "rb") as fh:
            data = fh.read()
        os.unlink(path)
        return data
    except Exception as e:
        print(f"(mic error: {e})")
        return b""


def _play(audio_bytes: bytes) -> None:
    """Play mp3/ogg/wav bytes with whatever player is available."""
    if not audio_bytes:
        return
    try:
        import soundfile as sf
        sr = 24000
        tmp = tempfile.NamedTemporaryFile(suffix=".wav", delete=False)
        sf.write(tmp.name, __import__("io").BytesIO(audio_bytes).read(), sr)
        path = tmp.name
        if sys.platform == "win32":
            os.system(f'start /min "" "{path}"')
        else:
            os.system(f'afplay "{path}" 2>/dev/null || aplay "{path}" 2>/dev/null || true')
    except Exception:
        pass


def main():
    ap = argparse.ArgumentParser(description="Friday work mode")
    ap.add_argument("--voice", action="store_true", help="use microphone + spoken replies")
    args = ap.parse_args()

    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    from .config import config
    from .memory import Memory
    from . import llm, voice
    from .orchestrator import handle_turn

    if not config.has_any_key():
        if config.provider_mode == "local":
            print("No local model found. Set LOCAL_MODEL_PATH in .env.")
        else:
            print("No LLM key found. Set DEEPSEEK_API_KEY in .env.")
        return

    memory = Memory(config.data_dir, llm_chat=llm.chat, summarize_interval=10)
    _banner()

    while True:
        try:
            if args.voice:
                data = _record()
                if not data:
                    continue
                from . import llm as _llm
                text, _, _ = _llm.transcribe(data, filename="audio.wav")
                if not text:
                    print("(didn't catch that)")
                    continue
                print(f"You: {text}")
            else:
                try:
                    text = input("You: ").strip()
                except (EOFError, KeyboardInterrupt):
                    print("\nBye.")
                    break
        except (EOFError, KeyboardInterrupt):
            print("\nBye.")
            break

        if not text:
            continue
        if text.startswith("!"):
            _cmd(text, memory)
            continue

        try:
            # Long tasks get the raised step ceiling; deadline still caps total time.
            result = handle_turn(text, lang="en", memory=memory,
                                 max_steps=_MAX_STEPS[0])
            reply = result.get("reply", "")
            print(f"\nFriday: {reply}")
            _capture_lesson(memory, result.get("steps", []))
            if args.voice:
                try:
                    audio = voice.synthesize(reply, natural=True)
                    _play(audio)
                except Exception as e:
                    logger.warning("TTS playback skipped: %s", e)
        except Exception as e:
            print(f"Friday: (error) {e}")


def _cmd(text, memory):
    parts = text[1:].split(" ", 1)
    cmd = parts[0].lower()
    arg = parts[1] if len(parts) > 1 else ""
    if cmd in ("quit", "exit"):
        print("Bye.")
        sys.exit(0)
    elif cmd == "help":
        _print_help()
    elif cmd == "upgrade":
        if not arg:
            print("usage: !upgrade <goal>  (he plans, builds, tests, applies)")
            return
        print("Friday: planning + building + testing + applying...")
        try:
            from . import upgrader
            rec = upgrader.upgrade_and_apply(arg)
            st = rec.get("status")
            print(f"  status: {st}")
            if st == "applied":
                print("  APPLIED. Restart Friday for it to take effect"
                      " (Python does not hot-reload). Backup saved for rollback.")
            elif st == "tested_fail":
                print("  NOT applied - tests failed. Nothing changed.")
            else:
                print(f"  not applied ({st}). Nothing changed.")
        except Exception as e:
            print(f"  upgrade error: {e}")
    elif cmd == "limit":
        try:
            _MAX_STEPS[0] = max(2, min(40, int(arg)))
            print(f"Agentic max steps set to {_MAX_STEPS[0]}")
        except Exception:
            print("usage: !limit <number>")
    elif cmd == "teach":
        if arg:
            memory.remember_fact(arg)
            print(f"Remembered: {arg}")
        else:
            print("usage: !teach <fact>")
    elif cmd == "forget":
        if not arg:
            print("usage: !forget <text>")
            return
        facts = memory.long_term.get("facts", [])
        before = len(facts)
        memory.long_term["facts"] = [f for f in facts if arg.lower() not in f.lower()]
        memory._save("long_term")
        print(f"Forgot {before - len(memory.long_term['facts'])} matching fact(s).")
    elif cmd == "facts":
        facts = memory.long_term.get("facts", [])
        print("\n".join(f"- {f}" for f in facts) or "(nothing remembered yet)")
    elif cmd == "lessons":
        eps = memory.episodic[-10:]
        print("\n".join(f"- {e['situation']} -> {e['fix']}" for e in eps) or "(no lessons yet)")
    else:
        print(f"unknown command: !{cmd}  (try !help)")


if __name__ == "__main__":
    main()
