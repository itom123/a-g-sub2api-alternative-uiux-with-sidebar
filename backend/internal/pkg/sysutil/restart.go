// Package sysutil provides system-level utilities for process management.
package sysutil

import (
	"log"
	"os"
	"runtime"
	"time"
)

// RestartService triggers a service restart by gracefully exiting.
//
// The process supervisor owns the restart: systemd on Linux and launchd on
// macOS. Both deployments configure the service to stay alive, so exiting is
// safer than trying to replace the running process from inside the request.
func RestartService() error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		log.Printf("Service restart is not configured for %s", runtime.GOOS)
		return nil
	}

	log.Println("Initiating service restart by graceful exit...")
	if runtime.GOOS == "darwin" {
		log.Println("launchd will automatically restart the service (KeepAlive=true)")
	} else {
		log.Println("systemd will automatically restart the service (Restart=always)")
	}

	// Give a moment for logs to flush and response to be sent
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()

	return nil
}

// RestartServiceAsync is a fire-and-forget version of RestartService.
// It logs errors instead of returning them, suitable for goroutine usage.
func RestartServiceAsync() {
	if err := RestartService(); err != nil {
		log.Printf("Service restart failed: %v", err)
		if runtime.GOOS == "darwin" {
			log.Println("Please restart the service manually with launchctl kickstart")
		} else {
			log.Println("Please restart the service manually: sudo systemctl restart sub2api")
		}
	}
}
