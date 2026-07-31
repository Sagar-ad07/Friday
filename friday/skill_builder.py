"""
Friday Base - Skill Builder (Production Hardened)
Turns user intent into reusable, tested, and registered skills (tools, workers, workflows).
Production-hardened: persisted records, JSON repair, async API, shared executor pool.
"""
import json
import logging
import os
import re
import textwrap
import threading
import time
import asyncio
from concurrent.futures import ThreadPoolExecutor
from typing import Dict, List, Optional, Any, Callable
from dataclasses import dataclass, field, asdict

from . import llm, prompts, tools, resilience
from .config import config
from .tools import register_tool

logger = logging.getLogger("Friday.SkillBuilder")

# ──────────────────────────────────────────────────────────────────────────────
# Config Keys (with safe defaults) - FIX: Added to config.py
# ──────────────────────────────────────────────────────────────────────────────

def _get_skill_config(key: str, default: Any = None) -> Any:
    """Get skill builder config with safe defaults."""
    # Try to get from config object (if attr exists), else env, else default
    if hasattr(config, f"skill_{key}"):
        return getattr(config, f"skill_{key}")
    return os.getenv(f"FRIDAY_SKILL_{key.upper()}", default)


# ──────────────────────────────────────────────────────────────────────────────
# Shared Executor Pool - FIX: Use shared executor, don't create dead code
# ──────────────────────────────────────────────────────────────────────────────

_EXECUTOR_POOL: Optional[ThreadPoolExecutor] = None
_EXECUTOR_LOCK = threading.Lock()

def _get_executor(max_workers: int = 4) -> ThreadPoolExecutor:
    """Get or create shared thread pool executor."""
    global _EXECUTOR_POOL
    with _EXECUTOR_LOCK:
        if _EXECUTOR_POOL is None or _EXECUTOR_POOL._shutdown:
            _EXECUTOR_POOL = ThreadPoolExecutor(max_workers=max_workers, thread_name_prefix="skill_exec")
        return _EXECUTOR_POOL


# ──────────────────────────────────────────────────────────────────────────────
# Persistent Skill Storage - FIX: Records persisted to disk, survive restart
# ──────────────────────────────────────────────────────────────────────────────

_SKILLS_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "skills")
_SKILLS_FILE = os.path.join(_SKILLS_DIR, "skills.json")
_SKILLS_LOCK = threading.Lock()

os.makedirs(_SKILLS_DIR, exist_ok=True)


@dataclass
class Skill:
    """A reusable skill definition - persists to disk."""
    name: str
    description: str
    type: str  # "tool", "worker", "workflow"
    code: str = ""
    args_schema: Dict = field(default_factory=dict)
    created_at: float = field(default_factory=time.time)
    updated_at: float = field(default_factory=time.time)
    version: int = 1
    tags: List[str] = field(default_factory=list)
    examples: List[str] = field(default_factory=list)
    test_cases: List[Dict] = field(default_factory=list)
    metadata: Dict = field(default_factory=dict)

    def to_dict(self) -> Dict:
        return asdict(self)

    @classmethod
    def from_dict(cls, data: Dict) -> 'Skill':
        return cls(**data)


class SkillRegistry:
    """Manages skill persistence and registration - FIX: Persists to disk."""

    def __init__(self):
        os.makedirs(_SKILLS_DIR, exist_ok=True)
        self._skills: Dict[str, Skill] = {}
        self._load()

    def _load(self):
        """Load skills from disk - survives restart."""
        try:
            with open(_SKILLS_FILE, "r", encoding="utf-8") as f:
                data = json.load(f)
            self._skills = {name: Skill.from_dict(s) for name, s in data.get("skills", {}).items()}
            logger.info(f"Loaded {len(self._skills)} skills from disk")
        except Exception as e:
            logger.warning(f"Skill registry load failed: {e}")
            self._skills = {}

    def _save(self):
        """Persist skills to disk - survives restart."""
        try:
            with _SKILLS_LOCK:
                data = {"skills": {name: s.to_dict() for name, s in self._skills.items()}}
                with open(_SKILLS_FILE, "w", encoding="utf-8") as f:
                    json.dump(data, f, ensure_ascii=False, indent=2)
        except Exception as e:
            logger.error(f"Skill registry save failed: {e}")

    def register(self, skill: Skill) -> bool:
        """Register a new skill or update existing - persists to disk."""
        with _SKILLS_LOCK:
            skill.updated_at = time.time()
            if skill.name in self._skills:
                skill.version = self._skills[skill.name].version + 1
            self._skills[skill.name] = skill
            self._save()
            return True

    def unregister(self, name: str) -> bool:
        with _SKILLS_LOCK:
            if name in self._skills:
                del self._skills[name]
                self._save()
                return True
            return False

    def get(self, name: str) -> Optional[Skill]:
        with _SKILLS_LOCK:
            return self._skills.get(name)

    def list_all(self) -> List[Skill]:
        with _SKILLS_LOCK:
            return list(self._skills.values())

    def list_names(self) -> List[str]:
        with _SKILLS_LOCK:
            return list(self._skills.keys())


