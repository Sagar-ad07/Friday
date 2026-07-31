package com.friday.android

import android.content.Context
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Image
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Divider
import androidx.compose.material3.Surface
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    server: String, token: String,
    voice: Boolean, autoSpeak: Boolean, eye: Boolean,
    reduceMotion: Boolean, allowActions: Boolean,
    agentName: String, agentVoice: String, avatarUri: String,
    textSizeScale: Float, continuousMode: Boolean,
    wakeWord: String, listeningTimeout: Int,
    onSave: (server: String, token: String, voice: Boolean, autoSpeak: Boolean,
             eye: Boolean, reduceMotion: Boolean, allowActions: Boolean,
             agentName: String, agentVoice: String, avatarUri: String,
             textSizeScale: Float, continuousMode: Boolean,
             wakeWord: String, listeningTimeout: Int) -> Unit,
    onEnableAccessibility: () -> Unit,
    onBack: () -> Unit,
) {
    var s by remember { mutableStateOf(server) }
    var tk by remember { mutableStateOf(token) }
    var v by remember { mutableStateOf(voice) }
    var au by remember { mutableStateOf(autoSpeak) }
    var e by remember { mutableStateOf(eye) }
    var rm by remember { mutableStateOf(reduceMotion) }
    var act by remember { mutableStateOf(allowActions) }
    var name by remember { mutableStateOf(agentName) }
    var voiceSel by remember { mutableStateOf(agentVoice) }
    var avatar by remember { mutableStateOf(avatarUri) }
    var scale by remember { mutableStateOf(textSizeScale) }
    var continuous by remember { mutableStateOf(continuousMode) }
    var wake by remember { mutableStateOf(wakeWord) }
    var timeout by remember { mutableIntStateOf(listeningTimeout) }

    val context = LocalContext.current

    Scaffold(containerColor = Obsidian, topBar = {
        TopAppBar(
            title = { Text("Settings", color = TextMain) },
            navigationIcon = {
                IconButton(onClick = {
                    onSave(s, tk, v, au, e, rm, act, name, voiceSel, avatar, scale, continuous, wake, timeout)
                    onBack()
                }) {
                    Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back", tint = TextMain)
                }
            },
            colors = TopAppBarDefaults.topAppBarColors(containerColor = Obsidian)
        )
    }) { pad ->
        Column(
            Modifier.padding(pad).padding(16.dp).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(14.dp)
        ) {
            // Connection
            SectionTitle("Connection")
            OutlinedTextField(s, { s = it }, label = { Text("Server address") },
                singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(tk, { tk = it }, label = { Text("Token (optional)") },
                singleLine = true, modifier = Modifier.fillMaxWidth())

            // Appearance
            SectionTitle("Appearance")
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Avatar", color = TextMain, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                    Text("Pick a photo for Friday", color = TextSoft, fontSize = 11.sp)
                }
                Surface(
                    modifier = Modifier.size(52.dp),
                    shape = CircleShape,
                    color = Surface3,
                    shadowElevation = 4.dp
                ) {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Text(
                            text = name.firstOrNull()?.toString() ?: "◈",
                            color = IndigoSoft,
                            fontSize = 20.sp,
                            fontWeight = FontWeight.Bold,
                            textAlign = androidx.compose.ui.text.style.TextAlign.Center
                        )
                    }
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                OutlinedButton(onClick = {
                    avatar = "custom"
                    (context as? android.app.Activity)?.let {
                        android.widget.Toast.makeText(it, "Avatar updated", android.widget.Toast.LENGTH_SHORT).show()
                    }
                }, modifier = Modifier.weight(1f)) {
                    Icon(Icons.Filled.Image, null, tint = Indigo, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("Use initial", color = Indigo, fontSize = 12.sp)
                }
                OutlinedButton(onClick = {
                    (context as? android.app.Activity)?.let {
                        android.widget.Toast.makeText(it, "Gallery pick coming soon", android.widget.Toast.LENGTH_SHORT).show()
                    }
                }, modifier = Modifier.weight(1f)) {
                    Icon(Icons.Filled.Image, null, tint = Indigo, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("Gallery", color = Indigo, fontSize = 12.sp)
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Text size", color = TextMain, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                    Text("${"%.1f".format(scale)}x", color = TextSoft, fontSize = 11.sp)
                }
                Slider(
                    value = scale,
                    onValueChange = { scale = it },
                    valueRange = 0.85f..1.4f,
                    steps = 5,
                    colors = SliderDefaults.colors(thumbColor = Mint, activeTrackColor = Mint)
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Background", color = TextMain, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                    Text("Dark theme", color = TextSoft, fontSize = 11.sp)
                }
                Switch(checked = true, onCheckedChange = {}, enabled = false,
                    colors = SwitchDefaults.colors(checkedThumbColor = Obsidian, checkedTrackColor = Mint))
            }

            // Voice
            SectionTitle("Voice")
            SettingRow("Voice replies", "Listen to Friday", v) { v = it }
            SettingRow("Auto-speak", "Speak every answer aloud", au) { au = it }
            OutlinedTextField(voiceSel, { voiceSel = it }, label = { Text("TTS voice") },
                singleLine = true, modifier = Modifier.fillMaxWidth())
            Text("Common: en-IN-NeerjaNeural, en-IN-ShaanNeural, en-US-GuyNeural",
                color = TextSoft, fontSize = 11.sp)

            // Continuous conversation
            SectionTitle("Conversation mode")
            SettingRow("Always listening", "Background voice — no button needed", continuous) { continuous = it }
            OutlinedTextField(wake, { wake = it }, label = { Text("Wake word") },
                singleLine = true, modifier = Modifier.fillMaxWidth())
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Listening timeout", color = TextMain, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                    Text("$timeout seconds", color = TextSoft, fontSize = 11.sp)
                }
                OutlinedButton(onClick = {
                    timeout = (timeout + 1).coerceAtMost(30)
                }) { Text("+1s", color = Indigo, fontSize = 12.sp) }
            }

            // Screen & actions
            SectionTitle("Screen & actions")
            SettingRow("Watch my screen", "Background screen capture (the eye)", e) { e = it }
            SettingRow("Allow real actions", "Let Friday send SMS / control the phone (asks first)",
                act) { act = it }
            SettingRow("Reduce motion", "Calm the presence animation", rm) { rm = it }

            OutlinedButton(onClick = onEnableAccessibility, modifier = Modifier.fillMaxWidth()) {
                Text("Open Accessibility settings", color = Indigo)
            }
            Text("Friday only acts on your command. Real actions ask for confirmation.",
                color = TextSoft, fontSize = 11.sp)
        }
    }
}

@Composable
private fun SectionTitle(title: String) {
    Text(title, color = IndigoSoft, fontSize = 12.sp, fontWeight = FontWeight.Bold,
        modifier = Modifier.padding(top = 4.dp))
}

@Composable
private fun SettingRow(title: String, sub: String, value: Boolean, onChange: (Boolean) -> Unit) {
    Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
        Column(modifier = Modifier.weight(1f)) {
            Text(title, color = TextMain, fontSize = 14.sp, fontWeight = FontWeight.Medium)
            Text(sub, color = TextSoft, fontSize = 11.sp)
        }
        Switch(checked = value, onCheckedChange = onChange,
            colors = SwitchDefaults.colors(
                checkedThumbColor = Obsidian, checkedTrackColor = Mint,
                uncheckedTrackColor = Surface2))
    }
}
