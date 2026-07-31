package com.friday.android

import androidx.compose.animation.core.*
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The Pulse — Friday's living presence. This IS the logo's core node, breathing.
 * Motion + color communicate her state of being. Respects reduceMotion.
 */
@Composable
fun PulseView(presence: Presence, reduceMotion: Boolean) {
    val accent = when (presence) {
        Presence.WATCHING -> Mint
        Presence.LISTENING -> Mint
        Presence.SPEAKING -> IndigoSoft
        Presence.THINKING -> Indigo
        Presence.BUSY -> TextSoft
        Presence.OFFLINE -> Danger
        else -> Indigo
    }
    val period = when (presence) {
        Presence.THINKING -> 1200
        Presence.SPEAKING -> 900
        Presence.LISTENING -> 1400
        else -> 3000
    }
    val transition = rememberInfiniteTransition(label = "pulse")
    val t by if (reduceMotion) mutableFloatStateOf(0.5f) else transition.animateFloat(
        initialValue = 0f, targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(period, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse
        ), label = "breath"
    )

    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Canvas(modifier = Modifier.size(150.dp)) {
            val cx = size.width / 2f
            val cy = size.height / 2f
            val base = size.minDimension / 2f
            // outer breathing rings
            val ringR = base * (0.62f + 0.10f * t)
            drawCircle(
                color = accent.copy(alpha = 0.10f + 0.10f * t),
                radius = ringR, center = Offset(cx, cy),
                style = Stroke(width = 3f)
            )
            drawCircle(
                color = accent.copy(alpha = 0.20f),
                radius = base * 0.46f, center = Offset(cx, cy),
                style = Stroke(width = 5f)
            )
            // bright core node (the logo's node)
            val coreR = base * (0.18f + 0.03f * t)
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(Color(0xFFEAF7FF), accent),
                    center = Offset(cx, cy), radius = coreR
                ),
                radius = coreR, center = Offset(cx, cy)
            )
        }
        Spacer(Modifier.height(14.dp))
        Text(presence.label, color = TextMain, fontSize = 15.sp, fontWeight = FontWeight.Medium)
    }
}