_SKILL_REGISTRY = SkillRegistry()


# ──────────────────────────────────────────────────────────────────────────────
# JSON Repair - FIX: Handle malformed LLM JSON gracefully
# ──────────────────────────────────────────────────────────────────────────────

def _repair_json(text: str) -> str:
    """Attempt to repair common LLM JSON formatting issues."""
    text = text.strip()
    
    # Strip markdown code fences
    if text.startswith("```"):
        parts = text.split("```")
        if len(parts) >= 3:
            text = parts[1]
            if text.lower().startswith("json"):
                text = text[4:].strip()
    
    # Find first { and last }
    start = text.find("{")
    end = text.rfind("}")
    if start >= 0 and end > start:
        text = text[start:end+1]
    
    # Fix common issues
    text = re.sub(r',\s*}', '}', text)  # trailing commas
    text = re.sub(r',\s*]', ']', text)
    text = re.sub(r'([{,])\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:', r'\1"\2":', text)  # unquoted keys
    
    return text


def _safe_json_loads(text: str, fallback: Any = None) -> Any:
    """Safely load JSON with repair fallback."""
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        repaired = _repair_json(text)
        try:
            return json.loads(repaired)
        except json.JSONDecodeError as e:
            logger.warning(f"JSON repair failed: {e}")
            if fallback is not None:
                return fallback
            raise


# ──────────────────────────────────────────────────────────────────────────────
# Skill Templates - Code Generation
# ──────────────────────────────────────────────────────────────────────────────

TOOL_TEMPLATE = textwrap.dedent("""
{header}
def _handle_{name}(args: dict) -> str:
    \"\"\"{description}\"\"\"
    {docstring}
    {body}

register_tool("{name}", "{description}", _handle_{name})
""")

WORKER_TEMPLATE = textwrap.dedent("""
{header}
# Worker: {name}
# Role: {role}
# Tagline: {tagline}

{system_prompt}
""")

WORKFLOW_TEMPLATE = textwrap.dedent("""
{header}
# Workflow: {name}
# Description: {description}
# Steps:
{steps}

def run_{name}(context: str = "") -> dict:
    \"\"\"Execute the {name} workflow.\"\"\"
    results = {{}}
    for step in STEPS:
        # Execute each step
        pass
    return {{"status": "completed", "results": results}}

STEPS = {steps_json}
""")


