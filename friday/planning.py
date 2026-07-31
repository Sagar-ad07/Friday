"""
Friday Base - DAG-Based Planning and Execution Engine

Transforms user goals into a Directed Acyclic Graph (DAG) of dependent steps,
executes them with parallel fan-out where possible, and provides checkpointing
for crash recovery.

Key concepts:
  - PlanNode: A single step in the plan (tool call, sub-agent, or checkpoint)
  - PlanGraph: The full DAG with dependency edges
  - ExecutionEngine: Runs the DAG with parallel execution and checkpointing
  - PlanCheckpoint: Serializable state for crash recovery

Example plan for "Build a trading dashboard":
  1. Research trading APIs (Scout) -> needs: none
  2. Design dashboard layout (Forge) -> needs: 1
  3. Implement frontend (Forge) -> needs: 2
  4. Set up backend API (Forge) -> needs: 1
  5. Deploy to Railway (Neo) -> needs: 3, 4
  6. Monitor P&L (Vayu) -> needs: 5
"""
import json
import logging
import os
import time
import uuid
from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, List, Optional, Set, Any, Callable
from concurrent.futures import ThreadPoolExecutor, as_completed

from .config import config

logger = logging.getLogger("Friday.Planning")


class NodeStatus(Enum):
    PENDING = "pending"
    READY = "ready"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"
    SKIPPED = "skipped"


class NodeType(Enum):
    TOOL = "tool"
    WORKER = "worker"
    SUB_AGENT = "sub_agent"
    CHECKPOINT = "checkpoint"
    VERIFY = "verify"


@dataclass
class PlanNode:
    """A single node in the execution DAG."""
    id: str
    description: str
    node_type: NodeType
    worker: str = "reasoner"
    tool: str = ""
    args: dict = field(default_factory=dict)
    dependencies: List[str] = field(default_factory=list)
    status: NodeStatus = NodeStatus.PENDING
    result: str = ""
    error: str = ""
    created_at: float = field(default_factory=time.time)
    started_at: float = 0.0
    completed_at: float = 0.0
    priority: int = 5
    retry_count: int = 0
    max_retries: int = 2

    def to_dict(self) -> dict:
        return {
            "id": self.id,
            "description": self.description,
            "node_type": self.node_type.value,
            "worker": self.worker,
            "tool": self.tool,
            "args": self.args,
            "dependencies": self.dependencies,
            "status": self.status.value,
            "result": self.result,
            "error": self.error,
            "created_at": self.created_at,
            "started_at": self.started_at,
            "completed_at": self.completed_at,
            "priority": self.priority,
            "retry_count": self.retry_count,
            "max_retries": self.max_retries,
        }

    @classmethod
    def from_dict(cls, d: dict) -> "PlanNode":
        return cls(
            id=d["id"],
            description=d["description"],
            node_type=NodeType(d["node_type"]),
            worker=d.get("worker", "reasoner"),
            tool=d.get("tool", ""),
            args=d.get("args", {}),
            dependencies=d.get("dependencies", []),
            status=NodeStatus(d.get("status", "pending")),
            result=d.get("result", ""),
            error=d.get("error", ""),
            created_at=d.get("created_at", time.time()),
            started_at=d.get("started_at", 0.0),
            completed_at=d.get("completed_at", 0.0),
            priority=d.get("priority", 5),
            retry_count=d.get("retry_count", 0),
            max_retries=d.get("max_retries", 2),
        )


