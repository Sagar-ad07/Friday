package com.friday.android

import android.content.Context
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.floatPreferencesKey
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

private val Context.dataStore by preferencesDataStore(name = "friday_settings")

/** Persistent user settings (server, token, feature toggles, avatar, text size, etc.). */
class Settings(private val ctx: Context) {
    companion object {
        val SERVER = stringPreferencesKey("server")
        val TOKEN = stringPreferencesKey("token")
        val DEVICE_ID = stringPreferencesKey("device_id")
        val VOICE = booleanPreferencesKey("voice_on")
        val AUTO_SPEAK = booleanPreferencesKey("auto_speak")
        val EYE = booleanPreferencesKey("eye_on")
        val REDUCE_MOTION = booleanPreferencesKey("reduce_motion")
        val ALLOW_ACTIONS = booleanPreferencesKey("allow_actions")
        val ONBOARDED = booleanPreferencesKey("onboarded")
        val ROLE = stringPreferencesKey("role")
        val AGENT_NAME = stringPreferencesKey("agent_name")
        val AGENT_VOICE = stringPreferencesKey("agent_voice")
        val AVATAR_URI = stringPreferencesKey("avatar_uri")
        val TEXT_SIZE_SCALE = floatPreferencesKey("text_size_scale")
        val CONTINUOUS_MODE = booleanPreferencesKey("continuous_mode")
        val WAKE_WORD = stringPreferencesKey("wake_word")
        val LISTENING_TIMEOUT = intPreferencesKey("listening_timeout")
    }

    val server: Flow<String> = ctx.dataStore.data.map { it[SERVER] ?: "http://localhost:8000" }
    val token: Flow<String> = ctx.dataStore.data.map { it[TOKEN] ?: "" }
    val deviceId: Flow<String> = ctx.dataStore.data.map { it[DEVICE_ID] ?: "android-001" }
    val voiceOn: Flow<Boolean> = ctx.dataStore.data.map { it[VOICE] ?: true }
    val autoSpeak: Flow<Boolean> = ctx.dataStore.data.map { it[AUTO_SPEAK] ?: true }
    val eyeOn: Flow<Boolean> = ctx.dataStore.data.map { it[EYE] ?: false }
    val reduceMotion: Flow<Boolean> = ctx.dataStore.data.map { it[REDUCE_MOTION] ?: false }
    val allowActions: Flow<Boolean> = ctx.dataStore.data.map { it[ALLOW_ACTIONS] ?: false }
    val onboarded: Flow<Boolean> = ctx.dataStore.data.map { it[ONBOARDED] ?: false }
    val role: Flow<String> = ctx.dataStore.data.map { it[ROLE] ?: "" }
    val agentName: Flow<String> = ctx.dataStore.data.map { it[AGENT_NAME] ?: "Friday" }
    val agentVoice: Flow<String> = ctx.dataStore.data.map { it[AGENT_VOICE] ?: "en-IN-NeerjaNeural" }
    val avatarUri: Flow<String> = ctx.dataStore.data.map { it[AVATAR_URI] ?: "" }
    val textSizeScale: Flow<Float> = ctx.dataStore.data.map { it[TEXT_SIZE_SCALE] ?: 1.0f }
    val continuousMode: Flow<Boolean> = ctx.dataStore.data.map { it[CONTINUOUS_MODE] ?: false }
    val wakeWord: Flow<String> = ctx.dataStore.data.map { it[WAKE_WORD] ?: "friday" }
    val listeningTimeout: Flow<Int> = ctx.dataStore.data.map { it[LISTENING_TIMEOUT] ?: 8 }

    suspend fun set(key: androidx.datastore.preferences.core.Preferences.Key<String>, v: String) =
        ctx.dataStore.edit { it[key] = v }

    suspend fun set(key: androidx.datastore.preferences.core.Preferences.Key<Boolean>, v: Boolean) =
        ctx.dataStore.edit { it[key] = v }

    suspend fun set(key: androidx.datastore.preferences.core.Preferences.Key<Float>, v: Float) =
        ctx.dataStore.edit { it[key] = v }

    suspend fun set(key: androidx.datastore.preferences.core.Preferences.Key<Int>, v: Int) =
        ctx.dataStore.edit { it[key] = v }
}
