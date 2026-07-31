"""
Friday Tools - Tool registry and execution
"""
import logging
import re
import json
import os
import subprocess
import time
import concurrent.futures
from typing import Dict, Any, Optional

logger = logging.getLogger("Friday.Tools")

# ── Go Bridge Integration ──
_go_bridge = None
def _get_go_bridge():
    """Lazy-load the Go bridge. Returns None if unavailable."""
    global _go_bridge
    if _go_bridge is not None:
        return _go_bridge
    try:
        from .bridge.go_bridge import get_go_bridge
        _go_bridge = get_go_bridge()
        logger.info("Go bridge loaded successfully")
    except Exception as e:
        logger.debug(f"Go bridge not available: {e}")
        _go_bridge = False
    return _go_bridge if _go_bridge is not False else None

# ── Tool Registry ──
TOOLS: Dict[str, Dict] = {}

def register_tool(name: str, description: str, handler, args_schema: dict = None):
    """Register a tool with its handler."""
    TOOLS[name] = {
        "name": name,
        "description": description,
        "handler": handler,
        "args_schema": args_schema or {}
    }

def get_tool_schemas() -> list:
    """Return tool schemas in the format expected by the orchestrator."""
    return get_all_tool_defs()

def get_all_tool_defs() -> list:
    """Return list of all tool definitions."""
    return [
        {
            "type": "function",
            "function": {
                "name": "run_terminal",
                "description": "Run a shell command in the sandbox",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "command": {"type": "string", "description": "Shell command to run"},
                        "timeout": {"type": "number", "description": "Timeout in seconds"}
                    },
                    "required": ["command"]
                }
            }
        },
        {
            "type": "function",
            "function": {
                "name": "run_code",
                "description": "Run Python code in the sandbox",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "code": {"type": "string", "description": "Python code to execute"},
                        "timeout": {"type": "number", "description": "Timeout in seconds"}
                    },
                    "required": ["code"]
                }
            }
        },
        {
            "type": "function",
            "function": {
                "name": "manage_files",
                "description": "Manage files (list, read, write, delete)",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "action": {"type": "string", "enum": ["list", "read", "write", "delete"]},
                        "path": {"type": "string", "description": "File path"},
                        "content": {"type": "string", "description": "Content for write"}
                    },
                    "required": ["action", "path"]
                }
            }
        },
        {
            "type": "function",
            "function": {
                "name": "web_search",
                "description": "Search the web for information",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {"type": "string", "description": "Search query"}
                    },
                    "required": ["query"]
                }
            }
        },
        {
            "type": "function",
            "function": {
                "name": "get_time",
                "description": "Get current time",
                "parameters": {"type": "object", "properties": {}, "required": []}
            }
        },
        {
            "type": "function",
            "function": {
                "name": "system_info",
                "description": "Get system information",
                "parameters": {"type": "object", "properties": {}, "required": []}
            }
        },
        {
            "type": "function",
            "function": {
                "name": "calc",
                "description": "Calculate a mathematical expression",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "expression": {"type": "string", "description": "Math expression to evaluate"}
                    },
                    "required": ["expression"]
                }
            }
        },
        {
            "type": "function",
            "function": {
                "name": "trading_start",
                "description": "Start the automated trading bot",
                "parameters": {"type": "object", "properties": {}, "required": []}
            }
        },
        {
            "type": "function",
            "function": {
                "name": "trading_stop",
                "description": "Stop the automated trading bot",
                "parameters": {"type": "object", "properties": {}, "required": []}
            }
        },
        {
            "type": "function",
            "function": {
                "name": "trading_status",
                "description": "Get current trading bot status and performance metrics",
                "parameters": {"type": "object", "properties": {}, "required": []}
            }
        },
        {
            "type": "function",
            "function": {
                "name": "plan_and_execute",
                "description": "Execute a complex multi-step goal as a DAG of steps with parallel execution",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "goal": {"type": "string", "description": "The complex goal to plan and execute"}
                    },
                    "required": ["goal"]
                }
            }
        },
        {
            "type": "function",
            "function": {
                "name": "execute_batch",
                "description": "Execute multiple independent tool calls in parallel. Use for independent operations that don't depend on each other's results.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "calls": {
                            "type": "array",
                            "description": "List of tool calls to execute in parallel",
                            "items": {
                                "type": "object",
                                "properties": {
                                    "name": {"type": "string", "description": "Tool name"},
                                    "args": {"type": "object", "description": "Tool arguments"}
                                },
                                "required": ["name", "args"]
                            }
                        }
                    },
                    "required": ["calls"]
                }
            }
        }
    ]

