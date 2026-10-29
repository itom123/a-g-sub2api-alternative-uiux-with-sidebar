package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/openclawworker"
)

func main() {
	secret, err := os.ReadFile(os.Getenv("OPENCLAW_WORKER_TOKEN_FILE")) // #nosec G703 -- Local operator supplies the credential file path, never a request parameter.
	if err != nil || len(strings.TrimSpace(string(secret))) < 32 {
		log.Fatal("OPENCLAW_WORKER_TOKEN_FILE must point to a private token file (32+ characters)")
	}
	binary := os.Getenv("OPENCLAW_BIN")
	if binary == "" {
		binary = "/usr/local/bin/openclaw"
	}
	maxCells := 6
	if value, parseErr := strconv.Atoi(os.Getenv("OPENCLAW_FLEET_MAX_CELLS")); parseErr == nil && value > 0 {
		maxCells = value
	}
	enrollmentSecret := []byte(strings.TrimSpace(os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_SECRET")))
	if value := strings.TrimSpace(os.Getenv("OPENCLAW_DEVICE_ENROLLMENT_SECRET_FILE")); value != "" {
		if fileSecret, readErr := os.ReadFile(value); readErr == nil { // #nosec G703 -- Local operator supplies the secret file path, never a request parameter.
			enrollmentSecret = []byte(strings.TrimSpace(string(fileSecret)))
		}
	}
	if len(enrollmentSecret) != 0 && len(enrollmentSecret) < 32 {
		log.Fatal("OPENCLAW_DEVICE_ENROLLMENT_SECRET must be empty or at least 32 characters")
	}
	worker := &openclawworker.Worker{Token: strings.TrimSpace(string(secret)), EnrollmentSecret: enrollmentSecret, Binary: binary, Image: os.Getenv("OPENCLAW_FLEET_IMAGE"), MaxCells: maxCells}
	addr := os.Getenv("OPENCLAW_WORKER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:19200"
	}
	server := &http.Server{Addr: addr, Handler: worker, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 6 * time.Minute, IdleTimeout: time.Minute, MaxHeaderBytes: 8192}
	log.Printf("OpenClaw private worker listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
