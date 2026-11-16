package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shortical.com/mock-server/src/models"
)

// NewHarnessRouter mounts all Shortical mock-server (api-service) endpoints:
// - POST /webhooks/revenuecat (1:1 RC webhook receiver with 5-retry fault injection)
// - GET  /api/v1/monetization/user-state/{subscriber_ref} & /api/v1/mock/subscribers/{subscriber_ref}
// - POST /api/v1/monetization/stripe/create-subscription & /create-payment-intent (calls alt-payments /s2s/authorize)
// - POST /api/v1/monetization/subscription/web/should-link & /link (attachWebPurchase email linking)
// - POST /api/v1/monetization/vip-gems/redeem & /api/v1/series/{id}/eos (calls alt-payments /s2s/entitlement-extension)
func (h *HarnessService) NewHarnessRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		h.mu.RLock()
		cfg := h.faultConfig
		h.mu.RUnlock()
		resp := map[string]any{
			"status":               "ok",
			"service":              "shortical-mock-server",
			"fault_injection_mode": cfg.Mode,
			"webhooks_received":    cfg.TotalWebhooksReceived,
			"timestamp_utc":        time.Now().UTC().Format(time.RFC3339),
		}
		if h.appEnv == "" || h.appEnv == "local" {
			resp["billing_gateway_url"] = h.gatewayURL
		}
		writeJSON(w, http.StatusOK, resp)
	})

	// 1. 1:1 RevenueCat Webhook Receiver (with 5-retry fault injection)
	mux.HandleFunc("POST /webhooks/revenuecat", h.HandleRevenueCatWebhook)

	// 1B. Clean-Room Contained RevenueCat + mirror_to_test Bridge (used when Wrapper SDK routes to RC in test/0% mode)
	mux.HandleFunc("POST /api/v1/mock/rc/contained-mirror", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SubscriberRef string `json:"subscriber_ref"`
			Brand         string `json:"brand"`
			Provider      string `json:"provider"`
			OfferingTag   string `json:"offering_tag"`
			PlanID        string `json:"plan_id"`
			AmountCents   int64  `json:"amount_cents"`
			Currency      string `json:"currency"`
			CountryCode   string `json:"country_code"`
			InvoiceID     string `json:"invoice_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		subRef := strings.TrimSpace(req.SubscriberRef)
		if subRef == "" {
			subRef = "uid_rc_contained_web_01"
		}
		brand := strings.TrimSpace(req.Brand)
		if brand == "" {
			brand = "Shortical"
		}
		prov := strings.TrimSpace(req.Provider)
		if prov == "" {
			prov = "stripe"
		}
		off := strings.TrimSpace(req.OfferingTag)
		if off == "" {
			off = "vip"
		}
		plan := strings.TrimSpace(req.PlanID)
		if plan == "" {
			plan = "shortical_vip_weekly"
		}
		h.recordContainedPurchase(subRef, brand, plan)
		mirrorOut := h.mirrorCompletedPurchase(subRef, brand, prov, off, plan, req.AmountCents, req.Currency, req.CountryCode, req.InvoiceID)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "mirrored_from_contained_rc",
			"subscriber_ref": subRef,
			"plan_id":        plan,
			"mirror":         mirrorOut,
		})
	})

	// 1C. S2S-Signed Contained RC Sync Proxy (calls POST /api/v1/billing/sandbox/rc-contained-sync with S2S HMAC)
	mux.HandleFunc("POST /api/v1/mock/rc/contained-sync", func(w http.ResponseWriter, r *http.Request) {
		raw, readErr := readBoundedMockBody(w, r, maxMockJSONBodyBytes)
		if readErr != nil {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "payload_too_large"})
			return
		}
		path := "/api/v1/billing/sandbox/rc-contained-sync"
		tsStr := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		sig := ComputeS2SSignature(h.s2sSecret, tsStr, http.MethodPost, path, raw)
		httpReq, reqErr := http.NewRequest(http.MethodPost, h.gatewayURL+path, bytes.NewReader(raw))
		if reqErr != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "gateway_request_failed"})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Billing-Timestamp", tsStr)
		httpReq.Header.Set("X-Billing-Signature", sig)
		resp, doErr := h.httpClient.Do(httpReq)
		if doErr != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "gateway_unreachable"})
			return
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if out == nil {
			out = map[string]any{}
		}
		writeJSON(w, resp.StatusCode, out)
	})

	// 2. Configure simulated HTTP response mode (always_200_ok | fail_3_times_then_200 | always_500_error)
	mux.HandleFunc("POST /api/v1/mock/fault-mode", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		updated := h.SetFaultMode(req.Mode)
		writeJSON(w, http.StatusOK, updated)
	})

	// 3. Polled by shortical-mock-web-client (20s poll) & shortical-mock-mobile-client (10s poll)
	mux.HandleFunc("GET /api/v1/mock/subscribers/{subscriber_ref}", func(w http.ResponseWriter, r *http.Request) {
		subRef := r.PathValue("subscriber_ref")
		writeJSON(w, http.StatusOK, h.GetSubscriberState(subRef))
	})
	mux.HandleFunc("GET /api/v1/monetization/user-state/{subscriber_ref}", func(w http.ResponseWriter, r *http.Request) {
		subRef := r.PathValue("subscriber_ref")
		writeJSON(w, http.StatusOK, h.GetSubscriberState(subRef))
	})

	// 4. S2S Authorize & Web Stripe Subscription / PaymentIntent Creation (SE-3067 + discount_code)
	mux.HandleFunc("POST /api/v1/mock/s2s/trigger", func(w http.ResponseWriter, r *http.Request) {
		var req models.S2SSimulatorRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
			return
		}
		res, err := h.ExecuteS2SScenario(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("POST /api/v1/monetization/stripe/create-subscription", func(w http.ResponseWriter, r *http.Request) {
		var req models.S2SSimulatorRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Scenario == "" {
			req.Scenario = "full_upstream_source_of_truth"
		}
		res, err := h.ExecuteS2SScenario(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, res.GatewayStatus, res.GatewayPayload)
	})

	mux.HandleFunc("POST /api/v1/monetization/stripe/create-payment-intent", func(w http.ResponseWriter, r *http.Request) {
		var req models.S2SSimulatorRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		req.OfferingTag = "coins"
		res, err := h.ExecuteS2SScenario(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, res.GatewayStatus, res.GatewayPayload)
	})

	// 5. Web-to-App Subscription Linking (attachWebPurchase via email — kept unchanged on api-service)
	mux.HandleFunc("POST /api/v1/monetization/subscription/web/should-link", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"should_link": true,
			"reason":      "unlinked_web_subscription_found_for_email",
		})
	})

	mux.HandleFunc("POST /api/v1/monetization/subscription/web/link", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SubscriberRef string `json:"subscriber_ref"`
			Email         string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		subRef := strings.TrimSpace(req.SubscriberRef)
		if subRef == "" {
			subRef = "uid_linked_web_user"
		}
		h.mu.Lock()
		st := h.walletsBySubscriber[subRef]
		st.SubscriberRef = subRef
		st.Brand = "Shortical"
		st.HasActiveVIP = true
		st.EpisodeUnlocked = true
		st.ActivePlanID = "shortical_vip_web_linked"
		st.UpdatedAtUTC = time.Now().UTC().Format(time.RFC3339)
		h.walletsBySubscriber[subRef] = st
		h.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"linked":         true,
			"subscriber_ref": subRef,
			"email":          req.Email,
			"vip_active":     true,
		})
	})

	// 6. VIP Gems Redemption (/vip-gems/redeem) & End-of-Series (/series/{id}/eos)
	mux.HandleFunc("POST /api/v1/monetization/vip-gems/redeem", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SubscriberRef string `json:"subscriber_ref"`
			ExtendDays    int    `json:"extend_days"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		subRef := strings.TrimSpace(req.SubscriberRef)
		if subRef == "" {
			subRef = "uid_vip_gems_user"
		}
		h.mu.Lock()
		st := h.walletsBySubscriber[subRef]
		st.SubscriberRef = subRef
		st.HasActiveVIP = true
		st.EpisodeUnlocked = true
		st.ActivePlanID = "shortical_vip_gems_extension"
		st.UpdatedAtUTC = time.Now().UTC().Format(time.RFC3339)
		h.walletsBySubscriber[subRef] = st
		h.mu.Unlock()
		writeJSON(w, http.StatusOK, st)
	})

	// 7. Mobile StoreKit 2 JWS / Play Billing v8 Receipt Generator & Verifier
	mux.HandleFunc("POST /api/v1/mock/mobile/simulate", func(w http.ResponseWriter, r *http.Request) {
		var req models.MobileHarnessSimulateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
			return
		}
		status, out, err := h.ExecuteMobileHarnessFlow(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, status, out)
	})

	mux.HandleFunc("GET /storefront", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; object-src 'none'; frame-ancestors 'self'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderMockStorefrontHTML(h.gatewayURL)))
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyMockSecurityHeaders(w)
		defer func() {
			if recovered := recover(); recovered != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "an unexpected internal error occurred", "code": "internal_error"})
			}
		}()
		if origin := r.Header.Get("Origin"); origin != "" && h.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token, X-Billing-Timestamp, X-Billing-Signature, X-Billing-SDK-Version")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/healthz" {
			allowed, remaining := h.limiter.allow(extractMockClientIP(r), time.Now().UTC())
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			if !allowed {
				writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded", "code": "too_many_requests"})
				return
			}
		}
		if r.Body != nil && r.URL.Path != "/webhooks/revenuecat" {
			r.Body = http.MaxBytesReader(w, r.Body, maxMockJSONBodyBytes)
		}
		mux.ServeHTTP(w, r)
		if r.URL.Path != "/healthz" {
			fmt.Printf("[mock-api-service :4470] %s %s %s\n", time.Now().UTC().Format("15:04:05"), r.Method, r.URL.Path)
		}
	})
}

