package com.friday.android

/** Friday's living presence states, mapped from backend signals. */
enum class Presence(val label: String) {
    OFFLINE("Reconnecting…"),
    HERE("Here."),
    WATCHING("Watching your screen"),
    LISTENING("Listening…"),
    THINKING("Thinking…"),
    SPEAKING("Speaking…"),
    BUSY("With someone else — one sec")
}