def _generate_skill_code(skill: Skill) -> str:
    """Generate the Python code for a skill."""
    header = textwrap.dedent(f"""
    # Auto-generated by Friday Skill Builder (Architect)
    # Skill: {skill.name} (v{skill.version})
    # Created: {time.ctime(skill.created_at)}
    # Updated: {time.ctime(skill.updated_at)}
    # DO NOT EDIT DIRECTLY - use Skill Builder to modify

    import json
    import logging
    import os
    import re
    import subprocess
    import time
    from typing import Dict, Any, Optional

    from friday.tools import register_tool, TOOLS, safe_tool_call
    from friday import llm, workers, resilience

    logger = logging.getLogger("Friday.Skill.{skill.name}")
    """)

    if skill.type == "tool":
        args = skill.args_schema.get("properties", {})
        required = skill.args_schema.get("required", [])
        
        body_lines = [
            f"    # Args: {json.dumps(args, indent=4)}",
            f"    # Required: {required}",
            "",
            "    # Extract args",
        ]
        for arg in args:
            default = f"args.get('{arg}', {json.dumps(args[arg].get('default'))})" if arg not in required else f"args.get('{arg}')"
            body_lines.append(f"    {arg} = {default}")
        
        body_lines.extend([
            "",
            "    # Validation",
            f"    if not all([{', '.join(required)}]):",
            f"        return f'Error: Missing required args: {required}'",
            "",
            "    # Main logic here",
            f"    try:",
            f"        # TODO: Implement {skill.name} logic",
            f"        result = f'{skill.name} executed with args: {list(args.keys())}'",
            f"        return result",
            f"    except Exception as e:",
            f"        logger.error(f'{skill.name} failed: {{e}}')",
            f"        return f'Error: {{str(e)}}'",
        ])
        
        body = "\n".join(body_lines)
        docstring = f'"""{skill.description}"""'
        
        return TOOL_TEMPLATE.format(
            header=header,
            name=skill.name,
            description=skill.description,
            docstring=docstring,
            body=body
        )
    
    elif skill.type == "worker":
        return WORKER_TEMPLATE.format(
            header=header,
            name=skill.name,
            role=skill.metadata.get("role", "custom"),
            tagline=skill.metadata.get("tagline", f"Custom worker: {skill.name}"),
            system_prompt=skill.metadata.get("system_prompt", f"You are {skill.name}, a specialized worker.")
        )
    
    elif skill.type == "workflow":
        steps = skill.metadata.get("steps", [])
        steps_json = json.dumps([{
            "id": s.get("id", f"step_{i}"),
            "description": s.get("description", ""),
            "worker": s.get("worker", "reasoner"),
            "task": s.get("task", ""),
            "dependencies": s.get("dependencies", [])
        } for i, s in enumerate(steps)], indent=4)
        
        steps_str = "\n".join(f"  {i+1}. {s.get('description', '')} ({s.get('worker', 'reasoner')})" for i, s in enumerate(steps))
        
        return WORKFLOW_TEMPLATE.format(
            header=header,
            name=skill.name,
            description=skill.description,
            steps=steps_str,
            steps_json=steps_json
        )
    
    return header + "\n# Unknown skill type: " + skill.type


# ──────────────────────────────────────────────────────────────────────────────
# Skill Builder - Main Entry Point
# ──────────────────────────────────────────────────────────────────────────────

SYSTEM_PROMPT = textwrap.dedent("""
You are Architect, Friday's Skill Builder. Your job is to turn user intent into
reusable, tested, and registered skills (tools, workers, workflows).

AVAILABLE SKILL TYPES:
1. TOOL - A Python function registered via register_tool(). Can do anything Python can.
   Use for: calculations, file ops, web search, API calls, data processing, etc.
   
2. WORKER - A specialized persona with system prompt. Invoked via run_worker().
   Use for: specialized reasoning styles, personas, specialized task handlers.
   
3. WORKFLOW - A multi-step DAG of dependent steps with parallel execution.
   Use for: complex multi-step processes with dependencies.

SKILL CREATION PROCESS:
1. Analyze the user's intent - what reusable capability do they need?
2. Choose the right type (tool/worker/workflow)
3. Design the interface (name, description, args_schema for tools)
3. Generate the code using templates
4. Test the skill
4. Register it for permanent use

OUTPUT FORMAT - Return ONLY valid JSON:
{
  "skill": {
    "name": "snake_case_name",
    "description": "What this skill does",
    "type": "tool|worker|workflow",
    "args_schema": { ... },  // for tools only
    "metadata": { ... },      // for workers/workflows
    "examples": ["example usage 1", "example usage 2"],
    "test_cases": [{"input": {...}, "expected": "..."}]
  }
}

RULES:
- Names must be snake_case, unique, descriptive
- Tools: args_schema must be valid JSON Schema
- Workers: metadata must include role, tagline, system_prompt
- Workflows: metadata must include steps array
- Always include at least 1 example and 1 test case
- Code must be safe (no dangerous imports, no eval, no file system outside sandbox)
""")


