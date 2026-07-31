"""
Friday Team Parallel Execution Engine
Enables all 9 workers to operate simultaneously on different tasks
"""
import asyncio
import concurrent.futures
import threading
import time
from typing import Dict, List, Any, Callable
from dataclasses import dataclass
from enum import Enum
import json

from .team import TEAM, FRIDAY

class WorkerStatus(Enum):
    IDLE = "idle"
    BUSY = "working"
    COMPLETED = "done"
    ERROR = "error"

@dataclass
class Task:
    id: str
    description: str
    worker_id: str
    priority: int = 5
    data: Dict[str, Any] = None
    result: Any = None
    error: str = None
    created_at: float = None
    completed_at: float = None
    
    def __post_init__(self):
        if self.created_at is None:
            self.created_at = time.time()

@dataclass
class Worker:
    id: str
    name: str
    role: str
    executor: Callable = None
    status: WorkerStatus = WorkerStatus.IDLE
    current_task: Task = None
    completed_tasks: List[Task] = None
    
    def __post_init__(self):
        if self.completed_tasks is None:
            self.completed_tasks = []

class ParallelTeamManager:
    """
    Manages parallel execution across all 9 Friday workers
    """
    
    def __init__(self, max_workers: int = 9):
        self.max_workers = max_workers
        self.workers: Dict[str, Worker] = {}
        self.task_queue: List[Task] = []
        self.completed_tasks: List[Task] = []
        self.running = False
        self.executor = concurrent.futures.ThreadPoolExecutor(max_workers=max_workers)
        self.lock = threading.Lock()
        
        # Initialize all workers from team.py
        self._initialize_workers()
    
    def _initialize_workers(self):
        """Initialize all 9 workers from team.py"""
        team_members = TEAM + [FRIDAY]
        
        # Define worker executors based on roles
        role_executors = {
            "router": self._router_executor,
            "reasoner": self._reasoner_executor,
            "coder": self._coder_executor,
            "researcher": self._researcher_executor,
            "judge": self._judge_executor,
            "verifier": self._verifier_executor,
            "planner": self._planner_executor,
            "builder": self._builder_executor,
            "reviewer": self._reviewer_executor,
            "friday": self._friday_executor,
        }
        
        for member in team_members:
            worker_id = member["id"]
            executor = role_executors.get(member["role"])
            
            self.workers[worker_id] = Worker(
                id=worker_id,
                name=member["name"],
                role=member["role"],
                executor=executor,
                status=WorkerStatus.IDLE
            )
    
    def submit_task(self, task: Task) -> str:
        """Submit a task for parallel execution"""
        with self.lock:
            self.task_queue.append(task)
            print(f"[TEAM] Task submitted: {task.id} for {task.worker_id}")
        
        # Start processing if not already running
        if not self.running:
            self._start_processing()
        
        return task.id
    
    def submit_parallel_tasks(self, tasks: List[Task]) -> List[str]:
        """Submit multiple tasks for parallel execution"""
        task_ids = []
        with self.lock:
            for task in tasks:
                self.task_queue.append(task)
                task_ids.append(task.id)
                print(f"[TEAM] Task submitted: {task.id} for {task.worker_id}")
        
        if not self.running:
            self._start_processing()
        
        return task_ids
    
    def _start_processing(self):
        """Start processing tasks in parallel"""
        self.running = True
        
        def process_tasks():
            while self.task_queue or any(w.status == WorkerStatus.BUSY for w in self.workers.values()):
                self._process_queue()
                time.sleep(0.1)  # Small delay to prevent CPU spinning
            
            self.running = False
            print("[TEAM] All tasks completed")
        
        # Start processing in background thread
        threading.Thread(target=process_tasks, daemon=True).start()
    
    def _process_queue(self):
        """Process queued tasks, assigning to available workers"""
        with self.lock:
            # Find idle workers
            idle_workers = [w for w in self.workers.values() if w.status == WorkerStatus.IDLE]
            
            # Assign tasks to idle workers
            for worker in idle_workers:
                if not self.task_queue:
                    break
                
                # Find task for this worker
                task = next((t for t in self.task_queue if t.worker_id == worker.id), None)
                if not task:
                    # Try to find any task if worker can handle it
                    task = self.task_queue.pop(0) if self.task_queue else None
                
                if task:
                    self.task_queue.remove(task)
                    self._assign_task(worker, task)
    
    def _assign_task(self, worker: Worker, task: Task):
        """Assign task to worker and execute in parallel"""
        worker.status = WorkerStatus.BUSY
        worker.current_task = task
        
        def task_completed(future):
            try:
                result = future.result()
                task.result = result
                task.completed_at = time.time()
                
                with self.lock:
                    worker.status = WorkerStatus.COMPLETED
                    worker.current_task = None
                    worker.completed_tasks.append(task)
                    self.completed_tasks.append(task)
                    
                    print(f"[TEAM] {worker.name} completed task: {task.id}")
                    
            except Exception as e:
                task.error = str(e)
                task.completed_at = time.time()
                
                with self.lock:
                    worker.status = WorkerStatus.ERROR
                    worker.current_task = None
                    self.completed_tasks.append(task)
                    
                    print(f"[TEAM] {worker.name} error on task {task.id}: {e}")
        
        # Submit task to thread pool
        if worker.executor:
            future = self.executor.submit(worker.executor, task.data)
            future.add_done_callback(task_completed)
        else:
            # Default executor
            future = self.executor.submit(self._default_executor, task.data)
            future.add_done_callback(task_completed)
    
    # Worker-specific executors
    def _router_executor(self, data):
        """Vayu - Route tasks to appropriate workers"""
        time.sleep(0.5)  # Simulate work
        return {"routed_to": data.get("target", "appropriate_worker")}
    
    def _reasoner_executor(self, data):
        """Neo - Analyze and plan"""
        time.sleep(1.0)
        return {"analysis": "completed", "plan": "generated"}
    
    def _coder_executor(self, data):
        """Forge - Write and execute code"""
        time.sleep(0.8)
        return {"code_written": True, "executed": True}
    
    def _researcher_executor(self, data):
        """Scout - Research information"""
        time.sleep(1.2)
        return {"research_completed": True, "sources": 3}
    
    def _judge_executor(self, data):
        """Verdict - Evaluate and decide"""
        time.sleep(0.6)
        return {"decision": "approved", "confidence": 0.92}
    
    def _verifier_executor(self, data):
        """Prism - Verify correctness"""
        time.sleep(0.7)
        return {"verified": True, "issues_found": 0}
    
    def _planner_executor(self, data):
        """Oracle - Plan upgrades"""
        time.sleep(1.5)
        return {"upgrade_plan": "generated", "steps": 5}
    
    def _builder_executor(self, data):
        """Titan - Build upgrades"""
        time.sleep(1.3)
        return {"built": True, "tests_passed": True}
    
    def _reviewer_executor(self, data):
        """Sentinel - Review for safety"""
        time.sleep(0.9)
        return {"reviewed": True, "safe": True, "risks": 0}
    
    def _friday_executor(self, data):
        """Friday - Coordinate everything"""
        time.sleep(0.4)
        return {"coordinated": True, "team_status": "working"}
    
    def _default_executor(self, data):
        """Default executor for any worker"""
        time.sleep(0.5)
        return {"executed": True}
    
    def get_status(self) -> Dict[str, Any]:
        """Get current team status"""
        with self.lock:
            return {
                "running": self.running,
                "queued_tasks": len(self.task_queue),
                "busy_workers": sum(1 for w in self.workers.values() if w.status == WorkerStatus.BUSY),
                "idle_workers": sum(1 for w in self.workers.values() if w.status == WorkerStatus.IDLE),
                "completed_tasks": len(self.completed_tasks),
                "workers": {
                    w.id: {
                        "name": w.name,
                        "status": w.status.value,
                        "current_task": w.current_task.id if w.current_task else None,
                        "completed_count": len(w.completed_tasks)
                    }
                    for w in self.workers.values()
                }
            }
    
    def wait_for_completion(self, timeout: float = 30.0) -> bool:
        """Wait for all tasks to complete"""
        start = time.time()
        while time.time() - start < timeout:
            if not self.running and not self.task_queue:
                return True
            time.sleep(0.1)
        return False
    
    def shutdown(self):
        """Shutdown the team manager"""
        self.executor.shutdown(wait=True)

