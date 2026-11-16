package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"shortical.com/mock-server/src/services"
)

func envIntOr(key string, fallback int) int {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func main() {
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if appEnv != "" && appEnv != "local" {
		s2sSec := strings.TrimSpace(os.Getenv("S2S_HMAC_SECRET_CURRENT"))
		if s2sSec == "" || strings.HasPrefix(s2sSec, "REPLACE_") {
			log.Fatalf("fail-closed: S2S_HMAC_SECRET_CURRENT must be configured to a non-placeholder secret when APP_ENV=%s", appEnv)
		}
	}

	port := strings.TrimSpace(os.Getenv("MOCK_API_PORT"))
	if port == "" {
		port = "4470"
	}
	gwURL := strings.TrimSpace(os.Getenv("BILLING_GATEWAY_URL"))
	if gwURL == "" {
		gwURL = "http://localhost:4460"
	}
	svc := services.NewHarnessService(gwURL, "", "")
	addr := ":" + port
	srv := &http.Server{
		Addr:              addr,
		Handler:           svc.NewHarnessRouter(),
		ReadHeaderTimeout: time.Duration(envIntOr("HTTP_READ_HEADER_TIMEOUT_MS", 5000)) * time.Millisecond,
		ReadTimeout:       time.Duration(envIntOr("HTTP_READ_TIMEOUT_MS", 10000)) * time.Millisecond,
		WriteTimeout:      time.Duration(envIntOr("HTTP_WRITE_TIMEOUT_MS", 15000)) * time.Millisecond,
		IdleTimeout:       time.Duration(envIntOr("HTTP_IDLE_TIMEOUT_MS", 60000)) * time.Millisecond,
		MaxHeaderBytes:    1 << 16,
	}
	fmt.Printf("==> [shortical-mock-server] Clean-Room Mock api-service listening on %s (Gateway: %s)\n", addr, gwURL)
	log.Fatal(srv.ListenAndServe())
}