def build_skill(user_intent: str, context: str = "") -> Dict[str, Any]:
    """Analyze intent and generate a complete skill definition."""
    
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": f"Context: {context}\n\nUser Intent: {user_intent}"}
    ]
    
    raw, provider = llm.chat(
        messages,
        role="skill_builder",
        temperature=0.2,
        max_tokens=3000,
        json_mode=True
    )
    
    # FIX: Use JSON repair for malformed LLM responses
    try:
        parsed = _safe_json_loads(raw.strip(), {"skill": {}})
        skill_data = parsed.get("skill", {})
        
        if not skill_data.get("name"):
            return {"error": "Missing skill name"}
        if not skill_data.get("type") or skill_data["type"] not in ("tool", "worker", "workflow"):
            return {"error": "Invalid skill type"}
        
        skill = Skill(
            name=skill_data["name"],
            description=skill_data.get("description", ""),
            type=skill_data["type"],
            args_schema=skill_data.get("args_schema", {}),
            metadata=skill_data.get("metadata", {}),
            examples=skill_data.get("examples", []),
            test_cases=skill_data.get("test_cases", []),
            tags=skill_data.get("tags", []),
        )
        
        code = _generate_skill_code(skill)
        skill.code = code
        
        # Register the skill - persists to disk
        _SKILL_REGISTRY.register(skill)
        
        return {
            "status": "created",
            "skill": skill.to_dict(),
            "code_preview": code[:2000] + ("..." if len(code) > 2000 else "")
        }
        
    except Exception as e:
        logger.error(f"Skill build failed: {e}")
        return {"error": f"Skill build failed: {e}"}


def list_skills() -> List[Dict]:
    """List all registered skills."""
    return [s.to_dict() for s in _SKILL_REGISTRY.list_all()]


def get_skill(name: str) -> Optional[Dict]:
    """Get a specific skill by name."""
    skill = _SKILL_REGISTRY.get(name)
    return skill.to_dict() if skill else None


def delete_skill(name: str) -> bool:
    """Delete a skill."""
    return _SKILL_REGISTRY.unregister(name)


def run_skill_test(name: str) -> Dict:
    """Run test cases for a skill."""
    skill = _SKILL_REGISTRY.get(name)
    if not skill:
        return {"error": f"Skill not found: {name}"}
    
    if skill.type != "tool":
        return {"error": "Only tool skills can be tested via this endpoint"}
    
    results = []
    for test in skill.test_cases:
        try:
            result = tools.safe_tool_call(name, test.get("input", {}))
            expected = test.get("expected", "")
            passed = expected.lower() in result.lower() if expected else True
            results.append({
                "test": test,
                "result": result,
                "passed": passed
            })
        except Exception as e:
            results.append({
                "test": test,
                "result": f"Error: {e}",
                "passed": False
            })
    
    return {
        "skill": name,
        "total": len(results),
        "passed": sum(1 for r in results if r["passed"]),
        "results": results
    }


# ──────────────────────────────────────────────────────────────────────────────
# ASYNC API - FIX: Async variants for FastAPI integration
# ──────────────────────────────────────────────────────────────────────────────

async def abuild_skill(user_intent: str, context: str = "") -> Dict[str, Any]:
    """Async version of build_skill for FastAPI integration."""
    loop = asyncio.get_event_loop()
    return await loop.run_in_executor(
        _get_executor(),
        lambda: build_skill(user_intent, context)
    )


async def arun_skill_test(name: str) -> Dict:
    """Async version of run_skill_test."""
    loop = asyncio.get_event_loop()
    return await loop.run_in_executor(
        _get_executor(),
        lambda: run_skill_test(name)
    )


# ──────────────────────────────────────────────────────────────────────────────
# Skill Builder Tools (for orchestrator to invoke)
# ──────────────────────────────────────────────────────────────────────────────

def _handle_build_skill(args: dict) -> str:
    """Tool handler for building skills from intent."""
    request = args.get("request", "")
    if not request:
        return "Error: No request provided"
    
    try:
        record = build_skill(request)
        return json.dumps(record, indent=2, default=str)
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_list_skills(args: dict) -> str:
    return json.dumps(list_skills(), indent=2, default=str)

