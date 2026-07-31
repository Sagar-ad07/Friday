package com.friday.android

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.Image as ComposeImage
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.clip
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.Image
import androidx.compose.animation.core.InfiniteRepeatableSpec
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.RepeatMode
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import android.content.Context
import android.os.Build
import android.os.VibrationEffect
import android.os.Vibrator
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FridayScreen(
    vm: FridayViewModel,
    eyeOn: Boolean,
    voiceOn: Boolean,
    onToggleEye: (Boolean) -> Unit,
    onToggleVoice: (Boolean) -> Unit,
    onMic: () -> Unit,
    onOpenSettings: () -> Unit,
    onSelectRole: (String) -> Unit,
) {
    // Read Compose state directly from ViewModel (no StateFlow / collectAsState needed)
    val presence: Presence = vm.presence
    val messages: List<ChatMsg> = vm.messages
    val workers: List<Worker> = vm.workers
    val sending: Boolean = vm.sending
    val continuousMode: Boolean = vm.continuousMode
    val avatarUri: String = vm.avatarUri
    val agentName: String = vm.agentName
    val textScale: Float = vm.textScale

    var input by remember { mutableStateOf("") }
    var selectedRole by remember { mutableStateOf("") }
    val listState = rememberLazyListState()

    LaunchedEffect(messages.size) {
        if (messages.isNotEmpty()) listState.animateScrollToItem(messages.size - 1)
    }

    LaunchedEffect(continuousMode) {
        if (continuousMode) {
            vm.startContinuousListening()
        } else {
            vm.stopContinuousListening()
        }
    }

    Scaffold(
        containerColor = Obsidian,
        topBar = {
            Column(modifier = Modifier.fillMaxWidth()) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    // Avatar — full Friday logo tile (branded, clearly visible)
                    ComposeImage(
                        painter = painterResource(id = R.drawable.friday_logo),
                        contentDescription = "Friday",
                        modifier = Modifier
                            .size(40.dp)
                            .clip(RoundedCornerShape(10.dp))
                    )
                    Spacer(Modifier.width(10.dp))
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = agentName.ifBlank { "FRIDAY" },
                            color = TextMain,
                            fontWeight = FontWeight.Bold,
                            fontSize = 15.sp,
                            letterSpacing = 1.sp
                        )
                        Text(
                            text = presence.label,
                            color = when (presence) {
                                Presence.THINKING -> IndigoSoft
                                Presence.SPEAKING -> Mint
                                Presence.LISTENING -> IndigoSoft
                                Presence.WATCHING -> Mint
                                Presence.BUSY -> Danger
                                Presence.OFFLINE -> TextSoft
                                else -> TextSoft
                            },
                            fontSize = 11.sp,
                            fontWeight = FontWeight.Medium
                        )
                    }
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (continuousMode) {
                            IconButton(onClick = { vm.stopContinuousMode() }) {
                                Icon(Icons.Filled.Stop, "Stop continuous", tint = Danger)
                            }
                        }
                        IconButton(onClick = { onToggleEye(!eyeOn) }) {
                            Icon(Icons.Filled.Visibility, "Eye", tint = if (eyeOn) Mint else TextSoft)
                        }
                        IconButton(onClick = onOpenSettings) {
                            Icon(Icons.Filled.Settings, "Settings", tint = TextSoft)
                        }
                    }
                }
                HorizontalDivider(color = Color.White.copy(alpha = 0.06f), thickness = 1.dp)
            }
        }
    ) { pad ->
        Column(modifier = Modifier.padding(pad).fillMaxSize()) {
            Spacer(Modifier.height(6.dp))

            // Worker chips
            if (workers.isNotEmpty()) {
                Row(
                    modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())
                        .padding(horizontal = 12.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp)
                ) {
                    workers.forEach { w ->
                        val sel = selectedRole == w.id
                        val c = runCatching { Color(android.graphics.Color.parseColor(w.color)) }
                            .getOrDefault(Indigo)
                        Row(
                            modifier = Modifier.clip(RoundedCornerShape(999.dp))
                                .background(if (sel) c.copy(alpha = 0.18f) else Surface2)
                                .border(1.dp, if (sel) c else Color.White.copy(alpha = 0.08f),
                                    RoundedCornerShape(999.dp))
                                .clickable {
                                    selectedRole = if (sel) "" else w.id
                                    onSelectRole(selectedRole)
                                }
                                .padding(horizontal = 12.dp, vertical = 6.dp),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Text(w.emoji, fontSize = 12.sp)
                            Spacer(Modifier.width(6.dp))
                            Text(w.name, color = if (sel) TextMain else TextSoft, fontSize = 11.sp)
                        }
                    }
                }
                Spacer(Modifier.height(8.dp))
            }

            // Chat messages
            LazyColumn(
                state = listState,
                modifier = Modifier.weight(1f).fillMaxWidth().padding(horizontal = 14.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp)
            ) {
                items(messages) { m ->
                    MessageBubble(m, textScale = textScale)
                }
            }

            // Continuous mode indicator
            if (continuousMode) {
                Surface(
                    color = Mint.copy(alpha = 0.1f),
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 14.dp)
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 14.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        Box(
                            modifier = Modifier.size(8.dp).clip(CircleShape).background(Mint)
                        )
                        Text(
                            text = "Conversation mode — just speak",
                            color = Mint,
                            fontSize = 12.sp
                        )
                        Spacer(Modifier.weight(1f))
                        TextButton(onClick = { vm.stopContinuousMode() }) {
                            Text("Stop", color = Danger, fontSize = 12.sp)
                        }
                    }
                }
                Spacer(Modifier.height(4.dp))
            }

            // Quick toggles
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                ToggleChip("👁 Watch", eyeOn) { onToggleEye(!eyeOn) }
                ToggleChip("🔊 Voice", voiceOn) { onToggleVoice(!voiceOn) }
                ToggleChip("🎙 Continuous", continuousMode) {
                    if (continuousMode) vm.stopContinuousMode() else vm.startContinuousMode()
                }
            }

            // FRIDAY AUTONOMY — signature master toggle (live trading engines + eye).
            // A single tap arms or disarms the whole autonomous stack.
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.Center
            ) {
                val ctx = LocalContext.current
                val transition = rememberInfiniteTransition(label = "autonomyPulse")
                val pulseAlpha by transition.animateFloat(
                    initialValue = 0.25f, targetValue = 0.9f,
                    animationSpec = InfiniteRepeatableSpec(tween(900, easing = LinearEasing), RepeatMode.Reverse),
                    label = "pulse"
                )
                Box(
                    modifier = Modifier
                        .clip(RoundedCornerShape(999.dp))
                        .background(if (vm.autonomyOn) Mint.copy(alpha = 0.18f) else Surface2)
                        .border(
                            width = if (vm.autonomyOn) 2.dp else 1.dp,
                            color = if (vm.autonomyOn) Mint.copy(alpha = pulseAlpha) else Color.White.copy(alpha = 0.08f),
                            shape = RoundedCornerShape(999.dp)
                        )
                        .clickable {
                            val v = ctx.getSystemService(Context.VIBRATOR_SERVICE) as Vibrator
                            if (Build.VERSION.SDK_INT >= 26)
                                v.vibrate(VibrationEffect.createOneShot(16, VibrationEffect.DEFAULT_AMPLITUDE))
                            else
                                @Suppress("DEPRECATION") v.vibrate(16)
                            vm.toggleAutonomy(!vm.autonomyOn)
                        }
                        .padding(horizontal = 18.dp, vertical = 10.dp)
                ) {
                    Text(
                        text = if (vm.autonomyOn) "⚡ AUTONOMY ON" else "⚡ AUTONOMY",
                        color = if (vm.autonomyOn) Mint else TextSoft,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.Bold,
                        letterSpacing = 0.5.sp
                    )
                }
                Spacer(Modifier.width(8.dp))
                Text(
                    text = vm.autonomyStatus,
                    color = if (vm.autonomyStatus == "live") Mint
                        else if (vm.autonomyStatus == "error") Danger else TextSoft,
                    fontSize = 10.sp
                )
            }

            // Input row
            Row(
                modifier = Modifier.fillMaxWidth().padding(12.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp)
            ) {
                OutlinedTextField(
                    value = input,
                    onValueChange = { input = it },
                    modifier = Modifier.weight(1f),
                    placeholder = {
                        Text(
                            "Message $agentName…",
                            color = TextSoft,
                            fontSize = (13.5f * textScale).sp
                        )
                    },
                    textStyle = MaterialTheme.typography.bodyMedium.copy(fontSize = (14f * textScale).sp),
                    shape = RoundedCornerShape(16.dp),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = Indigo,
                        unfocusedBorderColor = Color.White.copy(alpha = 0.1f),
                        focusedContainerColor = Surface2,
                        unfocusedContainerColor = Surface2,
                    ),
                    maxLines = 4
                )
                IconButton(onClick = onMic) {
                    Icon(Icons.Filled.Mic, "Voice", tint = if (voiceOn) Mint else TextSoft)
                }
                FilledIconButton(
                    onClick = {
                        if (input.isNotBlank() && !sending) {
                            vm.send(input.trim())
                            input = ""
                        }
                    },
                    colors = IconButtonDefaults.filledIconButtonColors(containerColor = Indigo)
                ) {
                    Icon(Icons.AutoMirrored.Filled.Send, "Send", tint = Obsidian)
                }
            }
        }
    }
}