@dataclass
class PlanGraph:
    """The full execution DAG."""
    id: str
    user_input: str
    nodes: Dict[str, PlanNode] = field(default_factory=dict)
    created_at: float = field(default_factory=time.time)
    updated_at: float = field(default_factory=time.time)
    context: str = ""
    final_answer: str = ""

    def add_node(self, node: PlanNode) -> None:
        self.nodes[node.id] = node
        self.updated_at = time.time()

    def get_ready_nodes(self) -> List[PlanNode]:
        """Get nodes whose dependencies are all completed."""
        ready = []
        for node in self.nodes.values():
            if node.status != NodeStatus.PENDING:
                continue
            deps_done = all(
                self.nodes.get(dep, PlanNode(dep, "", NodeType.TOOL)).status == NodeStatus.COMPLETED
                for dep in node.dependencies
            )
            if deps_done:
                node.status = NodeStatus.READY
                ready.append(node)
        return sorted(ready, key=lambda n: -n.priority)

    def get_completed_results(self) -> Dict[str, str]:
        """Get results from all completed nodes."""
        return {n.id: n.result for n in self.nodes.values() if n.status == NodeStatus.COMPLETED}

    def to_dict(self) -> dict:
        return {
            "id": self.id,
            "user_input": self.user_input,
            "nodes": {k: v.to_dict() for k, v in self.nodes.items()},
            "created_at": self.created_at,
            "updated_at": self.updated_at,
            "context": self.context,
            "final_answer": self.final_answer,
        }

    @classmethod
    def from_dict(cls, d: dict) -> "PlanGraph":
        return cls(
            id=d["id"],
            user_input=d["user_input"],
            nodes={k: PlanNode.from_dict(v) for k, v in d.get("nodes", {}).items()},
            created_at=d.get("created_at", time.time()),
            updated_at=d.get("updated_at", time.time()),
            context=d.get("context", ""),
            final_answer=d.get("final_answer", ""),
        )


