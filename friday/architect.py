"""
Friday Base - Architect / Project Manager
Scans the codebase, plans multi-file changes, and coordinates workers.
"""
import os
import json
import logging
from typing import Dict, List

from . import llm, prompts, workers, tools
from .config import config

logger = logging.getLogger("Friday.Architect")

_PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def scan_codebase() -> Dict[str, str]:
    """Scan project structure and return file tree + key file contents."""
    tree = {}
    for root, dirs, files in os.walk(_PROJECT_ROOT):
        dirs[:] = [d for d in dirs if not d.startswith('.') and d != '__pycache__']
        rel_root = os.path.relpath(root, _PROJECT_ROOT)
        if rel_root == '.':
            rel_root = ''
        for f in files:
            if f.endswith('.py') or f.endswith('.html') or f.endswith('.js') or f.endswith('.css') or f.endswith('.toml') or f.endswith('.md'):
                path = os.path.join(rel_root, f) if rel_root else f
                full_path = os.path.join(root, f)
                try:
                    with open(full_path, 'r', encoding='utf-8', errors='replace') as fh:
                        content = fh.read()
                    if len(content) > 3000:
                        content = content[:3000] + "\n... (truncated)"
                    tree[path] = content
                except Exception:
                    tree[path] = ""
    return tree


def plan_project(goal: str) -> Dict:
    """Plan a multi-file project. Returns a project plan with file changes."""
    codebase = scan_codebase()
    codebase_summary = "\n".join(f"{p}: {len(c)} chars" for p, c in codebase.items())

    key_files = {}
    for path in ["run.py", "friday/config.py", "friday/voice.py", "friday/llm.py",
                 "friday/prompts.py", "friday/orchestrator.py", "friday/tools.py",
                 "friday/workers.py", "friday/memory.py", "friday/resilience.py",
                 "friday/upgrader.py", "friday/team.py", "interface/index.html"]:
        if path in codebase:
            key_files[path] = codebase[path][:2000]

    prompt = f"""You are Friday's Architect. You plan multi-file projects.

GOAL: {goal}

CODEBASE STRUCTURE:
{codebase_summary}

KEY FILE CONTENTS (truncated):
{json.dumps(key_files, indent=2)}

Create a project plan. Output ONLY valid JSON:
{{
  "project_name": "short name",
  "description": "what this project does",
  "steps": [
    {{
      "step": 1,
      "description": "what this step does",
      "files_to_modify": ["friday/xxx.py", "interface/index.html"],
      "builder_prompt": "detailed instructions for the builder worker"
    }}
  ],
  "validation_criteria": "how to verify the project worked"
}}

Rules:
- Break the goal into 2-5 concrete steps
- Each step modifies 1-3 files maximum
- Steps must be in dependency order (later steps can depend on earlier ones)
- Be specific about what changes in each file
- The validation criteria should be testable
"""

    messages = [
        {"role": "system", "content": prompts.ARCHITECT_PROMPT},
        {"role": "user", "content": prompt}
    ]

    try:
        raw, _ = llm.chat(messages, role="reasoner", temperature=0.3, max_tokens=4000, json_mode=True)
        plan = json.loads(raw)
        return plan
    except Exception as e:
        logger.error(f"Architect planning failed: {e}")
        return {"error": str(e), "steps": []}


def coordinate_workers(plan: Dict) -> Dict:
    """Execute a project plan step by step using workers."""
    results = []
    for step in plan.get("steps", []):
        step_result = {
            "step": step.get("step"),
            "description": step.get("description"),
            "status": "pending"
        }

        if step.get("files_to_modify"):
            task = f"""Project step: {step.get('description')}
Files to modify: {', '.join(step.get('files_to_modify', []))}
Instructions: {step.get('builder_prompt', '')}

Current codebase context:
{json.dumps({f: scan_codebase().get(f, '')[:500] for f in step.get('files_to_modify', [])}, indent=2)}"""

            try:
                result = workers.run_worker("coder", task)
                step_result["status"] = "completed"
                step_result["result"] = result[:500]
            except Exception as e:
                step_result["status"] = "failed"
                step_result["error"] = str(e)

        results.append(step_result)

    return {
        "project_name": plan.get("project_name"),
        "total_steps": len(plan.get("steps", [])),
        "completed_steps": sum(1 for r in results if r.get("status") == "completed"),
        "results": results
    }
