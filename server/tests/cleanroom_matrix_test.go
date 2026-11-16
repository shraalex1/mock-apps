package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"shortical.com/mock-server/src/models"
	"shortical.com/mock-server/src/services"
)

func TestCleanRoomMockHarnessFaultInjectionAndWebhookUnlock(t *testing.T) {
	svc := services.NewHarnessService("http://localhost:4460", "dev_s2s_hmac_secret_shortical_2026_primary", "p8_sand_apple_storekit2_keyid_7721f")
	ts := httptest.NewServer(svc.NewHarnessRouter())
	defer ts.Close()

	// 1. Configure fail_3_times_then_200 fault injection mode on POST /api/v1/mock/fault-mode
	modeBody, _ := json.Marshal(map[string]string{"mode": "fail_3_times_then_200"})
	modeRes, err := http.Post(ts.URL+"/api/v1/mock/fault-mode", "application/json", bytes.NewReader(modeBody))
	if err != nil || modeRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK setting fault mode, got err=%v status=%d", err, modeRes.StatusCode)
	}
	_ = modeRes.Body.Close()

	// 2. Send 1:1 RevenueCat INITIAL_PURCHASE webhook -> attempts 1..3 return 500, attempt 4 returns 200 OK and unlocks episode + VIP
	rcWebhookPayload, _ := json.Marshal(map[string]any{
		"api_version": "1.0",
		"event": map[string]any{
			"id":              "rc_evt_cleanroom_01",
			"type":            "INITIAL_PURCHASE",
			"app_id":          "proj10308902_shortical",
			"app_user_id":     "uid_cleanroom_viewer_01",
			"product_id":      "shortical_vip_weekly_999",
			"entitlement_ids": []string{"vip"},
			"store":           "STRIPE",
			"price":           9.99,
			"currency":        "USD",
			"transaction_id":  "tx_cleanroom_01",
		},
	})

	for attempt := 1; attempt <= 4; attempt++ {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhooks/revenuecat", bytes.NewReader(rcWebhookPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer dev_s2s_hmac_secret_shortical_2026_primary")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("webhook request failed on attempt %d: %v", attempt, err)
		}
		_ = res.Body.Close()
		if attempt <= 3 && res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500 Internal Server Error on attempt %d, got %d", attempt, res.StatusCode)
		}
		if attempt == 4 && res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on 4th retry attempt, got %d", res.StatusCode)
		}
	}

	// 3. Poll GET /api/v1/mock/subscribers/uid_cleanroom_viewer_01 -> verify VIP & Episode Unlocked
	pollRes, err := http.Get(ts.URL + "/api/v1/mock/subscribers/uid_cleanroom_viewer_01")
	if err != nil || pollRes.StatusCode != http.StatusOK {
		t.Fatalf("poll subscriber state failed: err=%v status=%d", err, pollRes.StatusCode)
	}
	defer pollRes.Body.Close()
	var subState models.MockSubscriberWalletState
	_ = json.NewDecoder(pollRes.Body).Decode(&subState)
	if !subState.HasActiveVIP || !subState.EpisodeUnlocked || subState.ActivePlanID != "shortical_vip_weekly_999" {
		t.Fatalf("expected VIP active and episode unlocked after 4th retry, got %+v", subState)
	}

	// 4. Send NON_RENEWING_PURCHASE (100 coins) followed by CANCELLATION with CUSTOMER_SUPPORT -> verify wallet_clawback_applied
	svc.SetFaultMode("always_200_ok")
	coinsPayload, _ := json.Marshal(map[string]any{
		"api_version": "1.0",
		"event": map[string]any{
			"id":          "rc_evt_cleanroom_02",
			"type":        "NON_RENEWING_PURCHASE",
			"app_user_id": "uid_cleanroom_viewer_01",
			"product_id":  "shortical_coins_100",
			"store":       "APPLE",
		},
	})
	cReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhooks/revenuecat", bytes.NewReader(coinsPayload))
	cReq.Header.Set("Authorization", "Bearer dev_s2s_hmac_secret_shortical_2026_primary")
	cRes, _ := http.DefaultClient.Do(cReq)
	_ = cRes.Body.Close()

	refundPayload, _ := json.Marshal(map[string]any{
		"api_version": "1.0",
		"event": map[string]any{
			"id":            "rc_evt_cleanroom_03",
			"type":          "CANCELLATION",
			"app_user_id":   "uid_cleanroom_viewer_01",
			"product_id":    "shortical_coins_100",
			"cancel_reason": "CUSTOMER_SUPPORT",
		},
	})
	rReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhooks/revenuecat", bytes.NewReader(refundPayload))
	rReq.Header.Set("Authorization", "Bearer dev_s2s_hmac_secret_shortical_2026_primary")
	rRes, _ := http.DefaultClient.Do(rReq)
	_ = rRes.Body.Close()

	afterState := svc.GetSubscriberState("uid_cleanroom_viewer_01")
	if afterState.CoinsBalance != 0 || !afterState.WalletClawbackApplied || afterState.HasActiveVIP {
		t.Fatalf("expected coin wallet clawback and VIP revocation on CUSTOMER_SUPPORT refund, got %+v", afterState)
	}

	// 5. Verify Mock Gateway S2S Trigger (Full Source-of-Truth vs. Broken Deploy Quarantine) & Mobile JWS Harness
	mockGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/billing/s2s/authorize" {
			if commit, _ := body["commit_simulation"].(bool); commit {
				t.Errorf("authorize must not commit a gateway charge before the wrapper moves this subscriber in-house")
			}
			if _, hasCurr := body["currency"]; !hasCurr {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":          "contract_mismatch_quarantined",
					"quarantine_id": "quar_cleanroom_01",
				})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "bcs_cleanroom_01",
				"tax_source": "shortical_upstream_source_of_truth",
			})
			return
		}
		if r.URL.Path == "/api/v1/billing/client/mobile/verify-receipt" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"acknowledged":   true,
				"active_plan_id": body["product_id"],
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockGateway.Close()

	svcWithGW := services.NewHarnessService(mockGateway.URL, "dev_s2s_hmac_secret_shortical_2026_primary", "p8_sand_apple_storekit2_keyid_7721f")
	tsGW := httptest.NewServer(svcWithGW.NewHarnessRouter())
	defer tsGW.Close()

	// Mode 1: Full Upstream Source-of-Truth -> 200 OK
	s2sFullBody, _ := json.Marshal(models.S2SSimulatorRequest{
		Scenario:      "full_upstream_source_of_truth",
		SubscriberRef: "uid_s2s_cleanroom_01",
		Brand:         "Shortical",
		Provider:      "stripe",
		OfferingTag:   "vip",
		PlanID:        "shortical_vip_weekly_999",
		AmountCents:   999,
		Currency:      "USD",
	})
	s2sFullRes, err := http.Post(tsGW.URL+"/api/v1/mock/s2s/trigger", "application/json", bytes.NewReader(s2sFullBody))
	if err != nil {
		t.Fatalf("s2sFullRes failed: %v", err)
	}
	defer s2sFullRes.Body.Close()
	var s2sFullOut models.S2SSimulatorResponse
	_ = json.NewDecoder(s2sFullRes.Body).Decode(&s2sFullOut)
	if s2sFullOut.GatewayStatus != http.StatusOK || s2sFullOut.GatewayPayload["tax_source"] != "shortical_upstream_source_of_truth" {
		t.Fatalf("expected 200 OK for full_upstream_source_of_truth, got %+v", s2sFullOut)
	}

	// Mode 2: Broken Deploy (Missing Contract) -> 409 contract_mismatch_quarantined
	s2sBrokenBody, _ := json.Marshal(models.S2SSimulatorRequest{
		Scenario:      "broken_deploy_missing_contract",
		SubscriberRef: "uid_s2s_cleanroom_02",
		Brand:         "Shortical",
		Provider:      "stripe",
	})
	s2sBrokenRes, err := http.Post(tsGW.URL+"/api/v1/mock/s2s/trigger", "application/json", bytes.NewReader(s2sBrokenBody))
	if err != nil {
		t.Fatalf("s2sBrokenRes failed: %v", err)
	}
	defer s2sBrokenRes.Body.Close()
	var s2sBrokenOut models.S2SSimulatorResponse
	_ = json.NewDecoder(s2sBrokenRes.Body).Decode(&s2sBrokenOut)
	if s2sBrokenOut.GatewayStatus != http.StatusConflict || s2sBrokenOut.GatewayPayload["code"] != "contract_mismatch_quarantined" {
		t.Fatalf("expected 409 contract_mismatch_quarantined for broken_deploy_missing_contract, got %+v", s2sBrokenOut)
	}

	// Mobile Harness JWS Simulation -> 200 OK
	mobBody, _ := json.Marshal(models.MobileHarnessSimulateRequest{
		SubscriberRef: "uid_kmp_cleanroom_01",
		Brand:         "Shortical",
		Provider:      "apple",
		ProductID:     "com.shortical.coins.gold",
	})
	mobRes, err := http.Post(tsGW.URL+"/api/v1/mock/mobile/simulate", "application/json", bytes.NewReader(mobBody))
	if err != nil {
		t.Fatalf("mobRes failed: %v", err)
	}
	defer mobRes.Body.Close()
	if mobRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from mobile harness simulation, got %d", mobRes.StatusCode)
	}

	// Verify /healthz and /storefront
	hzRes, err := http.Get(tsGW.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer hzRes.Body.Close()
	sfRes, err := http.Get(tsGW.URL + "/storefront")
	if err != nil {
		t.Fatalf("GET /storefront failed: %v", err)
	}
	defer sfRes.Body.Close()
	if hzRes.StatusCode != http.StatusOK || sfRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /healthz and /storefront, got %d %d", hzRes.StatusCode, sfRes.StatusCode)
	}

	// OWASP A01/A02/A05: Reject forged webhook signatures/tokens and block arbitrary CORS Origin reflection
	forgedWhReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhooks/revenuecat", bytes.NewReader(rcWebhookPayload))
	forgedWhReq.Header.Set("Content-Type", "application/json")
	forgedWhReq.Header.Set("X-Billing-Signature", "forged_nonempty_signature")
	forgedWhRes, err := http.DefaultClient.Do(forgedWhReq)
	if err != nil {
		t.Fatalf("forgedWhReq failed: %v", err)
	}
	defer forgedWhRes.Body.Close()
	if forgedWhRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on forged X-Billing-Signature, got %d", forgedWhRes.StatusCode)
	}

	forgedBearerReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhooks/revenuecat", bytes.NewReader(rcWebhookPayload))
	forgedBearerReq.Header.Set("Authorization", "Bearer wrong_bearer_secret")
	forgedBearerRes, err := http.DefaultClient.Do(forgedBearerReq)
	if err != nil {
		t.Fatalf("forgedBearerReq failed: %v", err)
	}
	defer forgedBearerRes.Body.Close()
	if forgedBearerRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on wrong Bearer token, got %d", forgedBearerRes.StatusCode)
	}

	evilCorsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	evilCorsReq.Header.Set("Origin", "https://evil-attacker.example")
	evilCorsRes, err := http.DefaultClient.Do(evilCorsReq)
	if err != nil {
		t.Fatalf("evilCorsReq failed: %v", err)
	}
	defer evilCorsRes.Body.Close()
	if gotOrigin := evilCorsRes.Header.Get("Access-Control-Allow-Origin"); gotOrigin != "" {
		t.Fatalf("expected evil Origin not to be reflected in CORS header, got %q", gotOrigin)
	}
	if evilCorsRes.Header.Get("Strict-Transport-Security") == "" || evilCorsRes.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected hardened security headers on mock-server responses")
	}
}