# ── Tool Handlers ──
def _handle_run_terminal(args: dict) -> str:
    """Run a terminal command."""
    command = args.get("command", "")
    timeout = args.get("timeout", 30)
    
    if not command:
        return "Error: No command provided"
    
    # Security check - deny dangerous commands
    dangerous_patterns = ["rm -rf", "mkfs", "dd if=", "format ", "shutdown", "reboot"]
    for pattern in dangerous_patterns:
        if pattern in command.lower():
            return f"Error: Command blocked for security: {pattern}"
    
    try:
        result = subprocess.run(
            command,
            shell=True,
            capture_output=True,
            text=True,
            timeout=timeout,
            cwd=os.getcwd()
        )
        output = result.stdout + result.stderr
        return output[:1000] if len(output) > 1000 else output
    except subprocess.TimeoutExpired:
        return f"Error: Command timed out after {timeout}s"
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_run_code(args: dict) -> str:
    """Run Python code."""
    code = args.get("code", "")
    timeout = args.get("timeout", 10)
    
    if not code:
        return "Error: No code provided"
    
    try:
        import io
        import sys
        from contextlib import redirect_stdout, redirect_stderr
        
        old_stdout = sys.stdout
        old_stderr = sys.stderr
        stdout_capture = io.StringIO()
        stderr_capture = io.StringIO()
        
        try:
            sys.stdout = stdout_capture
            sys.stderr = stderr_capture
            exec(compile(code, '<string>', 'exec'), {})
        finally:
            sys.stdout = old_stdout
            sys.stderr = old_stderr
        
        output = stdout_capture.getvalue() + stderr_capture.getvalue()
        return output[:2000] if len(output) > 2000 else output
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_manage_files(args: dict) -> str:
    """Handle file operations."""
    action = args.get("action", "list")
    path = args.get("path", ".")
    content = args.get("content", "")
    
    # Try Go bridge first for performance
    bridge = _get_go_bridge()
    if bridge:
        try:
            go_action = {
                "operation": action,
                "path": path,
                "content": content
            }
            return bridge.manage_files(go_action)
        except Exception as e:
            logger.debug(f"Go manage_files failed: {e}")
    
    try:
        if action == "list":
            files = os.listdir(path) if os.path.isdir(path) else []
            return "\n".join(files) if files else "No files found"
        elif action == "read":
            with open(path, 'r') as f:
                return f.read()[:2000]
        elif action == "write":
            with open(path, 'w') as f:
                f.write(content)
            return f"Wrote {path}"
        elif action == "delete":
            os.remove(path)
            return f"Deleted {path}"
        else:
            return f"Unknown action: {action}"
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_web_search(args: dict) -> str:
    """Search the web."""
    query = args.get("query", "")
    if not query:
        return "Error: No search query"
    
    try:
        import urllib.request
        import urllib.parse
        import json
        
        # Use DuckDuckGo API or Wikipedia
        url = f"https://en.wikipedia.org/api/rest_v1/page/summary/{urllib.parse.quote(query)}"
        req = urllib.request.Request(url, headers={'User-Agent': 'Friday/1.0'})
        with urllib.request.urlopen(req, timeout=10) as response:
            data = json.loads(response.read())
            return data.get('extract', 'No results found')[:500]
    except Exception as e:
        return f"Search error: {str(e)}"

def _handle_get_time(args: dict) -> str:
    """Get current time."""
    bridge = _get_go_bridge()
    if bridge:
        try:
            return bridge.get_time()
        except Exception as e:
            logger.debug(f"Go get_time failed: {e}")
    from datetime import datetime, timezone
    return f"Local: {datetime.now().isoformat()}\nUTC: {datetime.now(timezone.utc).isoformat()}"

def _handle_system_info(args: dict) -> str:
    """Get system info."""
    bridge = _get_go_bridge()
    if bridge:
        try:
            return bridge.system_info()
        except Exception as e:
            logger.debug(f"Go system_info failed: {e}")
    import platform
    import sys
    return f"OS: {platform.system()} {platform.release()}\nPython: {sys.version}\nPlatform: {platform.machine()}"

# ── Trading Tools (Go-backed) ──
def _handle_trading_start(args: dict) -> str:
    """Start the trading bot."""
    bridge = _get_go_bridge()
    if bridge:
        try:
            return bridge.trading_start()
        except Exception as e:
            return f"Error: {str(e)}"
    return "Go bridge not available"