// ExecuteMobileHarnessFlow generates a signed StoreKit 2 JWS or Google Play v8 token and submits it to billing-web.
func (h *HarnessService) ExecuteMobileHarnessFlow(req models.MobileHarnessSimulateRequest) (int, map[string]any, error) {
	subRef := strings.TrimSpace(req.SubscriberRef)
	if subRef == "" {
		subRef = "uid_kmp_harness_user"
	}
	brand := strings.TrimSpace(req.Brand)
	if brand == "" {
		brand = "Shortical"
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "apple"
	}
	prodID := strings.TrimSpace(req.ProductID)
	if prodID == "" {
		prodID = "com.shortical.coins.gold"
	}

	route := h.lookupWrapperRoute(subRef, brand, map[string]string{"google": "android", "apple": "ios"}[provider])
	if !route.Inhouse {
		out := map[string]any{
			"wrapper_route":    "legacy_rc_offline_fallback",
			"charge_committed": false,
			"subscriber_ref":   subRef,
			"product_id":       prodID,
		}
		if route.Answered {
			out["wrapper_route"] = route.Session["routed_target"]
			if mirror, _ := route.Session["mirror_to_test"].(bool); mirror {
				offering := "vip"
				if strings.Contains(strings.ToLower(prodID), "coin") && !strings.Contains(prodID, "coins_based") {
					offering = "coins"
				}
				out["mirror"] = h.mirrorCompletedPurchase(subRef, brand, provider, offering, prodID, req.AmountCents, "USD", "", "")
			}
			h.recordContainedPurchase(subRef, brand, prodID)
		}
		return http.StatusOK, out, nil
	}

	verifyPayload := map[string]any{
		"subscriber_ref": subRef,
		"brand":          brand,
		"provider":       provider,
		"product_id":     prodID,
		"offer_token":    req.OfferToken,
	}

	uniqueSuffix := fmt.Sprintf("%s_%d", subRef, time.Now().UnixMilli()%1000000)
	if provider == "apple" {
		verifyPayload["signed_jws"] = SignStoreKit2JWS(h.appleSigningSecret, map[string]any{
			"transactionId":   "2000000_" + uniqueSuffix,
			"bundleId":        h.appleBundleID,
			"productId":       prodID,
			"appAccountToken": subRef,
			"environment":     "Sandbox",
			"price":           19990,
			"currency":        "USD",
		})
	} else {
		verifyPayload["purchase_token"] = "gp_v8_harness_tok_" + uniqueSuffix
	}

	raw, _ := json.Marshal(verifyPayload)
	httpReq, err := http.NewRequest(http.MethodPost, h.gatewayURL+"/api/v1/billing/client/mobile/verify-receipt", bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+SignClientBearerJWT(h.s2sSecret, subRef, time.Now().UTC()))

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out == nil {
		out = map[string]any{}
	}
	out["wrapper_route"] = "inhouse_gateway"
	out["charge_committed"] = resp.StatusCode == http.StatusOK
	if resp.StatusCode == http.StatusOK {
		h.mu.Lock()
		st := h.walletsBySubscriber[subRef]
		st.SubscriberRef = subRef
		st.Brand = brand
		st.HasActiveVIP = true
		st.EpisodeUnlocked = true
		st.ActivePlanID = prodID
		st.ReceivedRCEvents = append(st.ReceivedRCEvents, "INITIAL_PURCHASE:"+prodID)
		st.UpdatedAtUTC = time.Now().UTC().Format(time.RFC3339)
		h.walletsBySubscriber[subRef] = st
		h.mu.Unlock()
	}
	return resp.StatusCode, out, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func renderMockStorefrontHTML(gatewayURL string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <title>Shortical Clean-Room Mock Server</title>
</head>
<body style="background:#0d0e26;color:#f8fafc;font-family:system-ui,sans-serif;padding:24px;">
  <h1>Shortical Clean-Room Mock Server (api-service :4470)</h1>
  <p>Connected Gateway: <code>` + html.EscapeString(gatewayURL) + `</code> (100% Isolated from Production)</p>
</body>
</html>`
}
