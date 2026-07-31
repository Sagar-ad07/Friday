//go:build cgo

package main

import (
	"log"
	"net/http"
	"time"

	"github.com/webview/webview_go"
)

const (
	serverURL   = "http://localhost:8000"
	windowTitle = "Friday Control Center"
	windowW     = 1500
	windowH     = 950
)

// main_webview.go is the professional in-process desktop shell.
// It embeds an Edge WebView2 window (via webview_go) pointed at the
// local Friday server. Built when CGO is enabled; requires the
// WebView2 runtime (present) + a C/C++ toolchain (mingw64).
//
// Build:
//   set CGO_ENABLED=1 CC=C:\msys64\mingw64\bin\gcc.exe CXX=C:\msys64\mingw64\bin\g++.exe
//   go build ./cmd/friday-desktop
//
// When CGO is disabled, main_nocgo.go (browser launcher) is used instead.
func main() {
	log.Printf("Starting %s ...", windowTitle)

	// Wait for the Friday server to be ready before drawing the window.
	if !waitForServer(serverURL, 30*time.Second) {
		log.Printf("Friday server not reachable at %s after 30s", serverURL)
	}

	w := webview.NewWithOptions(webview.WebViewOptions{
		Debug:      false,
		AutoFocus:  true,
		WindowOptions: webview.WindowOptions{
			Title:     windowTitle,
			Width:     windowW,
			Height:    windowH,
			MinWidth:  1200,
			MinHeight: 800,
			Resizable: true,
		},
	})
	defer w.Destroy()

	w.SetSize(windowW, windowH, webview.HintNone)
	w.Navigate(serverURL)
	w.Run()
}

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