def _handle_trading_stop(args: dict) -> str:
    """Stop the trading bot."""
    bridge = _get_go_bridge()
    if bridge:
        try:
            return bridge.trading_stop()
        except Exception as e:
            return f"Error: {str(e)}"
    return "Go bridge not available"

def _handle_trading_status(args: dict) -> str:
    """Get trading bot status."""
    bridge = _get_go_bridge()
    if bridge:
        try:
            return json.dumps(bridge.trading_status(), indent=2)
        except Exception as e:
            return f"Error: {str(e)}"
    return "Go bridge not available"

def _handle_calc(args: dict) -> str:
    """Calculate mathematical expression."""
    expression = args.get("expression", "")
    if not expression:
        return "Error: No expression provided"
    
    bridge = _get_go_bridge()
    if bridge:
        try:
            return bridge.calc(expression)
        except Exception as e:
            logger.debug(f"Go calc failed: {e}")
    
    # Fallback to Python eval (safe-ish for basic math)
    try:
        result = eval(expression, {"__builtins__": {}}, {})
        return str(result)
    except Exception as e:
        return f"Error: {str(e)}"

# ── Register default tools ──
register_tool("run_terminal", "Run shell command", _handle_run_terminal)
register_tool("run_code", "Run Python code", _handle_run_code)
register_tool("manage_files", "File operations", _handle_manage_files)
register_tool("web_search", "Search the web", _handle_web_search)
register_tool("get_time", "Get current time", _handle_get_time)
register_tool("system_info", "Get system info", _handle_system_info)
register_tool("trading_start", "Start the trading bot", _handle_trading_start)
register_tool("trading_stop", "Stop the trading bot", _handle_trading_stop)
register_tool("trading_status", "Get trading bot status", _handle_trading_status)
register_tool("calc", "Calculate mathematical expression", _handle_calc)

def _handle_execute_batch(args: dict) -> str:
    """Execute multiple tool calls in parallel."""
    calls = args.get("calls", [])
    if not calls:
        return "Error: No calls provided"
    
    if not isinstance(calls, list):
        return "Error: calls must be a list"
    
    # Validate each call
    for call in calls:
        if not isinstance(call, dict) or "name" not in call:
            return f"Error: Invalid call format: {call}"
    
    try:
        results = execute_batch(calls)
        return json.dumps(results, indent=2, default=str)
    except Exception as e:
        logger.error(f"execute_batch failed: {e}")
        return f"Error: {str(e)}"

register_tool("execute_batch", "Execute multiple independent tool calls in parallel", _handle_execute_batch)

# ── Tool Execution ──
def safe_tool_call(name: str, args: dict) -> str:
    """Safely call a tool by name."""
    if name not in TOOLS:
        return f"Unknown tool: {name}"
    
    if args is None:
        args = {}
    
    try:
        return TOOLS[name]["handler"](args)
    except Exception as e:
        logger.error(f"Tool {name} failed: {e}")
        return f"Error: {str(e)}"

def execute_batch(calls: list) -> list:
    """Execute multiple tool calls in parallel for independent operations.
    
    Args:
        calls: List of {"name": str, "args": dict} dicts
        
    Returns:
        List of results in same order
    """
    import concurrent.futures
    
    if not calls:
        return []
    
    results = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        future_to_idx = {}
        for idx, call in enumerate(calls):
            name = call.get("name", "")
            args = call.get("args", {}) or {}
            future = executor.submit(safe_tool_call, name, args)
            future_to_idx[future] = idx
        
        # Collect results in original order
        results = [None] * len(calls)
        for future in concurrent.futures.as_completed(future_to_idx):
            idx = future_to_idx[future]
            try:
                results[idx] = future.result(timeout=30)
            except Exception as e:
                logger.error(f"Batch tool call {idx} failed: {e}")
                results[idx] = f"Error: {str(e)}"
    
    return results

def execute_approved(action: str, args: dict) -> str:
    """Execute a tool after approval."""
    return safe_tool_call(action, args)

def is_confirmation_result(result: str) -> bool:
    """Check if result is a confirmation prompt."""
    if not result:
        return False
    return result.startswith("CONFIRM:") or result.startswith("APPROVE:")

def parse_confirmation(result: str) -> dict:
    """Parse a confirmation result."""
    if not result:
        return {}
    # Simple parsing - extract action and args from confirmation
    try:
        if result.startswith("CONFIRM:"):
            return {"action": "confirmed", "args": {}}
        return {}
    except Exception:
        return {}

