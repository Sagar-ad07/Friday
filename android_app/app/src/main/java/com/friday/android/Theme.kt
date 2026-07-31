package com.friday.android

import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.sp

val Obsidian = Color(0xFF070A12)
val Surface2 = Color(0xFF0C1120)
val Surface3 = Color(0xFF111827)
val Indigo = Color(0xFF7C8CFF)
val IndigoSoft = Color(0xFF9AA6FF)
val Mint = Color(0xFF55E2C0)
val TextMain = Color(0xFFE8ECF8)
val TextSoft = Color(0xFF8B95B5)
val Danger = Color(0xFFFF6F91)

private val scheme = darkColorScheme(
    primary = Indigo,
    secondary = Mint,
    background = Obsidian,
    surface = Surface2,
    onPrimary = Obsidian,
    onBackground = TextMain,
    onSurface = TextMain,
    error = Danger,
)

// Chat body deliberately "smaller-than-medium, not tiny": 13.5sp.
private val typography = Typography(
    bodyMedium = TextStyle(fontSize = 13.5.sp, lineHeight = 19.sp, color = TextMain),
    bodyLarge = TextStyle(fontSize = 14.5.sp, lineHeight = 20.sp, color = TextMain),
    titleMedium = TextStyle(fontSize = 15.sp, color = TextMain),
    labelSmall = TextStyle(fontSize = 11.sp, color = TextSoft),
)

@Composable
fun FridayTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = scheme, typography = typography, content = content)
}