@Composable
private fun MessageBubble(m: ChatMsg, textScale: Float = 1f) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = if (m.fromUser) Arrangement.End else Arrangement.Start
    ) {
        Surface(
            color = if (m.fromUser) Indigo.copy(alpha = 0.16f) else Surface2,
            shape = RoundedCornerShape(
                topStart = 14.dp, topEnd = 14.dp,
                bottomStart = if (m.fromUser) 14.dp else 4.dp,
                bottomEnd = if (m.fromUser) 4.dp else 14.dp
            ),
            modifier = Modifier.widthIn(max = 300.dp)
        ) {
            Text(
                m.text,
                modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp),
                style = if (m.fromUser)
                    MaterialTheme.typography.bodyLarge.copy(fontSize = (14.5f * textScale).sp)
                else
                    MaterialTheme.typography.bodyMedium.copy(fontSize = (13.5f * textScale).sp),
                color = TextMain,
                lineHeight = (20.sp * textScale)
            )
        }
    }
}

@Composable
private fun ToggleChip(label: String, on: Boolean, onClick: () -> Unit) {
    val c = if (on) Mint else TextSoft
    Row(
        modifier = Modifier.clip(RoundedCornerShape(999.dp))
            .background(if (on) Mint.copy(alpha = 0.12f) else Surface2)
            .border(1.dp, if (on) Mint.copy(alpha = 0.5f) else Color.White.copy(alpha = 0.08f),
                RoundedCornerShape(999.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(label, color = c, fontSize = 12.sp)
    }
}