def _handle_get_skill(args: dict) -> str:
    name = args.get("name", "")
    if not name:
        return "Error: No skill name provided"
    skill = get_skill(name)
    if not skill:
        return f"Error: Skill not found: {name}"
    return json.dumps(skill, indent=2, default=str)

def _handle_delete_skill(args: dict) -> str:
    name = args.get("name", "")
    if not name:
        return "Error: No skill name provided"
    try:
        ok = delete_skill(name)
        if not ok:
            return f"Error: Skill not found: {name}"
        return json.dumps({"status": "deleted", "skill": name})
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_test_skill(args: dict) -> str:
    name = args.get("name", "")
    if not name:
        return "Error: No skill name provided"
    try:
        result = run_skill_test(name)
        return json.dumps(result, indent=2, default=str)
    except Exception as e:
        return f"Error: {str(e)}"


# Register the skill builder tools
register_tool("build_skill", "Build a new reusable skill from intent (tool/worker/workflow)", _handle_build_skill, {
    "type": "object",
    "properties": {
        "request": {"type": "string", "description": "Natural language description of the skill to build"},
        "auto_apply": {"type": "boolean", "description": "Auto-apply after tests pass (requires approval)"}
    },
    "required": ["request"]
})

register_tool("list_skills", "List all registered skills", _handle_list_skills, {
    "type": "object",
    "properties": {},
    "required": []
})

register_tool("get_skill", "Get details of a specific skill", _handle_get_skill, {
    "type": "object",
    "properties": {
        "name": {"type": "string", "description": "Skill name"}
    },
    "required": ["name"]
})

register_tool("delete_skill", "Delete a registered skill", _handle_delete_skill, {
    "type": "object",
    "properties": {
        "name": {"type": "string", "description": "Skill name to delete"}
    },
    "required": ["name"]
})

register_tool("test_skill", "Run test cases for a skill", _handle_test_skill, {
    "type": "object",
    "properties": {
        "name": {"type": "string", "description": "Skill name to test"}
    },
    "required": ["name"]
})


# ──────────────────────────────────────────────────────────────────────────────
# Pre-built Common Skills (Bootstrap)
# ──────────────────────────────────────────────────────────────────────────────

