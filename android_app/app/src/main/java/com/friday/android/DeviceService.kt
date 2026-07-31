package com.friday.android

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.first
import org.json.JSONObject

/**
 * Foreground service that keeps Friday's phone agent alive:
 *  - polls GET /device/{id}/commands every few seconds and executes them
 *  - hosts the (toggleable) screen-capture eye
 * Survives the app being backgrounded (that's the point of "live" mode).
 */
class DeviceService : Service() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var pollJob: Job? = null
    @Volatile private var eyeOn = false

    private lateinit var api: ApiClient
    private lateinit var deviceId: String
    private lateinit var settings: Settings

    override fun onCreate() {
        super.onCreate()
        settings = Settings(this)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIF_ID, buildNotification("Here."),
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            startForeground(NOTIF_ID, buildNotification("Here."))
        }
        scope.launch {
            api = ApiClient(settings.server.first(), settings.token.first())
            deviceId = settings.deviceId.first()
            eyeOn = settings.eyeOn.first()
            startPolling()
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_EYE_ON -> eyeOn = true
            ACTION_EYE_OFF -> eyeOn = false
        }
        return START_STICKY
    }

    private fun startPolling() {
        if (pollJob?.isActive == true) return
        pollJob = scope.launch {
            while (isActive) {
                try {
                    val cmds = api.pendingCommands(deviceId)
                    for (i in 0 until cmds.length()) {
                        val c = cmds.getJSONObject(i)
                        val id = c.optString("id")
                        val command = c.optJSONObject("command") ?: JSONObject()
                        val action = command.optString("action")
                        val args = command.optJSONObject("args") ?: JSONObject()
                        val result = CommandExecutor.execute(applicationContext, action, args)
                        api.postResult(deviceId, id, result)
                    }
                } catch (_: Exception) {}
                // Eye capture cadence handled by ScreenEye (started via MediaProjection
                // permission flow from the Activity). Here we just gate the flag.
                delay(POLL_MS)
            }
        }
    }

    private fun buildNotification(state: String): Notification {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val ch = NotificationChannel(CHANNEL, "Friday", NotificationManager.IMPORTANCE_LOW)
            (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)
                .createNotificationChannel(ch)
        }
        return NotificationCompat.Builder(this, CHANNEL)
            .setContentTitle("Friday")
            .setContentText(state)
            .setSmallIcon(R.mipmap.ic_launcher_foreground)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    override fun onBind(intent: Intent?): IBinder? = null
    override fun onDestroy() { scope.cancel(); super.onDestroy() }

    companion object {
        const val CHANNEL = "friday_service"
        const val NOTIF_ID = 1001
        const val POLL_MS = 3000L
        const val ACTION_EYE_ON = "com.friday.android.EYE_ON"
        const val ACTION_EYE_OFF = "com.friday.android.EYE_OFF"
    }
}