def is_phone_command_result(result: str) -> bool:
    """Check if result is a phone command."""
    if not result:
        return False
    return result.startswith("PHONE:")

def parse_phone_command(result: str) -> dict:
    """Parse a phone command result."""
    if not result:
        return {}
    try:
        if result.startswith("PHONE:"):
            return {"command_id": "phone_cmd", "action": "phone", "target": result[6:]}
        return {}
    except Exception:
        return {}

# ── Planning Tool ──
def _handle_plan_and_execute(args: dict) -> str:
    """Execute a complex goal as a DAG of steps."""
    goal = args.get("goal", "")
    if not goal:
        return "Error: No goal provided"
    
    try:
        from .planning import plan_and_execute
        result = plan_and_execute(goal)
        return json.dumps(result, indent=2, default=str)
    except Exception as e:
        return f"Error: {str(e)}"

# ── Skill Builder Tool ──
def _handle_build_skill(args: dict) -> str:
    """Build a new skill on demand via Friday's self-upgrade engine."""
    request = args.get("request", "")
    auto_apply = args.get("auto_apply", False)
    
    if not request:
        return "Error: No skill request provided"
    
    try:
        from .skill_builder import build_skill
        record = build_skill(request, auto_apply=auto_apply)
        return json.dumps(record, indent=2, default=str)
    except Exception as e:
        return f"Error: {str(e)}"


# ── Register planning tool ──
register_tool("plan_and_execute", "Execute a complex goal as a DAG of steps", _handle_plan_and_execute)
register_tool("build_skill", "Build a new skill on demand via self-upgrade", _handle_build_skill, {
    "type": "object",
    "properties": {
        "request": {"type": "string", "description": "Natural language description of the skill to build"},
        "auto_apply": {"type": "boolean", "description": "Auto-apply after tests pass (requires approval)"}
    },
    "required": ["request"]
})

# Skill Builder tools
def _handle_build_skill(args: dict) -> str:
    """Build a new skill from intent via the skill builder."""
    request = args.get("request", "")
    auto_apply = args.get("auto_apply", False)
    if not request:
        return "Error: No request provided"
    try:
        from .skill_builder import build_skill
        record = build_skill(request, auto_apply=auto_apply)
        return json.dumps(record, indent=2, default=str)
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_list_skills(args: dict) -> str:
    """List all registered skills."""
    try:
        from .skill_builder import list_skills
        skills = list_skills()
        return json.dumps(skills, indent=2, default=str)
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_get_skill(args: dict) -> str:
    """Get details of a specific skill."""
    name = args.get("name", "")
    if not name:
        return "Error: No skill name provided"
    try:
        from .skill_builder import get_skill
        skill = get_skill(name)
        if not skill:
            return f"Error: Skill not found: {name}"
        return json.dumps(skill, indent=2, default=str)
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_delete_skill(args: dict) -> str:
    """Delete a registered skill."""
    name = args.get("name", "")
    if not name:
        return "Error: No skill name provided"
    try:
        from .skill_builder import delete_skill
        ok = delete_skill(name)
        if not ok:
            return f"Error: Skill not found: {name}"
        return json.dumps({"status": "deleted", "skill": name})
    except Exception as e:
        return f"Error: {str(e)}"

def _handle_test_skill(args: dict) -> str:
    """Run test cases for a skill."""
    name = args.get("name", "")
    if not name:
        return "Error: No skill name provided"
    try:
        from .skill_builder import run_skill_test
        result = run_skill_test(name)
        return json.dumps(result, indent=2, default=str)
    except Exception as e:
        return f"Error: {str(e)}"

# Register planning tool
register_tool("plan_and_execute", "Execute a complex goal as a DAG of steps", _handle_plan_and_execute)
register_tool("build_skill", "Build a new skill on demand via self-upgrade", _handle_build_skill, {
    "type": "object",
    "properties": {
        "request": {"type": "string", "description": "Natural language description of the skill to build"},
        "auto_apply": {"type": "boolean", "description": "Auto-apply after tests pass (requires approval)"}
    },
    "required": ["request"]
})

# Skill Builder tools
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

# ── Initialize ──
if __name__ == "__main__":
    print("Friday Tools initialized")
    print(f"Available tools: {list(TOOLS.keys())}")

# Re-export skill builder functions for easy access
from .skill_builder import (
    Skill,
    SkillRegistry,
    build_skill,
    list_skills,
    get_skill,
    delete_skill,
    run_skill_test,
    _SKILL_REGISTRY,
)