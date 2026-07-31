"""
Friday Base - Configuration  (v2: per-role provider CHAINS)
Each worker role has an ORDERED chain of (provider, model) candidates.
llm.chat(role) walks the chain: first that succeeds wins, rest are failover.
"""
import os
import re
from dataclasses import dataclass
from typing import Dict, List

try:
    from dotenv import load_dotenv
    load_dotenv()
except Exception:
    pass

def _parse_seconds(val: str) -> float:
    """Parse duration string like '60s', '300s', '1.5m' to seconds (float)."""
    if not val:
        return 0.0
    val = str(val).strip().lower()
    # Remove any non-numeric suffix (s, m, h, ms)
    val = re.sub(r'[smh]+$', '', val)
    val = val.replace('ms', '')  # Handle milliseconds
    try:
        num = float(val)
        if val.endswith('m') or 'm' in val:
            return num * 60
        if val.endswith('h'):
            return num * 3600
        return num
    except ValueError:
        return 0.0

PROVIDER_ENDPOINTS = {
    "groq":       ("https://api.groq.com/openai/v1", "GROQ_API_KEY"),
    "gemini":     ("https://generativelanguage.googleapis.com/v1beta/openai/", "GEMINI_API_KEY"),
    "deepseek":   ("https://api.deepseek.com", "DEEPSEEK_API_KEY"),
    "openrouter": ("https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"),
    "openai":     ("https://api.openai.com/v1", "OPENAI_API_KEY"),
    "sarvam":     ("https://api.sarvam.ai", "SARVAM_API_KEY"),
    "ollama":     ("http://localhost:11434/v1", "OLLAMA_API_KEY"),
    "opencode":   ("https://opencode.ai/zen/v1", "OPENCODE_API_KEY"),
    # Free, powerful Chinese labs (no credit card). Qwen = Alibaba intl DashScope,
    # Zhipu = Z.ai/BigModel. Both OpenAI-compatible and tool-calling capable.
    "qwen":       ("https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
                   "DASHSCOPE_API_KEY"),
    "zhipu":      ("https://open.bigmodel.cn/api/paas/v4/", "ZHIPU_API_KEY"),
    "nvidia":     ("https://integrate.api.nvidia.com/v1", "NVIDIA_API_KEY"),
    "github":     ("https://models.inference.ai.azure.com", "GITHUB_TOKEN"),
    # DeepInfra - OpenAI-compatible API with many models
    "deepinfra":  ("https://api.deepinfra.com/v1/openai", "DEEPINFRA_API_KEY"),
    # SiliconFlow - OpenAI-compatible API with many Chinese models
    "siliconflow": ("https://api.siliconflow.cn/v1", "SILICONFLOW_API_KEY"),
}


@dataclass
class Candidate:
    provider: str
    model: str