# Singleton instance
team_manager = ParallelTeamManager()

def execute_parallel_tasks(tasks: List[Dict]) -> Dict[str, Any]:
    """
    High-level function to execute multiple tasks in parallel using the team
    """
    # Convert task dicts to Task objects
    task_objects = []
    for i, task_data in enumerate(tasks):
        task_objects.append(Task(
            id=f"task_{i}_{int(time.time())}",
            description=task_data.get("description", "Task"),
            worker_id=task_data.get("worker_id", "friday"),
            priority=task_data.get("priority", 5),
            data=task_data.get("data", {})
        ))
    
    # Submit all tasks
    task_ids = team_manager.submit_parallel_tasks(task_objects)
    
    # Wait for completion
    success = team_manager.wait_for_completion()
    
    # Get results
    results = []
    for task_id in task_ids:
        # Find completed task
        completed = next((t for t in team_manager.completed_tasks if t.id == task_id), None)
        if completed:
            results.append({
                "task_id": completed.id,
                "worker": completed.worker_id,
                "result": completed.result,
                "error": completed.error,
                "duration": completed.completed_at - completed.created_at if completed.completed_at else None
            })
    
    return {
        "success": success,
        "results": results,
        "status": team_manager.get_status()
    }

def quick_parallel_execution(task_types: List[str]) -> Dict[str, Any]:
    """
    Quick parallel execution for common task types
    """
    # Map task types to workers
    task_mapping = {
        "analyze": "reasoner",
        "code": "coder",
        "research": "researcher",
        "verify": "verifier",
        "judge": "judge",
        "plan": "planner",
        "build": "builder",
        "review": "reviewer",
        "coordinate": "friday"
    }
    
    tasks = []
    for task_type in task_types:
        worker_id = task_mapping.get(task_type, "friday")
        tasks.append({
            "worker_id": worker_id,
            "description": f"{task_type} task",
            "data": {"type": task_type},
            "priority": 1 if task_type in ["analyze", "code"] else 5
        })
    
    return execute_parallel_tasks(tasks)