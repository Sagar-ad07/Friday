//go:build !cgo

// Friday desktop launcher (no-CGO, chromeless app window).
//
// This is the default desktop entry point when CGO is disabled. It waits for
// the Friday server (http://localhost:8000) then opens it in a chromeless
// app window via Edge/Chrome --app= (a real desktop window with no address
// bar or tab strip), falling back to a browser tab if neither browser is
// found. Requires no C toolchain.
//
// The full in-process webview shell lives in main_webview.go (//go:build cgo)
// and is used on hosts with a working C toolchain:
//   set CGO_ENABLED=1
//   go build -o friday-desktop.exe ./cmd/friday-desktop

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

const serverURL = "http://localhost:8000"

var browsers = []string{
	`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
}

var errNoBrowser = fmt.Errorf("no supported browser found")

func main() {
	log.Printf("Friday desktop launcher starting; waiting for %s ...", serverURL)

	if !waitForServer(serverURL, 30*time.Second) {
		log.Printf("Friday server not reachable at %s after 30s - opening anyway", serverURL)
	}

	if err := openAppWindow(serverURL); err != nil {
		log.Printf("Could not launch app window: %v - opening %s in browser", err, serverURL)
		if err := openBrowser(serverURL); err != nil {
			log.Printf("All launch methods failed: %v", err)
		}
	}

	log.Printf("Friday Control Center launched - closing launcher.")
}

// openAppWindow opens a chromeless desktop window (no browser chrome) using
// the first available browser's --app= mode.
func openAppWindow(url string) error {
	for _, bin := range browsers {
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		cmd := exec.Command(bin,
			"--app="+url,
			"--window-size=1500,950",
			"--class=Friday Control Center",
		)
		cmd = detachCmd(cmd) // let the browser window outlive the launcher
		if err := cmd.Start(); err == nil {
			return err
		}
	}
	return errNoBrowser
}

// openBrowser is the final fallback: a normal browser tab via rundll32.
func openBrowser(url string) error {
	return exec.Command("rundll32.exe", "url.dll,FileExecutePass", url).Start()
}

// waitForServer polls the Friday server URL until it responds 200 or the
// timeout elapses.
func waitForServer(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// detachCmd detaches the browser from the launcher's console so its GUI
// window (and process) survives the launcher exiting. We deliberately do NOT
// set CREATE_NO_WINDOW (0x08000000) — that would suppress the Edge app-window.
func detachCmd(cmd *exec.Cmd) *exec.Cmd {
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: 0x00000008, // DETACHED_PROCESS
		}
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
	}
	return cmd
}
