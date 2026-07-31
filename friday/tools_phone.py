"""
Friday Base - Phone-specific tools (REAL dispatch path)

These register into the SAME tool registry as friday.tools so the agentic
loop can call them. When invoked they queue a command on the registered
phone device via DeviceController.send_command and return a structured
marker the orchestrator surfaces to the UI as a confirmation/result.

The native Android app polls GET /device/{id}/commands, executes the action
locally (AccessibilityService / Intent / SmsManager), then posts the result
to POST /device/{id}/result (echoing the command_id).

Only tools whose action the app actually knows are registered here. Intent
strings (sms:, mailto:, share://) are NOT executed locally on the phone — the
app handles the real action, so we queue a structured command instead.
"""
import logging

from .config import config
from . import tools

logger = logging.getLogger("Friday.PhoneTools")

# Which registered device_id do phone commands target? The app reports its
# id at registration. We target the most-recently-active phone device. If you
# run more than one phone, set FRIDAY_PHONE_DEVICE to a fixed id.
_DEFAULT_PHONE_DEVICE = None


def set_phone_device(device_id: str):
    global _DEFAULT_PHONE_DEVICE
    _DEFAULT_PHONE_DEVICE = device_id


def _target_device() -> str:
    if _DEFAULT_PHONE_DEVICE:
        return _DEFAULT_PHONE_DEVICE
    from .device_control import device_controller
    # Prefer the most-recently-active device; fall back to any known device.
    best, best_ts = None, -1.0
    for did, dev in device_controller.devices.items():
        ts = dev.get("last_seen", 0)
        if ts >= best_ts:
            best, best_ts = did, ts
    return best


def _queue(action: str, args: dict) -> str:
    """Queue a command on the target phone device. Returns a marker string."""
    from .device_control import device_controller
    target = _target_device()
    if not target:
        return ("Phone not connected. Open the Friday Android app and let it "
                "register, then ask again.")
    res = device_controller.send_command(target, {"action": action, "args": args})
    if res.get("status") == "queued":
        return (f"__PHONE_COMMAND_QUEUED__||{res.get('command_id')}||"
                f"{action}||{target}")
    return f"Failed to queue phone command: {res.get('error', 'unknown')}"


# ── Tool handlers (queue real phone actions) ────────────────────────────────

def tool_send_sms(args: dict) -> str:
    phone = args.get("phone", "")
    message = args.get("message", "")
    if not phone:
        return "Error: phone number required."
    return _queue("send_sms", {"phone": phone, "message": message})


def tool_send_email(args: dict) -> str:
    to = args.get("to", "")
    subject = args.get("subject", "")
    body = args.get("body", "")
    if not to:
        return "Error: recipient email required."
    return _queue("send_email", {"to": to, "subject": subject, "body": body})


def tool_open_url(args: dict) -> str:
    url = args.get("url", "")
    if not url:
        return "Error: URL required."
    return _queue("open_url", {"url": url})


def tool_open_app(args: dict) -> str:
    name = args.get("name", args.get("app", ""))
    if not name:
        return "Error: app name required."
    return _queue("open_app", {"name": name})


def tool_share_content(args: dict) -> str:
    text = args.get("text", "")
    url = args.get("url", "")
    title = args.get("title", "")
    return _queue("share", {"text": text, "url": url, "title": title})


def tool_web_search(args: dict) -> str:
    """Search the web (delegates to the standard web_search tool)."""
    return tools.get_tool_handler("web_search")({"query": args.get("query", "")})


def tool_take_photo(args: dict) -> str:
    return _queue("take_photo", {})


def tool_get_location(args: dict) -> str:
    return _queue("get_location", {})


def register():
    """Register phone-control tools into the shared registry.

    Phone SMS/email/share/open are treated as destructive-ish (they act on the
    real device / send real messages) so they pass through the confirmation
    gate when confirm_destructive is on. We mark them so the orchestrator can
    confirm before _queue runs on the real device.
    """
    tools.register(
        "phone_send_sms",
        "Queue an SMS to be sent from the connected Android phone. "
        "Requires a connected device + confirm_destructive approval.",
        {
            "type": "object",
            "properties": {
                "phone": {"type": "string", "description": "destination number"},
                "message": {"type": "string", "description": "message body"},
            },
            "required": ["phone", "message"],
        },
        tool_send_sms,
    )
    tools.register(
        "phone_send_email",
        "Open the email composer on the connected Android phone with the given "
        "recipient/subject/body.",
        {
            "type": "object",
            "properties": {
                "to": {"type": "string"},
                "subject": {"type": "string"},
                "body": {"type": "string"},
            },
            "required": ["to"],
        },
        tool_send_email,
    )
    tools.register(
        "phone_open_url",
        "Open a URL in the browser on the connected Android phone.",
        {
            "type": "object",
            "properties": {"url": {"type": "string", "description": "http(s) URL"}},
            "required": ["url"],
        },
        tool_open_url,
    )
    tools.register(
        "phone_open_app",
        "Launch an app on the connected Android phone by name.",
        {
            "type": "object",
            "properties": {"name": {"type": "string", "description": "app name"}},
            "required": ["name"],
        },
        tool_open_app,
    )
    tools.register(
        "phone_share",
        "Trigger the Android share sheet on the connected phone.",
        {
            "type": "object",
            "properties": {
                "text": {"type": "string"},
                "url": {"type": "string"},
                "title": {"type": "string"},
            },
            "required": [],
        },
        tool_share_content,
    )
    tools.register(
        "phone_take_photo",
        "Capture a photo using the connected phone's camera.",
        {"type": "object", "properties": {}},
        tool_take_photo,
    )
    tools.register(
        "phone_get_location",
        "Request the connected phone's current location.",
        {"type": "object", "properties": {}},
        tool_get_location,
    )
    tools.register(
        "phone_web_search",
        "Search the web (same as web_search).",
        {
            "type": "object",
            "properties": {"query": {"type": "string"}},
            "required": ["query"],
        },
        tool_web_search,
    )
    logger.info("Phone tools registered")
