"""
Friday Base - Nightly Consolidation Job
Clusters interaction patterns → extracts reusable skills → writes few-shot examples into worker prompts.
Runs automatically on schedule or manual trigger.
"""
import json
import logging
import os
import re
import threading
import time
from collections import Counter
from typing import Dict, List

from .config import config
from . import learning as _learning
from . import memory as _memory_mod
from . import llm

logger = logging.getLogger("Friday.Consolidation")

CONSOLIDATION_DIR = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "consolidation"
)
os.makedirs(CONSOLIDATION_DIR, exist_ok=True)

FEW_SHOT_PATH = os.path.join(CONSOLIDATION_DIR, "few_shots.json")
PATTERNS_PATH = os.path.join(CONSOLIDATION_DIR, "patterns.json")
SKILLS_PATH = os.path.join(CONSOLIDATION_DIR, "skills.json")

_last_run = 0
_lock = threading.Lock()


def _load_json(path: str, default):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception:
        return default


def _save_json(path: str, data):
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
    os.replace(tmp, path)


def _get_memory():
    from .memory import Memory
    return Memory(config.data_dir)


def _extract_interaction_clusters(min_interactions: int = 10) -> List[Dict]:
    """Cluster interactions by topic/tool pattern using keyword overlap."""
    engine = _learning.get_engine()
    interactions = engine.interaction_log
    if len(interactions) < min_interactions:
        return []

    # Group by tool used
    by_tool = {}
    for i in interactions:
        tool = i.get("tool", "chat")
        by_tool.setdefault(tool, []).append(i)

    clusters = []
    for tool, items in by_tool.items():
        if len(items) < 3:
            continue
        # Extract common keywords
        all_text = " ".join(i["input"] + " " + i["response"] for i in items)
        words = re.findall(r'\b[a-z]{4,}\b', all_text.lower())
        common = Counter(words).most_common(20)
        keywords = [w for w, c in common if c >= 3]
        
        if keywords:
            clusters.append({
                "tool": tool,
                "count": len(items),
                "keywords": keywords,
                "success_rate": sum(1 for i in items if i["success"]) / len(items),
                "avg_latency": sum(i["latency"] for i in items) / len(items),
                "sample_inputs": [i["input"][:100] for i in items[:3]],
            })
    return clusters


def _generate_few_shots(clusters: List[Dict]) -> Dict[str, List[Dict]]:
    """Generate few-shot examples for each worker from successful clusters."""
    few_shots = {
        "reasoner": [],
        "coder": [],
        "researcher": [],
        "companion": [],
    }
    
    for cluster in clusters:
        tool = cluster["tool"]
        if cluster["success_rate"] < 0.7:
            continue  # Only learn from successful patterns
            
        worker_map = {
            "run_code": "coder",
            "run_terminal": "coder", 
            "web_search": "researcher",
            "open_url": "researcher",
            "manage_files": "coder",
            "trading_backtest": "reasoner",
            "trading_analyze": "reasoner",
            "chat": "companion",
        }
        target_worker = worker_map.get(tool, "reasoner")
        
        # Create few-shot from best example
        for sample in cluster["sample_inputs"]:
            few_shots[target_worker].append({
                "input": sample,
                "tool": tool,
                "keywords": cluster["keywords"][:5],
                "confidence": cluster["success_rate"],
            })
    
    # Cap at 10 per worker
    for w in few_shots:
        few_shots[w] = few_shots[w][:10]
    
    return few_shots


def _inject_few_shots_into_prompts(few_shots: Dict[str, List[Dict]]):
    """Append few-shot examples to worker prompts for next session."""
    from . import prompts
    
    for worker, shots in few_shots.items():
        if not shots:
            continue
        prompt_key = f"WORKER_PROMPTS.{worker}"
        if hasattr(prompts, worker.upper() + "_PROMPT"):
            # We can't easily mutate imported module constants at runtime
            # Instead, save to file for prompt builder to use
            pass
    
    # Save to consolidation dir for prompt builder to pick up
    _save_json(FEW_SHOT_PATH, {
        "generated_at": time.time(),
        "few_shots": few_shots,
    })