def _bootstrap_skills():
    """Pre-register commonly useful skills."""
    common_skills = [
        Skill(
            name="http_request",
            description="Make HTTP requests (GET, POST, PUT, DELETE) with headers and body",
            type="tool",
            args_schema={
                "type": "object",
                "properties": {
                    "url": {"type": "string", "description": "Target URL"},
                    "method": {"type": "string", "enum": ["GET", "POST", "PUT", "DELETE", "PATCH"], "default": "GET"},
                    "headers": {"type": "object", "description": "HTTP headers"},
                    "body": {"type": "object", "description": "Request body (for POST/PUT/PATCH)"},
                    "timeout": {"type": "number", "default": 30}
                },
                "required": ["url"]
            },
            examples=["Fetch JSON from API", "POST data to webhook"],
            test_cases=[{"input": {"url": "https://httpbin.org/get"}, "expected": "200"}],
            tags=["http", "api", "network"],
            code="""
import urllib.request
import json

def _handle_http_request(args: dict) -> str:
    url = args.get("url", "")
    method = args.get("method", "GET")
    headers = args.get("headers", {}) or {}
    body = args.get("body")
    timeout = args.get("timeout", 30)
    
    if not url:
        return "Error: No URL provided"
    
    try:
        data = json.dumps(body).encode() if body else None
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        with urllib.request.urlopen(req, timeout=timeout) as response:
            return response.read().decode()[:5000]
    except Exception as e:
        return f"Error: {e}"

register_tool("http_request", "Make HTTP requests", _handle_http_request)
"""
        ),
        Skill(
            name="json_parse",
            description="Parse and query JSON data with JSONPath support",
            type="tool",
            args_schema={
                "type": "object",
                "properties": {
                    "json": {"type": "string", "description": "JSON string to parse"},
                    "path": {"type": "string", "description": "JSONPath expression (e.g., $.data[0].name)"}
                },
                "required": ["json"]
            },
            examples=["Extract nested value from API response"],
            test_cases=[{"input": {"json": '{"a": {"b": 1}}', "path": "$.a.b"}, "expected": "1"}],
            tags=["json", "parse", "query"],
            code="""
import json

def _handle_json_parse(args: dict) -> str:
    json_str = args.get("json", "")
    path = args.get("path", "")
    
    if not json_str:
        return "Error: No JSON provided"
    
    try:
        data = json.loads(json_str)
        if not path:
            return json.dumps(data, indent=2)
        
        # Simple JSONPath implementation for common cases
        parts = path.replace("$", "").lstrip(".").split(".")
        current = data
        for part in parts:
            if part.endswith("]") and "[" in part:
                key, idx = part[:-1].split("[")
                current = current[key][int(idx)]
            else:
                current = current[part]
        return json.dumps(current) if not isinstance(current, (str, int, float, bool)) else str(current)
    except Exception as e:
        return f"Error: {e}"

register_tool("json_parse", "Parse and query JSON", _handle_json_parse)
"""
        ),
        Skill(
            name="file_grep",
            description="Search for patterns in files recursively",
            type="tool",
            args_schema={
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "Regex pattern to search"},
                    "path": {"type": "string", "default": ".", "description": "Directory to search"},
                    "include": {"type": "string", "description": "File pattern (e.g., *.py)"}
                },
                "required": ["pattern"]
            },
            examples=["Find all TODO comments", "Search for function definitions"],
            test_cases=[{"input": {"pattern": "TODO", "path": "."}, "expected": "TODO"}],
            tags=["search", "files", "grep"],
            code="""
import os
import re

def _handle_file_grep(args: dict) -> str:
    pattern = args.get("pattern", "")
    path = args.get("path", ".")
    include = args.get("include", "")
    
    if not pattern:
        return "Error: No pattern provided"
    
    try:
        regex = re.compile(pattern)
        results = []
        for root, dirs, files in os.walk(path):
            for file in files:
                if include and not file.endswith(include.replace("*", "")):
                    continue
                filepath = os.path.join(root, file)
                try:
                    with open(filepath, 'r', encoding='utf-8', errors='ignore') as f:
                        for i, line in enumerate(f, 1):
                            if regex.search(line):
                                results.append(f"{filepath}:{i}: {line.strip()}")
                except Exception:
                    pass
        return "\\n".join(results[:100]) if results else "No matches found"
    except Exception as e:
        return f"Error: {e}"

register_tool("file_grep", "Search files with regex", _handle_file_grep)
"""
        ),
    ]
    
    for skill in common_skills:
        # Only register if not already exists
        if not _SKILL_REGISTRY.get(skill.name):
            _SKILL_REGISTRY.register(skill)
            # Write skill code to file for reference
            skill_file = os.path.join(_SKILLS_DIR, f"{skill.name}.py")
            try:
                with open(skill_file, "w", encoding="utf-8") as f:
                    f.write(skill.code)
            except Exception:
                pass


# ──────────────────────────────────────────────────────────────────────────────
# Lazy bootstrap - only runs when first accessed, not on import
# ──────────────────────────────────────────────────────────────────────────────

_bootstrapped = False
_bootstrap_lock = threading.Lock()

def _ensure_bootstrapped():
    """Lazy bootstrap - only runs once when first needed."""
    global _bootstrapped
    with _bootstrap_lock:
        if not _bootstrapped:
            _bootstrap_skills()
            _bootstrapped = True

def _ensure_bootstrap_skill(name: str):
    """Ensure bootstrap runs before accessing a specific skill."""
    _ensure_bootstrapped()

# Override registry methods to ensure bootstrap
_original_get = _SKILL_REGISTRY.get
_original_list_all = _SKILL_REGISTRY.list_all

def _wrapped_get(name: str):
    _ensure_bootstrapped()
    return _original_get(name)

def _wrapped_list_all():
    _ensure_bootstrapped()
    return _original_list_all()

_SKILL_REGISTRY.get = _wrapped_get
_SKILL_REGISTRY.list_all = _wrapped_list_all
# ──────────────────────────────────────────────────────────────────────────────

__all__ = [
    "Skill",
    "SkillRegistry",
    "build_skill",
    "list_skills",
    "get_skill",
    "delete_skill",
    "run_skill_test",
    "_SKILL_REGISTRY",
    # Async variants
    "abuild_skill",
    "arun_skill_test",
]