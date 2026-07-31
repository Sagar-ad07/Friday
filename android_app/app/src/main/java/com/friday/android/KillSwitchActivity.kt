package com.friday.android

import android.content.Intent
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * Kill Switch Confirmation Activity (Compose).
 * Requires the user to type "KILL" to confirm the emergency shutdown.
 * Reaches KillSwitchReceiver via the com.friday.android.KILL_SWITCH broadcast.
 */
class KillSwitchActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            KillSwitchScreen(
                onConfirmed = { confirmed ->
                    if (confirmed) {
                        sendBroadcast(Intent("com.friday.android.KILL_SWITCH"))
                        Toast.makeText(
                            this,
                            "KILL SWITCH ACTIVATED - Emergency shutdown initiated",
                            Toast.LENGTH_LONG
                        ).show()
                        setResult(RESULT_OK)
                    } else {
                        setResult(RESULT_CANCELED)
                    }
                    finish()
                },
                onCanceled = {
                    setResult(RESULT_CANCELED)
                    finish()
                }
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KillSwitchScreen(onConfirmed: (Boolean) -> Unit, onCanceled: () -> Unit) {
    var text by remember { mutableStateOf("") }
    val canConfirm = text.trim().uppercase() == "KILL"

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Obsidian)
    ) {
        Surface(
            shape = RoundedCornerShape(20.dp),
            color = Surface2,
            modifier = Modifier
                .align(Alignment.Center)
                .padding(horizontal = 24.dp)
        ) {
            Column(
                modifier = Modifier
                    .padding(horizontal = 20.dp, vertical = 18.dp),
                horizontalAlignment = Alignment.CenterHorizontally
            ) {
                Text(
                    text = "⚡ Kill Switch",
                    color = Danger,
                    fontSize = 18.sp,
                    fontWeight = FontWeight.Bold
                )
                Spacer(Modifier.height(6.dp))
                Text(
                    text = "Type KILL to activate emergency shutdown",
                    color = TextSoft,
                    fontSize = 13.sp
                )
                Spacer(Modifier.height(14.dp))

                OutlinedTextField(
                    value = text,
                    onValueChange = { if (it.length <= 4) text = it },
                    singleLine = true,
                    isError = !canConfirm && text.isNotBlank(),
                    placeholder = { Text("KILL", color = TextSoft) },
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Text,
                        capitalization = KeyboardCapitalization.Characters
                    ),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = Danger,
                        unfocusedBorderColor = TextSoft,
                        cursorColor = Danger,
                        focusedPlaceholderColor = Danger,
                        unfocusedPlaceholderColor = TextSoft
                    ),
                    textStyle = androidx.compose.ui.text.TextStyle(color = TextMain, fontWeight = FontWeight.Bold)
                )

                Spacer(Modifier.height(8.dp))
                Text(
                    text = if (canConfirm) "Tap below to execute" else "Type exactly KILL",
                    color = if (canConfirm) Mint else Danger,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.SemiBold
                )

                Spacer(Modifier.height(18.dp))
                Row(
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    TextButton(onClick = onCanceled) {
                        Text("Cancel", color = TextSoft, fontWeight = FontWeight.Medium)
                    }
                    Button(
                        onClick = { onConfirmed(true) },
                        enabled = canConfirm,
                        shape = RoundedCornerShape(12.dp),
                        colors = ButtonDefaults.buttonColors(containerColor = Danger)
                    ) {
                        Text("ACTIVATE", color = Color.White, fontWeight = FontWeight.Bold, fontSize = 12.sp)
                    }
                }
            }
        }
    }
}