class Config:
    def __init__(self):
        self.deploy_mode = os.getenv("DEPLOY_MODE", "local")
        self.host = os.getenv("HOST", "0.0.0.0")
        self.port = int(os.getenv("PORT", 8000))
        self.dev_mode = os.getenv("DEV_MODE", "true").lower() == "true"

        self.api_token = os.getenv("FRIDAY_TOKEN", "").strip()
        self.enable_exec_tools = os.getenv("ENABLE_EXEC_TOOLS", "true").lower() == "true"

        self.keys: Dict[str, str] = {}
        for name, (_, env) in PROVIDER_ENDPOINTS.items():
            k = os.getenv(env, "").strip()
            if k:
                self.keys[name] = k

        self.provider_mode = os.getenv("PROVIDER_MODE", "cloud").lower()
        self.local_model_path = os.getenv("LOCAL_MODEL_PATH", os.path.join(
            os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "models", "qwen2.5-vl-7b-instruct.Q4_K_M.gguf"))
        self.local_model_context = int(os.getenv("LOCAL_MODEL_CONTEXT", "8192"))

        m_groq   = os.getenv("GROQ_MODEL",   "llama-3.3-70b-versatile")
        m_gemini = os.getenv("GEMINI_MODEL", "gemini-2.0-flash")
        m_deep   = os.getenv("DEEPSEEK_MODEL", "deepseek-chat")
        m_deep_reason = os.getenv("DEEPSEEK_REASONER_MODEL", "deepseek-reasoner")
        m_or     = os.getenv("OPENROUTER_MODEL", "meta-llama/llama-3.3-70b-instruct:free")
        m_openai = os.getenv("OPENAI_MODEL", "gpt-4o-mini")
        m_openai_vision = os.getenv("OPENAI_VISION_MODEL", "gpt-4o-mini")
        m_sarvam_stt = os.getenv("SARVAM_STT_MODEL", "saaras:v3")
        m_sarvam_tts = os.getenv("SARVAM_TTS_MODEL", "bulbul:v3")
        # Free powerful labs. Qwen (Alibaba) gets 70M free signup tokens on the
        # intl endpoint; Zhipu GLM-4.7-Flash is free forever. Swap the model id
        # via env if you want a different free-tier model.
        m_qwen = os.getenv("QWEN_MODEL", "qwen-plus")
        m_zhipu = os.getenv("ZHIPU_MODEL", "glm-5.2")
        m_opencode = os.getenv("OPENCODE_MODEL", "deepseek-v4-flash-free")
        m_nvidia  = os.getenv("NVIDIA_MODEL",  "meta/llama-3.3-70b-instruct")
        m_github  = os.getenv("GITHUB_MODEL",  "gpt-4o-mini")
        mm = {"groq": m_groq, "gemini": m_gemini, "deepseek": m_deep,
              "openrouter": m_or, "openai": m_openai,
              "qwen": m_qwen, "zhipu": m_zhipu, "opencode": m_opencode,
              "nvidia": m_nvidia, "github": m_github}

        # Expose vision models for the eye (screen/camera describe).
        self.gemini_model = m_gemini
        self.openai_vision_model = m_openai_vision

        _main_model = os.getenv("OLLAMA_MODEL", "qwen2.5:1.5b")
        _fast_model = os.getenv("OLLAMA_MODEL_FAST", "qwen2.5:1.5b")
        if self.provider_mode == "local":
            model_map = {"ollama": _main_model, "ollama_fast": _fast_model,
                         "deepseek": m_deep, "deepseek_reasoner": m_deep_reason, "groq": m_groq}
            main_chain = ["groq", "deepseek", "ollama"]
        else:
            model_map = {"deepseek": m_deep, "deepseek_reasoner": m_deep_reason,
                         "groq": m_groq, "ollama": _main_model,
                         "qwen": m_qwen, "zhipu": m_zhipu, "opencode": m_opencode,
                         "gemini": m_gemini, "openrouter": m_or,
                         "nvidia": m_nvidia, "github": m_github,
                         "deepinfra": "meta-llama/Meta-Llama-3-8B-Instruct",
                         "siliconflow": "deepseek-ai/deepseek-chat"}
            # CLOUD-FIRST: best tool-calling providers first, Ollama LAST as emergency fallback
            main_chain = ["opencode", "groq", "gemini", "openrouter", "deepseek",
                          "nvidia", "github", "deepinfra", "siliconflow",
                          "zhipu", "qwen", "ollama"]

        # CLOUD-FIRST (crash-free) mode: DeepSeek-chat is the primary fast brain
        # (~$2/mo, me-grade, 1-3s latency) for companion/chat. DeepSeek-reasoner
        # (10-25s, deeper logic) powers reasoner/coder/researcher roles for heavy
        # work: trading backtests, debugging, architecture. Groq = free fast backup.
        # Ollama is EXCLUDED from active chains so the 9B never auto-loads (OOM fix).
        _groq_candidate = Candidate(provider="groq", model=m_groq)
        _deepseek_candidate = Candidate(provider="deepseek", model=m_deep)
        _deepseek_reasoner_candidate = Candidate(provider="deepseek", model=m_deep_reason)
        _qwen_candidate = Candidate(provider="qwen", model=m_qwen)
        _zhipu_candidate = Candidate(provider="zhipu", model=m_zhipu)
        _opencode_candidate = Candidate(provider="opencode", model=m_opencode)
        _gemini_candidate = Candidate(provider="gemini", model=m_gemini)
        _openrouter_candidate = Candidate(provider="openrouter", model=m_or)
        _nvidia_candidate = Candidate(provider="nvidia", model=m_nvidia)
        _github_candidate = Candidate(provider="github", model=m_github)
        _deepinfra_candidate = Candidate(provider="deepinfra", model="meta-llama/Meta-Llama-3-8B-Instruct")
        _siliconflow_candidate = Candidate(provider="siliconflow", model="deepseek-ai/deepseek-chat")
        # Exhaustive chain: try every provider we have. Working free first,
        # then paid fallback, then unknown/dead providers last. Friday never
        # gives up until every candidate has been tried.
        # Primary: GitHub (free, smart, working). Fallbacks if GitHub ever fails.
        _chat_candidates = [
            _opencode_candidate,    # PRIMARY: free, no rate limits, unlimited
            _github_candidate,      # fallback: free GPT-4o-mini, 6000/day
            _groq_candidate,        # fallback: fast but 429 lately
            _gemini_candidate,      # fallback
            _deepseek_candidate,    # paid fallback
        ]
        _reason_candidates = [
            _opencode_candidate,    # PRIMARY: free, unlimited
            _github_candidate,
            _groq_candidate,
            _gemini_candidate,
            _deepseek_reasoner_candidate,
            _deepseek_candidate,
        ]

        self.role_chains: Dict[str, List[Candidate]] = {
            "companion":  _chat_candidates[:],
            "reasoner":   _reason_candidates[:],
            "coder":      _reason_candidates[:],
            "researcher": _reason_candidates[:],
            "judge":      _chat_candidates[:],
            "verifier":   _chat_candidates[:],
            "router":     _chat_candidates[:],
        }
        self._default_role = "companion"

        # "My level, no hard restriction" tuning: more retries, longer timeouts,
        # self-check on, research browser on, self-upgrade enabled. Money/trading
        # stays demo-only (that's not a brain restriction, it's not blowing accounts).
        self.max_retries = int(os.getenv("MAX_RETRIES", 5))
        self.verify_passes = int(os.getenv("VERIFY_PASSES", 1))
        self.breaker_threshold = int(os.getenv("BREAKER_THRESHOLD", 3))
        self.breaker_cooldown = _parse_seconds(os.getenv("BREAKER_COOLDOWN", "10"))
        self.llm_call_timeout = _parse_seconds(os.getenv("LLM_CALL_TIMEOUT", "15"))
        # Generous deadline so long reasoning/voice answers aren't cut short.
        self.turn_deadline_seconds = _parse_seconds(os.getenv("TURN_DEADLINE_SECONDS", "60"))

        # ── "Thinks-like-you" toggles (all ON for full capability) ──
        self.single_call_mode = os.getenv("SINGLE_CALL_MODE", "true").lower() == "true"
        # TTS: generate audio asynchronously so it never blocks the text reply.
        self.tts_generate_async = os.getenv("TTS_GENERATE_ASYNC", "true").lower() == "true"
        # Paid Sarvam TTS is OFF by default; edge-tts (free) is used instead.
        self.tts_paid_enabled = os.getenv("TTS_PAID_ENABLED", "false").lower() == "true"
        self.primary_chat_provider = os.getenv("PRIMARY_CHAT_PROVIDER", "deepseek")
        self.ollama_model = os.getenv("OLLAMA_MODEL", "qwen3.5:9b")
        self.ollama_model_fast = os.getenv("OLLAMA_MODEL_FAST", "qwen2.5:1.5b")
        self.ollama_vision_model = os.getenv("OLLAMA_VISION_MODEL", "moondream:latest")
        # Creative, local, zero-extra-cost features.
        self.enable_anticipation = os.getenv("ENABLE_ANTICIPATION", "true").lower() == "true"
        self.enable_user_memory = os.getenv("ENABLE_USER_MEMORY", "true").lower() == "true"
        # Higher-fidelity mode: reason() + one self-check pass (cost ~2 calls).
        self.enable_selfcheck = os.getenv("ENABLE_SELFCHECK", "false").lower() == "true"
        # Local answer cache TTL (seconds). 0 = disabled (full freshness).
        self.local_cache_ttl = _parse_seconds(os.getenv("LOCAL_CACHE_TTL", "600"))

        # ── Autonomous research/build/verify/promote engine ──
        # Each topic gets its own isolated folder under RESEARCH_DIR. The script
        # must self-test 100% (green) before it is "promoted" as verified.
        self.research_dir = os.getenv("RESEARCH_DIR", os.path.join(
            os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "research"))
        self.research_max_steps = int(os.getenv("RESEARCH_MAX_STEPS", 25))
        self.research_max_fixes = int(os.getenv("RESEARCH_MAX_FIXES", 6))
        # Headless browser for JS/SPA sources (needs Playwright installed).
        self.research_browser = os.getenv("RESEARCH_BROWSER", "true").lower() == "true"
        os.makedirs(self.research_dir, exist_ok=True)

        # ── Laptop execution ──
        self.execute_on_host = os.getenv("EXECUTE_ON_HOST", "false").lower() == "true"
        self.confirm_destructive = os.getenv("CONFIRM_DESTRUCTIVE", "false").lower() == "true"

        self.stt_provider = os.getenv("STT_PROVIDER", "sarvam")
        self.tts_engine = os.getenv("TTS_ENGINE", "edge")
        self.tts_voice_en = os.getenv("TTS_VOICE_EN", "en-IN-ShaanNeural")
        self.tts_voice_edge = os.getenv("TTS_VOICE_EDGE", "en-IN-NeerjaNeural")
        self.tts_pitch_edge = os.getenv("TTS_PITCH_EDGE", "+0Hz")
        self.allow_gtts_voice = os.getenv("ALLOW_GTTS_VOICE", "false").lower() == "true"
        self.voice_natural = os.getenv("VOICE_NATURAL", "true").lower() == "true"
        self.local_vision_enabled = os.getenv("LOCAL_VISION_ENABLED", "true").lower() == "true"

        # ── Live screen watcher (proactive) ──
        self.screen_watch = os.getenv("SCREEN_WATCH", "true").lower() == "true"
        self.screen_watch_interval = int(os.getenv("SCREEN_WATCH_INTERVAL", 30))
        self.screen_watch_fast = os.getenv("SCREEN_WATCH_FAST", "false").lower() == "true"
        self.screen_watch_fast_interval = int(os.getenv("SCREEN_WATCH_FAST_INTERVAL", 3))
        self.eye_guide_only = os.getenv("EYE_GUIDE_ONLY", "false").lower() == "false"
        self.proactive_act = os.getenv("PROACTIVE_ACT", "true").lower() == "true"
        self.wake_word = os.getenv("WAKE_WORD", "friday")
        self.greeting_text = os.getenv("GREETING_TEXT", "At your service, sir")
        self.listening_timeout = int(os.getenv("LISTENING_TIMEOUT", "8"))

        self.sarvam_stt_model = os.getenv("SARVAM_STT_MODEL", "saaras:v3")
        self.sarvam_tts_model = os.getenv("SARVAM_TTS_MODEL", "bulbul:v3")

        self.data_dir = os.getenv("MEMORY_DIR", os.path.join(
            os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "base"))
        os.makedirs(self.data_dir, exist_ok=True)

        self.wife_agent_voice = os.getenv("WIFE_AGENT_VOICE", "en-IN-NeerjaNeural")
        self.wife_agent_name = os.getenv("WIFE_AGENT_NAME", "Assistant")
        self.wife_agent_enabled = os.getenv("WIFE_AGENT_ENABLED", "false").lower() == "true"

    def _chain(self, env_name, default_order, model_map) -> List[Candidate]:
        order = os.getenv(env_name, "")
        providers = [p.strip() for p in order.split(",") if p.strip()] or default_order
        return [Candidate(provider=p, model=model_map[p]) for p in providers if p in model_map]

    def endpoint(self, provider: str) -> str:
        return PROVIDER_ENDPOINTS[provider][0]

    def has_key(self, provider: str) -> bool:
        if provider == "ollama" and self.provider_mode == "local":
            return True
        if provider == "opencode":
            return True  # no API key needed
        return provider in self.keys

    def has_any_key(self) -> bool:
        return self.provider_mode == "local" or self.has_key("opencode") or bool(self.keys)

    def live_chain(self, role: str) -> List[Candidate]:
        chain = self.role_chains.get(role) or self.role_chains[self._default_role]
        # Hybrid mode: always try local Ollama first, then fall back to cloud
        # providers that have valid keys. This enables offline-first with
        # cloud escalation for complex tasks.
        candidates = []
        for c in chain:
            if c.provider == "ollama":
                candidates.append(c)
            elif self.has_key(c.provider):
                candidates.append(c)
        return candidates

    def active_providers(self) -> List[str]:
        return list(self.keys.keys())


config = Config()
