"""
Friday Relay — lightweight bridge for cross-network phone ↔ desktop sync.
Runs on Railway's free tier. Desktop keeps a WebSocket connection.
Phone sends HTTP requests through the relay.
"""
import asyncio
import json
import logging
import time
import uuid
from typing import Optional

logger = logging.getLogger("Friday.Relay")

# ── In-memory store of connected desktop instances ──
# {desktop_id: {"ws": WebSocket, "connected_at": float, "last_ping": float}}
_desktops = {}
_desktop_lock = asyncio.Lock()


async def handle_desktop_ws(websocket):
    """Desktop connects here via WebSocket and stays alive."""
    desktop_id = str(uuid.uuid4())[:8]
    connected_at = time.time()

    async with _desktop_lock:
        _desktops[desktop_id] = {
            "ws": websocket,
            "connected_at": connected_at,
            "last_ping": time.time(),
        }

    logger.info("Desktop connected: %s (total: %d)", desktop_id, len(_desktops))

    try:
        async for message in websocket.iter_text():
            data = json.loads(message)
            msg_type = data.get("type")

            if msg_type == "ping":
                async with _desktop_lock:
                    if desktop_id in _desktops:
                        _desktops[desktop_id]["last_ping"] = time.time()
                await websocket.send(json.dumps({"type": "pong"}))

            elif msg_type == "register":
                # Desktop tells us its LAN URL so the relay can forward
                lan_url = data.get("lan_url", "")
                async with _desktop_lock:
                    if desktop_id in _desktops:
                        _desktops[desktop_id]["lan_url"] = lan_url

            elif msg_type == "forward_response":
                # Response to a forwarded phone request
                request_id = data.get("request_id")
                response_data = data.get("data", {})
                async with _desktop_lock:
                    if desktop_id in _desktops:
                        _desktops[desktop_id].setdefault("pending_responses", {})
                        _desktops[desktop_id]["pending_responses"][request_id] = response_data

    except Exception as e:
        logger.warning("Desktop %s disconnected: %s", desktop_id, e)
    finally:
        async with _desktop_lock:
            _desktops.pop(desktop_id, None)
        logger.info("Desktop disconnected: %s (total: %d)", desktop_id, len(_desktops))


async def forward_to_desktop(desktop_id: str, request_data: dict) -> Optional[dict]:
    """Forward a phone request to the desktop and wait for response."""
    async with _desktop_lock:
        desktop = _desktops.get(desktop_id)
        if desktop is None:
            return None
        ws = desktop.get("ws")

    if ws is None:
        return None

    request_id = str(uuid.uuid4())
    try:
        await ws.send(json.dumps({
            "type": "forward_request",
            "request_id": request_id,
            "data": request_data,
        }))

        # Wait for response (with timeout)
        for _ in range(150):  # 15 second timeout
            await asyncio.sleep(0.1)
            async with _desktop_lock:
                desktop = _desktops.get(desktop_id)
                if desktop and request_id in desktop.get("pending_responses", {}):
                    response = desktop["pending_responses"].pop(request_id)
                    return response
    except Exception:
        pass
    return None


async def get_connected_desktops() -> list:
    """Return list of connected desktop IDs."""
    async with _desktop_lock:
        return [
            {
                "id": did,
                "connected_at": info.get("connected_at"),
                "last_ping": info.get("last_ping"),
                "lan_url": info.get("lan_url", ""),
                "alive": (time.time() - info.get("last_ping", 0)) < 30,
            }
            for did, info in _desktops.items()
        ]


def cleanup_stale():
    """Remove desktops that haven't pinged in 60 seconds."""
    import asyncio
    async def _clean():
        async with _desktop_lock:
            stale = [
                did for did, info in _desktops.items()
                if (time.time() - info.get("last_ping", 0)) > 60
            ]
            for did in stale:
                _desktops.pop(did, None)
            if stale:
                logger.info("Cleaned %d stale desktop(s)", len(stale))

    asyncio.create_task(_clean())
