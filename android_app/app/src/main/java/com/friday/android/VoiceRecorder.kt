package com.friday.android

import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlin.coroutines.resume

/**
 * Records mic audio, encodes a 16-bit PCM WAV, and POSTs it to the brain's
 * STT endpoint (POST /voice/transcribe). Returns the transcript, or null.
 *
 * Supports two modes:
 *  - Single shot: record up to maxSeconds, stop, transcribe once.
 *  - Continuous conversation: record in a loop, auto-detect speech end via
 *    silence, then transcribe each utterance. Designed for background use so
 *    the user does NOT need to hold a button.
 */
object VoiceRecorder {

    private const val SAMPLE_RATE = 16000
    private const val CHANNEL = AudioFormat.CHANNEL_IN_MONO
    private const val ENCODING = AudioFormat.ENCODING_PCM_16BIT

    /**
     * Single-shot capture. Returns the transcript string, or null on failure.
     */
    suspend fun recordAndTranscribe(
        api: ApiClient,
        maxSeconds: Int = 8
    ): String? = withContext(Dispatchers.IO) {
        val minBuf = AudioRecord.getMinBufferSize(SAMPLE_RATE, CHANNEL, ENCODING)
        val bufSize = (minBuf * 2).coerceAtLeast(4096)
        val rec = try {
            AudioRecord(MediaRecorder.AudioSource.MIC, SAMPLE_RATE, CHANNEL, ENCODING, bufSize)
        } catch (e: SecurityException) {
            return@withContext null
        }
        if (rec.state != AudioRecord.STATE_INITIALIZED) return@withContext null

        val pcm = ByteArrayOutputStream()
        val chunk = ByteArray(bufSize)
        rec.startRecording()
        val started = System.currentTimeMillis()
        var spoke = false
        try {
            while (System.currentTimeMillis() - started < maxSeconds * 1000) {
                val n = rec.read(chunk, 0, chunk.size)
                if (n > 0) {
                    if (!spoke && hasSignal(chunk, n)) spoke = true
                    if (spoke) pcm.write(chunk, 0, n)
                }
            }
        } finally {
            try { rec.stop() } catch (_: Exception) {}
            try { rec.release() } catch (_: Exception) {}
        }
        val raw = pcm.toByteArray()
        if (raw.isEmpty()) return@withContext null

        val wav = encodeWav(raw, SAMPLE_RATE)
        val body = wav.toRequestBody("audio/wav".toMediaType())
        return@withContext api.transcribe(body)
    }

    /**
     * Continuous conversation mode. Keeps the mic open in the background,
     * segments speech by silence, and returns each transcript as it becomes
     * available. Designed to run inside a foreground service so the OS does
     * not kill it.
     *
     * [onUtterance] is invoked for each detected speech segment.
     * Returns when [shouldRun] becomes false or the coroutine is cancelled.
     */
    suspend fun continuousListen(
        api: ApiClient,
        onUtterance: (String) -> Unit,
        shouldRun: () -> Boolean,
        silenceMs: Int = 1200,
        maxUtteranceMs: Int = 15000
    ) = withContext(Dispatchers.IO) {
        val minBuf = AudioRecord.getMinBufferSize(SAMPLE_RATE, CHANNEL, ENCODING)
        val bufSize = (minBuf * 2).coerceAtLeast(4096)
        val rec = try {
            AudioRecord(MediaRecorder.AudioSource.VOICE_RECOGNITION, SAMPLE_RATE, CHANNEL, ENCODING, bufSize)
        } catch (e: SecurityException) {
            return@withContext
        }
        if (rec.state != AudioRecord.STATE_INITIALIZED) return@withContext

        rec.startRecording()
        val chunk = ByteArray(bufSize)
        val utterance = ByteArrayOutputStream()
        var lastVoiceTime = System.currentTimeMillis()
        var isSpeaking = false

        try {
            while (shouldRun()) {
                val n = rec.read(chunk, 0, chunk.size)
                if (n <= 0) continue
                val hasVoice = hasSignal(chunk, n)
                val now = System.currentTimeMillis()

                if (hasVoice) {
                    lastVoiceTime = now
                    if (!isSpeaking) {
                        isSpeaking = true
                        utterance.reset()
                    }
                    utterance.write(chunk, 0, n)
                } else if (isSpeaking) {
                    // Silence detected after speech
                    if (now - lastVoiceTime > silenceMs) {
                        isSpeaking = false
                        val raw = utterance.toByteArray()
                        if (raw.isNotEmpty()) {
                            val wav = encodeWav(raw, SAMPLE_RATE)
                            val body = wav.toRequestBody("audio/wav".toMediaType())
                            api.transcribe(body)?.let { txt ->
                                if (txt.isNotBlank()) onUtterance(txt)
                            }
                        }
                        utterance.reset()
                    } else if (now - lastVoiceTime > maxUtteranceMs) {
                        // Force-end long utterance
                        isSpeaking = false
                        val raw = utterance.toByteArray()
                        if (raw.isNotEmpty()) {
                            val wav = encodeWav(raw, SAMPLE_RATE)
                            val body = wav.toRequestBody("audio/wav".toMediaType())
                            api.transcribe(body)?.let { txt ->
                                if (txt.isNotBlank()) onUtterance(txt)
                            }
                        }
                        utterance.reset()
                    }
                }
            }
        } finally {
            try { rec.stop() } catch (_: Exception) {}
            try { rec.release() } catch (_: Exception) {}
        }
    }

    private fun hasSignal(b: ByteArray, n: Int): Boolean {
        var peak = 0
        var i = 0
        while (i < n - 1) {
            val s = (b[i].toInt() and 0xFF) or (b[i + 1].toInt() shl 8)
            val v = if (s > 32767) s - 65536 else s
            if (v < 0) v.ushr(0)
            val a = kotlin.math.abs(v)
            if (a > peak) peak = a
            i += 2
        }
        return peak > 800 // ~2.5% of full scale
    }

    private fun encodeWav(pcm: ByteArray, sampleRate: Int): ByteArray {
        val out = ByteArrayOutputStream()
        val channels = 1
        val bits = 16
        val dataLen = pcm.size
        val total = 44 + dataLen
        fun writeStr(s: String) = out.write(s.toByteArray(Charsets.US_ASCII))
        fun writeInt(v: Int) = out.write(ByteBuffer.allocate(4).order(ByteOrder.LITTLE_ENDIAN).putInt(v).array())
        fun writeShort(v: Short) = out.write(ByteBuffer.allocate(2).order(ByteOrder.LITTLE_ENDIAN).putShort(v).array())
        writeStr("RIFF"); writeInt(total - 8); writeStr("WAVE")
        writeStr("fmt "); writeInt(16); writeShort(1); writeShort(channels.toShort())
        writeInt(sampleRate); writeInt(sampleRate * channels * bits / 8)
        writeShort((channels * bits / 8).toShort()); writeShort(bits.toShort())
        writeStr("data"); writeInt(dataLen); out.write(pcm)
        return out.toByteArray()
    }
}
