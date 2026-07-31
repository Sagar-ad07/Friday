package com.friday.android

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.telephony.SmsManager
import org.json.JSONObject

/**
 * Executes a single command from Friday on the phone and returns a result string.
 * All actions are real device actions. SMS uses SmsManager (real send).
 */
object CommandExecutor {

    fun execute(ctx: Context, action: String, args: JSONObject): JSONObject {
        val out = JSONObject().put("status", "completed")
        try {
            when (action) {
                "tap" -> {
                    val ok = FridayAccessibilityService.instance?.tap(
                        args.optDouble("x", 0.0).toFloat(),
                        args.optDouble("y", 0.0).toFloat()
                    ) ?: false
                    out.put("output", if (ok) "tapped" else "accessibility not enabled")
                }
                "swipe" -> {
                    val ok = FridayAccessibilityService.instance?.swipe(
                        args.optDouble("x1").toFloat(), args.optDouble("y1").toFloat(),
                        args.optDouble("x2").toFloat(), args.optDouble("y2").toFloat()
                    ) ?: false
                    out.put("output", if (ok) "swiped" else "accessibility not enabled")
                }
                "type" -> {
                    val ok = FridayAccessibilityService.instance?.typeText(args.optString("text")) ?: false
                    out.put("output", if (ok) "typed" else "no focused field / accessibility off")
                }
                "back" -> { FridayAccessibilityService.instance?.pressBack(); out.put("output", "back") }
                "home" -> { FridayAccessibilityService.instance?.pressHome(); out.put("output", "home") }
                "open_app" -> out.put("output", openApp(ctx, args))
                "open_url" -> {
                    val url = args.optString("url")
                    ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    out.put("output", "opened $url")
                }
                "send_sms" -> out.put("output", sendSms(ctx, args))
                "screenshot" -> out.put("output", "screenshot handled by eye service")
                "kill_switch" -> {
                    val confirm = args.optString("confirm", "")
                    if (confirm != "KILL") {
                        out.put("status", "failed")
                        out.put("output", "kill_switch requires confirm=KILL")
                    } else {
                        val intent = Intent(ctx, KillSwitchActivity::class.java)
                        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                        ctx.startActivity(intent)
                        out.put("output", "kill_switch_triggered")
                    }
                }
                else -> { out.put("status", "failed"); out.put("output", "unknown action: $action") }
            }
        } catch (e: Exception) {
            out.put("status", "failed").put("output", "error: ${e.message}")
        }
        return out
    }

    private fun openApp(ctx: Context, args: JSONObject): String {
        val pkg = args.optString("package")
        val name = args.optString("app").lowercase()
        val pm = ctx.packageManager
        // Try explicit package, else a small alias map, else search installed apps.
        val target = when {
            pkg.isNotBlank() -> pkg
            name.contains("youtube") -> "com.google.android.youtube"
            name.contains("whatsapp") -> "com.whatsapp"
            name.contains("chrome") -> "com.android.chrome"
            name.contains("maps") -> "com.google.android.apps.maps"
            name.contains("gmail") -> "com.google.android.gm"
            name.contains("camera") -> "com.android.camera"
            name.contains("settings") -> "com.android.settings"
            else -> pm.getInstalledApplications(0).firstOrNull {
                pm.getApplicationLabel(it).toString().lowercase().contains(name)
            }?.packageName
        } ?: return "app not found: $name"
        val launch = pm.getLaunchIntentForPackage(target)
            ?: return "cannot launch $target"
        ctx.startActivity(launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        return "opened $target"
    }

    private fun sendSms(ctx: Context, args: JSONObject): String {
        val phone = args.optString("phone")
        val msg = args.optString("message")
        if (phone.isBlank()) return "no phone number"
        @Suppress("DEPRECATION")
        val sms = if (android.os.Build.VERSION.SDK_INT >= 31)
            ctx.getSystemService(SmsManager::class.java)
        else SmsManager.getDefault()
        sms.sendTextMessage(phone, null, msg, null, null)
        return "SMS sent to $phone"
    }
}
