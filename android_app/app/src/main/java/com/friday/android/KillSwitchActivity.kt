package com.friday.android

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import android.widget.TextView
import android.text.TextUtils

/**
 * Kill Switch Confirmation Activity
 * Requires user to type "KILL" to confirm emergency shutdown
 */
class KillSwitchActivity : AppCompatActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_kill_switch)

        val input = findViewById<EditText>(R.id.killInput)
        val btnConfirm = findViewById<Button>(R.id.btnConfirm)
        val btnCancel = findViewById<Button>(R.id.btnCancel)
        val tvWarning = findViewById<TextView>(R.id.tvWarning)

        // Focus the input field
        input.requestFocus()

        input.addTextChangedListener { s ->
            val text = s.toString().trim().uppercase()
            findViewById<Button>(R.id.btnConfirm).isEnabled = text == "KILL"
        }

        btnCancel.setOnClickListener {
            setResult(RESULT_CANCELED)
            finish()
        }

        btnConfirm.setOnClickListener {
            val inputText = input.text.toString().trim().uppercase()
            if (inputText == "KILL") {
                // Send kill broadcast
                val intent = Intent("com.friday.android.KILL_SWITCH")
                sendBroadcast(Intent("com.friday.android.KILL_SWITCH"))
                Toast.makeText(this, "KILL SWITCH ACTIVATED - Emergency shutdown initiated", Toast.LENGTH_LONG).show()
                setResult(RESULT_OK)
                finish()
            } else {
                Toast.makeText(this, "Type KILL exactly to confirm", Toast.LENGTH_SHORT).show()
            }
        }
    }
}