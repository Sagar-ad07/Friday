package com.friday.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.provider.Settings as AndroidSettings
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.compose.runtime.*
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

class MainActivity : ComponentActivity() {

    private lateinit var settings: Settings
    private val permLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        installSplashScreen()
        super.onCreate(savedInstanceState)
        settings = Settings(this)
        requestBasicPermissions()

        setContent {
            FridayTheme {
                val vm: FridayViewModel = viewModel()
                var route by remember { mutableStateOf("loading") }
                var server by remember { mutableStateOf("http://192.168.1.168:8000") }
                var token by remember { mutableStateOf("") }
                var deviceId by remember { mutableStateOf("android-001") }
                var voice by remember { mutableStateOf(true) }
                var autoSpeak by remember { mutableStateOf(true) }
                var eye by remember { mutableStateOf(false) }
                var reduceMotion by remember { mutableStateOf(false) }
                var allowActions by remember { mutableStateOf(false) }

                // Load persisted settings once.
                LaunchedEffect(Unit) {
                    server = settings.server.first()
                    token = settings.token.first()
                    deviceId = settings.deviceId.first()
                    voice = settings.voiceOn.first()
                    autoSpeak = settings.autoSpeak.first()
                    eye = settings.eyeOn.first()
                    reduceMotion = settings.reduceMotion.first()
                    allowActions = settings.allowActions.first()
                    val agentName = settings.agentName.first()
                    val agentVoice = settings.agentVoice.first()
                    val avatarUri = settings.avatarUri.first()
                    val textSizeScale = settings.textSizeScale.first()
                    val continuousMode = settings.continuousMode.first()
                    val onboarded = settings.onboarded.first()
                    vm.api = ApiClient(server, token)
                    vm.deviceId = deviceId
                    vm.role = settings.role.first()
                    vm.agentName = agentName
                    vm.agentVoice = agentVoice
                    vm.applyAvatarUri(avatarUri)
                    vm.applyTextScale(textSizeScale)
                    vm.applyContinuousMode(continuousMode)
                    vm.setEyeOn(eye)
                    route = if (onboarded) "main" else "onboarding"
                    if (onboarded) {
                        registerAndStart(vm, deviceId)
                        vm.start()
                    }
                }

                when (route) {
                    "onboarding" -> OnboardingScreen(
                        initialServer = server,
                        onTest = { addr, cb ->
                            lifecycleScope.launch {
                                val ok = withContext(Dispatchers.IO) {
                                    ApiClient(addr, token).status() != null
                                }
                                cb(ok)
                            }
                        },
                        onScan = {
                            lifecycleScope.launch {
                                val found = withContext(Dispatchers.IO) { scanForFriday() }
                                if (found != null) {
                                    server = found
                                }
                            }
                        },
                        onEnableAccessibility = { openAccessibility() },
                        onDone = { addr ->
                            lifecycleScope.launch {
                                settings.set(Settings.SERVER, addr)
                                settings.set(Settings.ONBOARDED, true)
                                server = addr
                                vm.api = ApiClient(addr, token)
                                registerAndStart(vm, deviceId)
                                vm.start()
                                route = "main"
                            }
                        }
                    )
                    "main" -> FridayScreen(
                        vm = vm, eyeOn = eye, voiceOn = voice,
                        onToggleEye = { on ->
                            eye = on; vm.setEyeOn(on)
                            lifecycleScope.launch { settings.set(Settings.EYE, on) }
                            toggleEyeService(on)
                        },
                        onToggleVoice = { on ->
                            voice = on
                            lifecycleScope.launch { settings.set(Settings.VOICE, on) }
                        },
                        onMic = {
                            if (!voice) return@FridayScreen
                            lifecycleScope.launch {
                                val text = withContext(Dispatchers.IO) {
                                    VoiceRecorder.recordAndTranscribe(vm.api)
                                }
                                if (!text.isNullOrBlank()) vm.send(text.trim())
                            }
                        },
                        onOpenSettings = { route = "settings" },
                        onSelectRole = { r ->
                            vm.role = r
                            lifecycleScope.launch { settings.set(Settings.ROLE, r) }
                        }
                    )
                    "settings" -> SettingsScreen(
                        server = server, token = token,
                        voice = voice, autoSpeak = autoSpeak, eye = eye,
                        reduceMotion = reduceMotion, allowActions = allowActions,
                        agentName = vm.agentName, agentVoice = vm.agentVoice,
                        avatarUri = vm.avatarUri, textSizeScale = vm.textScale,
                        continuousMode = vm.continuousMode,
                        wakeWord = "", listeningTimeout = 8,
                        onSave = { s2, tk2, v2, au2, e2, rm2, act2, name2, voice2, avatar2, scale2, continuous2, wake2, timeout2 ->
                            lifecycleScope.launch {
                                settings.set(Settings.SERVER, s2)
                                settings.set(Settings.TOKEN, tk2)
                                settings.set(Settings.VOICE, v2)
                                settings.set(Settings.AUTO_SPEAK, au2)
                                settings.set(Settings.EYE, e2)
                                settings.set(Settings.REDUCE_MOTION, rm2)
                                settings.set(Settings.ALLOW_ACTIONS, act2)
                                settings.set(Settings.AGENT_NAME, name2)
                                settings.set(Settings.AGENT_VOICE, voice2)
                                settings.set(Settings.AVATAR_URI, avatar2)
                                settings.set(Settings.TEXT_SIZE_SCALE, scale2)
                                settings.set(Settings.CONTINUOUS_MODE, continuous2)
                                settings.set(Settings.WAKE_WORD, wake2)
                                settings.set(Settings.LISTENING_TIMEOUT, timeout2)
                                server = s2; token = tk2; voice = v2; autoSpeak = au2
                                eye = e2; reduceMotion = rm2; allowActions = act2
                                vm.api.base = s2; vm.api.token = tk2
                                vm.agentName = name2
                                vm.agentVoice = voice2
                                vm.applyAvatarUri(avatar2)
                                vm.applyTextScale(scale2)
                                vm.applyContinuousMode(continuous2)
                                vm.setEyeOn(e2)
                                toggleEyeService(e2)
                            }
                        },
                        onEnableAccessibility = { openAccessibility() },
                        onBack = { route = "main" }
                    )
                }
            }
        }
    }

    private fun registerAndStart(vm: FridayViewModel, deviceId: String) {
        lifecycleScope.launch(Dispatchers.IO) {
            val info = JSONObject()
                .put("name", Build.MODEL).put("platform", "android")
                .put("version", Build.VERSION.RELEASE)
            vm.api.register(deviceId, info)
        }
        // Start the foreground service (command poller). Eye capture is armed by toggle.
        val i = Intent(this, DeviceService::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(i) else startService(i)
    }

    private fun toggleEyeService(on: Boolean) {
        val i = Intent(this, DeviceService::class.java)
            .setAction(if (on) DeviceService.ACTION_EYE_ON else DeviceService.ACTION_EYE_OFF)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(i) else startService(i)
    }

    private fun requestBasicPermissions() {
        val perms = mutableListOf<String>()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU)
            perms.add(Manifest.permission.POST_NOTIFICATIONS)
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED)
            perms.add(Manifest.permission.RECORD_AUDIO)
        if (perms.isNotEmpty()) permLauncher.launch(perms.toTypedArray())
    }

    private fun openAccessibility() {
        startActivity(Intent(AndroidSettings.ACTION_ACCESSIBILITY_SETTINGS))
    }

    private fun getLocalSubnet(): Pair<String, Int>? {
        try {
            val ifaces = java.net.NetworkInterface.getNetworkInterfaces()
            for (iface in ifaces) {
                if (iface.isLoopback || !iface.isUp) continue
                for (addr in iface.inetAddresses) {
                    if (addr is java.net.Inet4Address && !addr.isLoopbackAddress) {
                        val ip = addr.hostAddress ?: continue
                        val parts = ip.split(".")
                        if (parts.size == 4) {
                            val last = parts[3].toIntOrNull() ?: 0
                            return parts.subList(0, 3).joinToString(".") to last
                        }
                    }
                }
            }
        } catch (_: Exception) {}
        return null
    }

    private suspend fun scanForFriday(): String? = withContext(Dispatchers.IO) {
        val subnetInfo = getLocalSubnet() ?: return@withContext null
        val subnet = subnetInfo.first
        val phoneLast = subnetInfo.second

        val candidates = mutableSetOf<String>()
        candidates.add("$subnet.1")
        candidates.add("$subnet.100")
        candidates.add("$subnet.101")
        candidates.add("$subnet.102")
        candidates.add("$subnet.254")
        for (i in maxOf(2, phoneLast - 5)..minOf(50, phoneLast + 5)) {
            candidates.add("$subnet.$i")
        }

        val client = okhttp3.OkHttpClient.Builder()
            .connectTimeout(2, java.util.concurrent.TimeUnit.SECONDS)
            .readTimeout(2, java.util.concurrent.TimeUnit.SECONDS)
            .build()

        for (ip in candidates) {
            try {
                val url = okhttp3.Request.Builder().url("http://$ip:8000/health").get().build()
                client.newCall(url).execute().use { resp ->
                    if (resp.isSuccessful) return@withContext "http://$ip:8000"
                }
            } catch (_: Exception) {}
        }
        null
    }
}