class ExecutionEngine:
    """Executes a PlanGraph with parallel fan-out and checkpointing."""

    def __init__(self, max_workers: int = 4):
        self.max_workers = max_workers
        self._checkpoint_dir = os.path.join(config.data_dir, "plan_checkpoints")
        os.makedirs(self._checkpoint_dir, exist_ok=True)
        self._executor = ThreadPoolExecutor(max_workers=max_workers, thread_name_prefix="PlanExec")

    def _node_executor(self, node: PlanNode, graph: PlanGraph) -> PlanNode:
        """Execute a single node."""
        node.status = NodeStatus.RUNNING
        node.started_at = time.time()

        try:
            if node.node_type == NodeType.TOOL:
                from . import tools
                args = dict(node.args)
                # Inject dependency results
                for dep_id in node.dependencies:
                    dep = graph.nodes.get(dep_id)
                    if dep and dep.status == NodeStatus.COMPLETED:
                        args[f"_dep_{dep_id}"] = dep.result

                result = tools.safe_tool_call(node.tool, args)
                node.result = result
                node.status = NodeStatus.COMPLETED

            elif node.node_type == NodeType.WORKER:
                from . import workers
                dep_results = graph.get_completed_results()
                context = graph.context
                for dep_id, dep_result in dep_results.items():
                    context += f"\n[Step {dep_id} result]: {dep_result}"

                result = workers.run_worker(
                    node.worker,
                    node.description,
                    context,
                    "",
                    fallback=""
                )
                node.result = result
                node.status = NodeStatus.COMPLETED

            elif node.node_type == NodeType.SUB_AGENT:
                from .orchestrator import agentic_run
                dep_results = graph.get_completed_results()
                sub_text = node.description
                for dep_id, dep_result in dep_results.items():
                    sub_text += f"\n\nPrevious result [{dep_id}]: {dep_result}"

                result = agentic_run(sub_text, max_steps=3)
                node.result = result.get("reply", "")
                node.status = NodeStatus.COMPLETED

            elif node.node_type == NodeType.VERIFY:
                from .orchestrator import _verify
                dep_results = graph.get_completed_results()
                combined = "\n\n".join(dep_results.values())
                node.result = _verify(graph.user_input, combined, "en")
                node.status = NodeStatus.COMPLETED

            elif node.node_type == NodeType.CHECKPOINT:
                self._save_checkpoint(graph)
                node.result = f"Checkpoint saved at {time.time()}"
                node.status = NodeStatus.COMPLETED

        except Exception as e:
            node.error = str(e)
            node.status = NodeStatus.FAILED
            logger.error("Node %s failed: %s", node.id, e)

        node.completed_at = time.time()
        return node

    def _save_checkpoint(self, graph: PlanGraph) -> None:
        """Save graph state for crash recovery."""
        path = os.path.join(self._checkpoint_dir, f"{graph.id}.json")
        with open(path, "w") as f:
            json.dump(graph.to_dict(), f, indent=2)
        logger.debug("Checkpoint saved: %s", path)

    def _load_checkpoint(self, graph_id: str) -> Optional[PlanGraph]:
        """Load a saved graph state."""
        path = os.path.join(self._checkpoint_dir, f"{graph_id}.json")
        if not os.path.exists(path):
            return None
        try:
            with open(path, "r") as f:
                data = json.load(f)
            return PlanGraph.from_dict(data)
        except Exception as e:
            logger.error("Failed to load checkpoint %s: %s", graph_id, e)
            return None

    def execute(self, graph: PlanGraph, checkpoint_interval: int = 3) -> PlanGraph:
        """Execute the full DAG with parallel fan-out."""
        logger.info("Starting execution of plan %s with %d nodes", graph.id, len(graph.nodes))

        completed_count = 0
        pending_nodes = set(graph.nodes.keys())

        while pending_nodes:
            # Get ready nodes
            ready = graph.get_ready_nodes()
            if not ready:
                # Check if remaining nodes are all failed/skipped
                remaining = [graph.nodes[nid] for nid in pending_nodes]
                if all(n.status in (NodeStatus.FAILED, NodeStatus.SKIPPED) for n in remaining):
                    logger.warning("All remaining nodes failed/skipped")
                    break
                # Wait a bit and retry
                time.sleep(0.5)
                continue

            # Execute ready nodes in parallel
            futures = {}
            for node in ready:
                if node.status == NodeStatus.READY:
                    future = self._executor.submit(self._node_executor, node, graph)
                    futures[future] = node
                    pending_nodes.discard(node.id)

            # Wait for completion
            for future in as_completed(futures):
                node = futures[future]
                try:
                    completed_node = future.result(timeout=60)
                    completed_count += 1

                    # Checkpoint periodically
                    if completed_count % checkpoint_interval == 0:
                        self._save_checkpoint(graph)

                    # Handle failures
                    if completed_node.status == NodeStatus.FAILED:
                        if completed_node.retry_count < completed_node.max_retries:
                            completed_node.retry_count += 1
                            completed_node.status = NodeStatus.PENDING
                            pending_nodes.add(completed_node.id)
                            logger.info("Retrying node %s (attempt %d)",
                                        completed_node.id, completed_node.retry_count)
                        else:
                            # Mark dependents as skipped
                            for other in graph.nodes.values():
                                if completed_node.id in other.dependencies:
                                    other.status = NodeStatus.SKIPPED
                                    pending_nodes.discard(other.id)

                except Exception as e:
                    logger.error("Execution error for node %s: %s", node.id, e)
                    node.status = NodeStatus.FAILED
                    node.error = str(e)

        # Final checkpoint
        self._save_checkpoint(graph)

        # Generate final answer
        completed_results = graph.get_completed_results()
        if completed_results:
            from .orchestrator import _verify
            combined = "\n\n".join(completed_results.values())
            graph.final_answer = _verify(graph.user_input, combined, "en")

        logger.info("Plan %s execution complete. Final answer length: %d",
                    graph.id, len(graph.final_answer))
        return graph

    def resume(self, graph_id: str) -> Optional[PlanGraph]:
        """Resume execution from a checkpoint."""
        graph = self._load_checkpoint(graph_id)
        if graph is None:
            return None
        logger.info("Resuming plan %s from checkpoint", graph_id)
        return self.execute(graph)


