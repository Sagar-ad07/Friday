# Friday — Native Android App

A free, **sideloadable** native Android app for Friday (no Play Store, no $25 fee).
Built with Kotlin + Jetpack Compose. The brand mark is the **F1 "living core"** logo.

> iPhone / iOS is intentionally out of scope — Apple blocks free permanent sideload.
> Keep iPhone on the browser PWA (`interface/phone_eye.html`).

---

## What it does

- **Presence "Pulse"** — Friday's living core node breathes and changes color/state:
  `Here.` → `Watching your screen` → `Listening…` → `Thinking…` → `Speaking…` → `With someone else`.
- **Chat** — streams `/command/stream` (Server-Sent Events): thought → final → audio.
- **Worker chips** — shows Friday's versions (companion / coder / researcher / reasoner /
  judge / verifier / router). Tap one to bias the turn (sent as `role`).
- **Voice** — mic + speak (hook ready at `VoiceRecorder.kt`).
- **Watch my screen** — background MediaProjection capture (`ScreenEye`), change-gated,
  uploaded to `/eye/submit` and `/device/{id}/screenshot`. Friday can then *see* the phone.
- **Real device control** — via AccessibilityService + command queue: tap, swipe, type,
  open app, open URL, **send real SMS**. Every real action routes through Friday's
  confirmation gate first (`tools.py` marks `phone_*` destructive).
- **Onboarding + Settings** — connect to your PC's Friday server, then enable the
  accessibility permission (with a plain-English *why*).

---

## Architecture

```
com.friday.android
 ├ MainActivity.kt          # Splash → onboarding / main / settings, perms, service start
 ├ FridayScreen.kt          # Compose chat + chips + toggles + Pulse
 ├ PulseView.kt             # The living presence core (logo node, animated)
 ├ FridayViewModel.kt       # Presence heartbeat, team load, chat stream, greeting
 ├ OnboardingScreen.kt      # Connect + explain permissions
 ├ SettingsScreen.kt        # Server/token/toggles
 ├ Theme.kt                 # Obsidian dark theme, 13.5sp chat text
 ├ Presence.kt              # Presence states
 ├ Settings.kt              # DataStore persistence
 ├ ApiClient.kt             # HTTP + SSE to run.py (best-effort, never throws)
 ├ CommandExecutor.kt       # Run a device command (tap/type/open/sms/url)
 ├ DeviceService.kt         # Foreground: polls GET /device/{id}/commands, runs them
 ├ ScreenEye.kt             # MediaProjection capture → upload (the eye)
 └ FridayAccessibilityService.kt  # Gesture/typing hands; user-enabled
```

Backend contract (already wired in `run.py`):
`POST /device/register`, `GET /device/{id}/commands`,
`POST /device/{id}/result`, `POST /device/{id}/screenshot`,
`POST /device/{id}/command`, plus `POST /command/stream` (SSE) and `POST /eye/submit`.

---

## Prerequisites (one-time)

1. **Android Studio** (or just the **command-line Android SDK**) with:
   - SDK Platform 34, build-tools 34.x, NDK not required.
2. **Java 17** (`JAVA_HOME` set).
3. **Gradle 8.7** (or use the wrapper — see below).
4. **USB debugging** enabled on the phone:
   Settings → About phone → tap Build number 7× → Developer options → USB debugging.

---

## Build

### Option A — Android Studio
Open the `android_app/` folder. Let it sync Gradle, then
**Build → Build Bundle(s) / APK(s) → Build APK(s)**.
The APK lands in `app/build/outputs/apk/debug/app-debug.apk`.

### Option B — Command line
From `android_app/` (needs the gradle wrapper jar, or a system gradle 8.7):

```powershell
# regenerate the wrapper jar if gradlew is missing
gradle wrapper --gradle-version 8.7

# build debug APK
.\gradlew.bat assembleDebug
# -> app\build\outputs\apk\debug\app-debug.apk
```

---

## Connect the phone

Make sure your **PC** is running Friday and is on the **same Wi-Fi** as the phone:

```
cd "D:\Friday - prototype"
.\Start-Friday.bat
```

Note the address (`http://<PC-IP>:8000`). You'll type it into the app during onboarding.

- If it says `0.0.0.0:8000`, use your LAN IP (`ipconfig` → IPv4).
- Use `http://127.0.0.1:8000` only if the app runs on the PC itself (emulator).

Optional token: set `FRIDAY_TOKEN` in the PC's `.env`; enter the same value in the app.
Leave blank if no token is configured.

---

## Sideload / install

```powershell
# 1. Find the phone
adb devices

# 2. Install (no Play Store, free)
adb install -r "android_app\app\build\outputs\apk\debug\app-debug.apk"

# 3. Launch: open "Friday" from the app drawer, or
adb shell monkey -p com.friday.android -c android.intent.category.LAUNCHER 1
```

On first open:
1. Enter the server address → **Test connection** (must show ✓ Connected).
2. Tap **Open Accessibility settings** → enable **Friday**.
3. **Enter Friday.**

To grant the projection/notification permissions that prompt at runtime, just approve the
system dialogs when toggling **Watch my screen** or on the first launch.

---

## Updating

Rebuild and reinstall:
```powershell
.\gradlew.bat assembleDebug
adb install -r "android_app\app\build\outputs\apk\debug\app-debug.apk"
```

---

## Notes / limits

- `minSdk 26` (Android 8). `targetSdk 34`.
- The app never runs Friday's model itself — it's a **client** to your PC's brain, so it's
  light and the logic stays in one place.
- The eye uploads ~640px, ~0.55 JPEG, every 4s, and **only when the frame changed**.
- Real actions (SMS etc.) always ask for confirmation through Friday first.
- iPhone: use the browser PWA, not this app.
