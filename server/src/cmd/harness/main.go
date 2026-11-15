package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"shortical.com/mock-server/src/services"
)

func main() {
	port := os.Getenv("MOCK_API_PORT")
	if port == "" {
		port = "4470"
	}
	gwURL := os.Getenv("BILLING_GATEWAY_URL")
	if gwURL == "" {
		gwURL = "http://localhost:4460"
	}
	svc := services.NewHarnessService(gwURL, "", "")
	addr := ":" + port
	fmt.Printf("==> [shortical-mock-server] Clean-Room Mock api-service listening on %s (Gateway: %s)\n", addr, gwURL)
	log.Fatal(http.ListenAndServe(addr, svc.NewHarnessRouter()))
}
