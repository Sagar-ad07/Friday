package com.friday.android

import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableStateOf
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

data class ChatMsg(val fromUser: Boolean, val text: String)

data class Worker(val id: String, val name: String, val emoji: String, val color: String)

class FridayViewModel : ViewModel() {
    lateinit var api: ApiClient
    var deviceId: String = "android-001"
    var role: String = ""
    var agentName: String = "Friday"
    var agentVoice: String = "en-IN-NeerjaNeural"

    // Compose-observable state via MutableState delegates
    private val _presence: MutableState<Presence> = mutableStateOf(Presence.OFFLINE)
    var presence: Presence
        get() = _presence.value
        private set(value) { _presence.value = value }

    private val _messages: MutableState<List<ChatMsg>> = mutableStateOf(emptyList())
    var messages: List<ChatMsg>
        get() = _messages.value
        private set(value) { _messages.value = value }

    private val _workers: MutableState<List<Worker>> = mutableStateOf(emptyList())
    var workers: List<Worker>
        get() = _workers.value
        private set(value) { _workers.value = value }

    private val _sending: MutableState<Boolean> = mutableStateOf(false)
    var sending: Boolean
        get() = _sending.value
        private set(value) { _sending.value = value }

    private val _continuousMode: MutableState<Boolean> = mutableStateOf(false)
    var continuousMode: Boolean
        get() = _continuousMode.value
        private set(value) { _continuousMode.value = value }

    private val _avatarUri: MutableState<String> = mutableStateOf("")
    var avatarUri: String
        get() = _avatarUri.value
        private set(value) { _avatarUri.value = value }

    private val _textScale: MutableState<Float> = mutableStateOf(1.0f)
    var textScale: Float
        get() = _textScale.value
        private set(value) { _textScale.value = value }

    private var eyeOn = false
    fun setEyeOn(on: Boolean) { eyeOn = on }

    // FRIDAY AUTONOMY: one-tap control of the live trading engines + eye.
    private val _autonomyOn: MutableState<Boolean> = mutableStateOf(false)
    var autonomyOn: Boolean
        get() = _autonomyOn.value
        private set(value) { _autonomyOn.value = value }

    private val _autonomyStatus: MutableState<String> = mutableStateOf("idle")
    var autonomyStatus: String
        get() = _autonomyStatus.value
        private set(value) { _autonomyStatus.value = value }

    fun toggleAutonomy(on: Boolean) {
        if (!::api.isInitialized) return
        _autonomyOn.value = on
        _autonomyStatus.value = if (on) "arming…" else "disarming…"
        viewModelScope.launch(Dispatchers.IO) {
            try {
                if (on) {
                    api.tradingStart()
                    api.androidConnect()
                    _autonomyStatus.value = "live"
                } else {
                    api.tradingStop()
                    _autonomyStatus.value = "idle"
                }
            } catch (_: Exception) {
                _autonomyStatus.value = "error"
            }
        }
    }
    fun applyAvatarUri(uri: String) { avatarUri = uri }
    fun applyTextScale(scale: Float) { textScale = scale }
    fun applyContinuousMode(on: Boolean) {
        continuousMode = on
        if (on) startContinuousListening() else stopContinuousListening()
    }

    private var continuousJob: kotlinx.coroutines.Job? = null

    /** Begin the presence heartbeat + one-time greeting + team load. */
    fun start() {
        loadAgentConfig()
        loadTeam()
        greetOnce()
        viewModelScope.launch {
            while (true) {
                refreshPresence()
                delay(2500)
            }
        }
    }

    fun startContinuousMode() {
        if (continuousMode) return
        continuousMode = true
        startContinuousListening()
    }

    fun stopContinuousMode() {
        continuousMode = false
        stopContinuousListening()
    }

    fun startContinuousListening() {
        stopContinuousListening()
        continuousJob = viewModelScope.launch(Dispatchers.IO) {
            VoiceRecorder.continuousListen(
                api = api,
                onUtterance = { transcript ->
                    if (transcript.isNotBlank()) {
                        launch(Dispatchers.Main) {
                            messages = messages + ChatMsg(true, transcript)
                            send(transcript)
                        }
                    }
                },
                shouldRun = { continuousMode }
            )
        }
    }

    fun stopContinuousListening() {
        continuousJob?.cancel()
        continuousJob = null
    }

    private fun loadAgentConfig() = viewModelScope.launch {
        try {
            val cfg = withContext(Dispatchers.IO) { api.phoneAgentConfig(deviceId) }
            if (cfg != null) {
                agentName = cfg.optString("name", "Friday")
                agentVoice = cfg.optString("voice", "en-IN-NeerjaNeural")
                role = cfg.optString("role", "")
            }
        } catch (_: Exception) {}
    }

    private fun greetOnce() = viewModelScope.launch {
        val g = withContext(Dispatchers.IO) { api.greeting() }
        val t = g?.optString("text").orEmpty()
        if (t.isNotBlank()) messages = messages + ChatMsg(false, t)
    }

    private fun loadTeam() = viewModelScope.launch {
        val t = withContext(Dispatchers.IO) { api.team() } ?: return@launch
        val out = mutableListOf<Worker>()
        val self = t.optJSONObject("friday")
        if (self != null) out.add(worker(self))
        val members = t.optJSONArray("members")
        if (members != null) for (i in 0 until members.length())
            out.add(worker(members.getJSONObject(i)))
        if (out.isNotEmpty()) workers = out
    }

    private fun worker(o: JSONObject) = Worker(
        id = o.optString("id", o.optString("name", "?")),
        name = o.optString("name", o.optString("id", "?")),
        emoji = o.optString("emoji_face", o.optString("head_sign", "◈")),
        color = o.optString("color", "#7C8CFF"),
    )

    private suspend fun refreshPresence() = withContext(Dispatchers.IO) {
        if (sending) return@withContext
        val st = api.status()
        if (st == null) { presence = Presence.OFFLINE; return@withContext }
        val lock = api.sessionLock()
        val busyHolder = lock?.optString("holder").orEmpty()
        val busy = lock?.optBoolean("busy") ?: false
        val eyeActive = st.optBoolean("eye_active", false)
        presence = when {
            busy && busyHolder != "android" -> Presence.BUSY
            eyeOn && eyeActive -> Presence.WATCHING
            else -> Presence.HERE
        }
    }

    fun send(text: String) {
        if (text.isBlank() || sending) return
        messages = messages + ChatMsg(true, text)
        sending = true
        presence = Presence.THINKING
        messages = messages + ChatMsg(false, "…")
        viewModelScope.launch(Dispatchers.IO) {
            api.chatStream(text, role) { ev ->
                when (ev.optString("type")) {
                    "thought" -> presence = Presence.THINKING
                    "final" -> replaceLast(ev.optString("reply", ""))
                    "audio" -> presence = Presence.SPEAKING
                    "error" -> replaceLast("⚠ " + ev.optString("message", "error"))
                    "done" -> {
                        sending = false
                        presence = Presence.HERE
                    }
                }
            }
            sending = false
        }
    }

    private fun replaceLast(text: String) {
        if (text.isBlank()) return
        val list = messages.toMutableList()
        if (list.isNotEmpty() && !list.last().fromUser) list[list.size - 1] = ChatMsg(false, text)
        else list.add(ChatMsg(false, text))
        messages = list
    }
}
