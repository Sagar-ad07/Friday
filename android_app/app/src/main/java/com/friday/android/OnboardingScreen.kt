package com.friday.android

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/** First-run onboarding: connect, then explain permissions with WHY (builds trust). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OnboardingScreen(
    initialServer: String,
    onTest: (String, (Boolean) -> Unit) -> Unit,
    onScan: () -> Unit,
    onEnableAccessibility: () -> Unit,
    onDone: (String) -> Unit,
) {
    var server by remember { mutableStateOf(initialServer) }
    var tested by remember { mutableStateOf<Boolean?>(null) }
    var testing by remember { mutableStateOf(false) }

    Column(
        Modifier.fillMaxSize().padding(24.dp).verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(18.dp)
    ) {
        Spacer(Modifier.height(12.dp))
        Text("Meet Friday", color = TextMain, fontSize = 26.sp, fontWeight = FontWeight.Bold)
        Text("A quiet companion in your pocket. Let's connect her to your computer.",
            color = TextSoft, fontSize = 13.5.sp)

        Card(colors = CardDefaults.cardColors(containerColor = Surface2)) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("1 · Connect", color = TextMain, fontWeight = FontWeight.SemiBold)
                Text("Enter the address shown when you run Friday on your PC (e.g. http://192.168.1.50:8000).",
                    color = TextSoft, fontSize = 12.sp)
                OutlinedTextField(
                    value = server, onValueChange = { server = it; tested = null },
                    label = { Text("Server address") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedContainerColor = Obsidian, unfocusedContainerColor = Obsidian
                    )
                )
                TextButton(onClick = onScan, colors = ButtonDefaults.textButtonColors(contentColor = Indigo)) {
                    Text("Scan local network")
                }
                Button(
                    onClick = { testing = true; onTest(server) { ok -> tested = ok; testing = false } },
                    enabled = !testing,
                    colors = ButtonDefaults.buttonColors(containerColor = Indigo)
                ) { Text(if (testing) "Testing…" else "Test connection", color = Obsidian) }
                when (tested) {
                    true -> Text("✓ Connected", color = Mint, fontSize = 12.sp)
                    false -> Text("✗ Couldn't reach Friday. Same Wi-Fi? Server running?",
                        color = Color(0xFFFF6F91), fontSize = 12.sp)
                    else -> {}
                }
            }
        }

        Card(colors = CardDefaults.cardColors(containerColor = Surface2)) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("2 · Let Friday help (optional)", color = TextMain, fontWeight = FontWeight.SemiBold)
                Text("Accessibility lets Friday tap and type for you. Screen sharing lets her SEE what you're looking at. You control both — turn them off any time.",
                    color = TextSoft, fontSize = 12.sp)
                OutlinedButton(onClick = onEnableAccessibility) {
                    Text("Open Accessibility settings", color = Indigo)
                }
            }
        }

        Button(
            onClick = { onDone(server) },
            enabled = tested == true,
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.buttonColors(containerColor = Mint)
        ) { Text("Enter Friday", color = Obsidian, fontWeight = FontWeight.Bold) }
        Spacer(Modifier.height(20.dp))
    }
}