def _extract_skills(clusters: List[Dict]) -> List[Dict]:
    """Extract reusable skill definitions from clusters."""
    skills = []
    for cluster in clusters:
        if cluster["count"] >= 5 and cluster["success_rate"] >= 0.8:
            skills.append({
                "name": f"skill_{cluster['tool']}_{cluster['keywords'][0] if cluster['keywords'] else 'general'}",
                "tool": cluster["tool"],
                "trigger_keywords": cluster["keywords"][:5],
                "confidence": cluster["success_rate"],
                "usage_count": cluster["count"],
                "description": f"Auto-learned: {cluster['tool']} for {', '.join(cluster['keywords'][:3])}",
            })
    return skills


def run_consolidation() -> Dict:
    """Main consolidation entry point. Call nightly or on demand."""
    global _last_run
    with _lock:
        _last_run = time.time()
    
    logger.info("Starting nightly consolidation...")
    
    # 1. Cluster interactions
    clusters = _extract_interaction_clusters()
    logger.info(f"Found {len(clusters)} interaction clusters")
    
    # 2. Generate few-shots
    few_shots = _generate_few_shots(clusters)
    total_shots = sum(len(v) for v in few_shots.values())
    logger.info(f"Generated {total_shots} few-shot examples")
    
    # 3. Extract skills
    skills = _extract_skills(clusters)
    logger.info(f"Extracted {len(skills)} reusable skills")
    
    # 4. Persist
    _save_json(PATTERNS_PATH, {
        "updated_at": time.time(),
        "clusters": clusters,
    })
    _save_json(FEW_SHOT_PATH, {
        "updated_at": time.time(),
        "few_shots": few_shots,
    })
    _save_json(SKILLS_PATH, {
        "updated_at": time.time(),
        "skills": skills,
    })
    
    # 5. Update learning engine skills
    engine = _learning.get_engine()
    for skill in skills:
        if skill["name"] not in engine.skills:
            from .learning import Skill
            engine.skills[skill["name"]] = Skill(
                name=skill["name"],
                category="auto",
                description=skill["description"],
            )
        engine.skills[skill["name"]].mastery = skill["confidence"]
    engine._save("skills", {k: v.to_dict() for k, v in engine.skills.items()})
    
    result = {
        "status": "completed",
        "clusters_found": len(clusters),
        "few_shots_generated": total_shots,
        "skills_extracted": len(skills),
        "timestamp": time.time(),
    }
    logger.info(f"Consolidation complete: {result}")
    return result


def get_consolidation_status() -> Dict:
    """Get status of last consolidation run."""
    patterns = _load_json(PATTERNS_PATH, {})
    few_shots = _load_json(FEW_SHOT_PATH, {})
    skills = _load_json(SKILLS_PATH, {})
    
    return {
        "last_consolidation": patterns.get("updated_at"),
        "clusters_count": len(patterns.get("clusters", [])),
        "few_shots": {k: len(v) for k, v in few_shots.get("few_shots", {}).items()},
        "skills_count": len(skills.get("skills", [])),
        "next_scheduled": _last_run + 86400 if _last_run else None,
    }


def schedule_nightly(interval_seconds: int = 86400):
    """Schedule consolidation to run every interval_seconds (default 24h)."""
    def runner():
        while True:
            time.sleep(interval_seconds)
            try:
                run_consolidation()
            except Exception as e:
                logger.error(f"Consolidation failed: {e}")
    
    t = threading.Thread(target=runner, daemon=True)
    t.start()
    logger.info(f"Scheduled nightly consolidation every {interval_seconds}s")


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    result = run_consolidation()
    print(json.dumps(result, indent=2))