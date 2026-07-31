package com.friday.android

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log

/**
 * Broadcast receiver for the KILL_SWITCH action.
 * When received, immediately stops all services and broadcasts kill signal.
 */
class KillSwitchReceiver : BroadcastReceiver() {

    companion object {
        const val ACTION_KILL_SWITCH = "com.friday.android.KILL_SWITCH"
    }

    override fun onReceive(context: Context, intent: Intent) {
        Log.w("Friday.KillSwitch", "KILL_SWITCH received - executing emergency kill sequence")

        // 1. Stop DeviceService (stops polling, eye capture, etc.)
        context.stopService(Intent(context, DeviceService::class.java).apply {
            action = "com.friday.android.KILL_SWITCH"
        })

        // 2. Kill the main app process (clean restart)
        // This forces a clean restart on next launch
        android.os.Process.killProcess(android.os.Process.myPid())
        System.exit(10)
    }
}