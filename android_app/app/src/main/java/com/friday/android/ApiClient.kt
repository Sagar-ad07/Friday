package com.friday.android

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * Talks to the Friday brain (run.py). All calls are best-effort and never throw
 * to the caller — they return null / empty on failure so the UI degrades calmly.
 */
class ApiClient(
    @Volatile var base: String,
    @Volatile var token: String = ""
) {
    private val json = "application/json".toMediaType()
    private val client = OkHttpClient.Builder()
        .connectTimeout(8, TimeUnit.SECONDS)
        .readTimeout(120, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .build()

    private fun url(path: String) = base.trimEnd('/') + path

    private fun req(path: String): Request.Builder {
        val b = Request.Builder().url(url(path))
        if (token.isNotBlank()) b.header("Authorization", "Bearer $token")
        return b
    }

    private fun post(path: String, body: JSONObject): JSONObject? = try {
        val r = client.newCall(
            req(path).post(body.toString().toRequestBody(json)).build()
        ).execute()
        r.use { if (it.isSuccessful) JSONObject(it.body?.string() ?: "{}") else null }
    } catch (e: Exception) { null }

    private fun get(path: String): JSONObject? = try {
        val r = client.newCall(req(path).get().build()).execute()
        r.use { if (it.isSuccessful) JSONObject(it.body?.string() ?: "{}") else null }
    } catch (e: Exception) { null }

    // ── health / presence ────────────────────────────────────────────────────
    fun status(): JSONObject? = get("/status")
    fun sessionLock(): JSONObject? = get("/session/lock")
    fun screenState(): JSONObject? = get("/screen/state")
    fun team(): JSONObject? = get("/team")
    fun greeting(): JSONObject? = get("/greeting")
    fun phoneAgentConfig(agentId: String): JSONObject? = get("/phone-agents/$agentId")

    // ── device control ────────────────────────────────────────────────────────
    fun register(deviceId: String, info: JSONObject): Boolean {
        val body = JSONObject().put("device_id", deviceId).put("info", info)
        return post("/device/register", body)?.optString("status") == "registered"
    }

    // ── Friday AUTONOMY: one-tap control of the live trading engines + eye ─────
    fun tradingStart(): JSONObject? = post("/trading/start", JSONObject())
    fun tradingStop(): JSONObject?  = post("/trading/stop", JSONObject())
    fun androidConnect(): JSONObject? = post("/devices/android/connect", JSONObject())

    fun pendingCommands(deviceId: String): JSONArray {
        val res = get("/device/$deviceId/commands") ?: return JSONArray()
        return res.optJSONArray("commands") ?: JSONArray()
    }

    fun postResult(deviceId: String, commandId: String, result: JSONObject) {
        val body = JSONObject().put("command_id", commandId).put("result", result)
        post("/device/$deviceId/result", body)
    }

    fun postScreenshot(deviceId: String, imageB64: String): String {
        val body = JSONObject().put("image_b64", imageB64)
        return post("/device/$deviceId/screenshot", body)?.optString("description") ?: ""
    }

    fun submitEye(device: String, imageB64: String, kind: String = "screen"): String {
        val body = JSONObject().put("device", device).put("kind", kind).put("image_b64", imageB64)
        return post("/eye/submit", body)?.optString("description") ?: ""
    }

    // ── voice (mic -> STT) ─────────────────────────────────────────────────────
    /** POST raw WAV bytes to /voice/transcribe; returns transcript or null. */
    fun transcribe(wav: okhttp3.RequestBody): String? {
        return try {
            val r = client.newCall(
                req("/voice/transcribe").post(wav).build()
            ).execute()
            r.use { resp ->
                if (!resp.isSuccessful) return@use null
                JSONObject(resp.body?.string() ?: "{}").optString("text").ifBlank { null }
            }
        } catch (e: Exception) { null }
    }

    // ── chat (SSE stream) ──────────────────────────────────────────────────────
    /**
     * Streams a chat turn. Calls [onEvent] for each SSE JSON object
     * (types: run_id, thought, step, final, audio, confirm, error, done).
     */
    fun chatStream(
        text: String,
        role: String,
        onEvent: (JSONObject) -> Unit
    ) {
        val body = JSONObject()
            .put("text", text).put("lang", "en").put("client_id", "android")
        if (role.isNotBlank()) body.put("role", role)
        try {
            val r = client.newCall(
                req("/command/stream").post(body.toString().toRequestBody(json)).build()
            ).execute()
            r.use { resp ->
                if (!resp.isSuccessful) { onEvent(errEvent("HTTP ${resp.code}")); return }
                val src = resp.body?.source() ?: return
                while (!src.exhausted()) {
                    val line = src.readUtf8Line() ?: break
                    if (line.startsWith("data: ")) {
                        val payload = line.substring(6)
                        try { onEvent(JSONObject(payload)) } catch (_: Exception) {}
                    }
                }
            }
        } catch (e: Exception) {
            onEvent(errEvent(e.message ?: "connection error"))
        }
    }

    private fun errEvent(msg: String) = JSONObject().put("type", "error").put("message", msg)
}