# ── Plan Generator (uses LLM to create the DAG) ──
def generate_plan(user_input: str, context: str = "", max_nodes: int = 10) -> PlanGraph:
    """Generate a DAG plan from user input using the reasoner."""
    from . import llm
    from . import tools

    graph = PlanGraph(
        id=f"plan_{int(time.time())}_{uuid.uuid4().hex[:8]}",
        user_input=user_input,
        context=context
    )

    # Get tool schemas for the planner
    tool_schemas = json.dumps(tools.get_tool_schemas(), ensure_ascii=False)

    planner_prompt = f"""
You are Friday's Planner. Decompose the user's goal into a DAG of steps.

Goal: {user_input}
Context: {context}

Available tools:
{tool_schemas}

Output JSON with this exact format:
{{"nodes": [
  {{"id": "step_1", "description": "What to do", "node_type": "tool|worker|sub_agent|checkpoint|verify",
    "worker": "reasoner|coder|researcher|companion", "tool": "tool_name", "args": {{}},
    "dependencies": [], "priority": 5}}
]}}

Rules:
- Each node must have a unique id (step_1, step_2, etc.)
- dependencies lists node ids that must complete first
- For tool nodes: specify tool name and args
- For worker nodes: specify which worker (reasoner, coder, researcher, companion)
- For sub_agent nodes: the description is the sub-task to execute
- Use checkpoint nodes periodically for crash recovery
- Use verify node at the end to synthesize final answer
- Max {max_nodes} nodes
- Think about what can run in parallel (no dependencies between them)
"""

    try:
        raw, _ = llm.chat(
            [
                {"role": "system", "content": planner_prompt},
                {"role": "user", "content": f"Goal: {user_input}\nContext: {context}"}
            ],
            role="reasoner",
            temperature=0.3,
            max_tokens=2000
        )

        plan_data = json.loads(raw.strip())
        for node_data in plan_data.get("nodes", []):
            node = PlanNode(
                id=node_data["id"],
                description=node_data["description"],
                node_type=NodeType(node_data["node_type"]),
                worker=node_data.get("worker", "reasoner"),
                tool=node_data.get("tool", ""),
                args=node_data.get("args", {}),
                dependencies=node_data.get("dependencies", []),
                priority=node_data.get("priority", 5)
            )
            graph.add_node(node)

        if not graph.nodes:
            # Fallback: simple linear plan
            graph.add_node(PlanNode(
                id="step_1",
                description=user_input,
                node_type=NodeType.SUB_AGENT,
                worker="reasoner"
            ))
            graph.add_node(PlanNode(
                id="step_2",
                description="Verify and synthesize final answer",
                node_type=NodeType.VERIFY,
                dependencies=["step_1"]
            ))

    except Exception as e:
        logger.error("Plan generation failed: %s", e)
        # Fallback: simple linear plan
        graph.add_node(PlanNode(
            id="step_1",
            description=user_input,
            node_type=NodeType.SUB_AGENT,
            worker="reasoner"
        ))
        graph.add_node(PlanNode(
            id="step_2",
            description="Verify and synthesize final answer",
            node_type=NodeType.VERIFY,
            dependencies=["step_1"]
        ))

    return graph


# ── Convenience function ──
def plan_and_execute(user_input: str, context: str = "", max_nodes: int = 10) -> dict:
    """Generate a plan, execute it, and return results."""
    engine = ExecutionEngine()
    graph = generate_plan(user_input, context, max_nodes)
    result = engine.execute(graph)

    return {
        "plan_id": result.id,
        "user_input": result.user_input,
        "nodes": [n.to_dict() for n in result.nodes.values()],
        "final_answer": result.final_answer,
        "execution_time": time.time() - result.created_at,
    }


# ── Module-level singleton ──
_engine: Optional[ExecutionEngine] = None

def get_engine() -> ExecutionEngine:
    global _engine
    if _engine is None:
        _engine = ExecutionEngine()
    return _engine
