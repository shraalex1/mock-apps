package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"shortical.com/mock-server/src/models"
)

// HarnessService coordinates the 3 Clean-Room Mock components:
// 1. mock-api-service (RC 1:1 webhook receiver with 5-retry fault injection + S2S caller)
// 2. mock-web-storefront (Locked episode checkout & 20s polling simulator)
// 3. mock-mobile-harness (StoreKit 2 JWS / ASSN v2 / Play v8 / RTDN / KMP Wrapper SDK simulator)
type HarnessService struct {
	mu                  sync.RWMutex
	gatewayURL          string
	s2sSecret           string
	appleSigningSecret  string
	appleBundleID       string
	googlePackageName   string
	googleOIDCAudience  string
	faultConfig         models.WebhookFaultInjectionConfig
	walletsBySubscriber map[string]models.MockSubscriberWalletState
}

// NewHarnessService initializes a thread-safe Clean-Room HarnessService.
func NewHarnessService(gatewayURL, s2sSecret, appleSigningSecret string) *HarnessService {
	if strings.TrimSpace(gatewayURL) == "" {
		gatewayURL = envOr("BILLING_GATEWAY_URL", "http://localhost:4460")
	}
	if strings.TrimSpace(s2sSecret) == "" {
		s2sSecret = envOr("S2S_HMAC_SECRET_CURRENT", "dev_s2s_hmac_secret_shortical_2026_primary")
	}
	if strings.TrimSpace(appleSigningSecret) == "" {
		appleSigningSecret = envOr("APPLE_OFFER_SIGNING_SECRET", "p8_sand_apple_storekit2_keyid_7721f")
	}
	return &HarnessService{
		gatewayURL:         strings.TrimRight(gatewayURL, "/"),
		s2sSecret:          s2sSecret,
		appleSigningSecret: appleSigningSecret,
		appleBundleID:      envOr("APPLE_BUNDLE_ID", "com.shortical.app"),
		googlePackageName:  envOr("GOOGLE_PACKAGE_NAME", "com.shortical.app"),
		googleOIDCAudience: envOr("GOOGLE_RTDN_OIDC_AUDIENCE", strings.TrimRight(gatewayURL, "/")+"/api/v1/billing/webhooks/google"),
		faultConfig: models.WebhookFaultInjectionConfig{
			Mode: envOr("MOCK_WEBHOOK_RESPONSE_MODE", "always_200_ok"),
		},
		walletsBySubscriber: make(map[string]models.MockSubscriberWalletState),
	}
}

// SetFaultMode updates the simulated HTTP response mode on POST /webhooks/revenuecat.
func (h *HarnessService) SetFaultMode(mode string) models.WebhookFaultInjectionConfig {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.faultConfig.Mode = strings.TrimSpace(mode)
	h.faultConfig.FailedAttemptsCounter = 0
	return h.faultConfig
}

// GetSubscriberState returns the current VIP & Coin wallet state for a subscriber_ref (polled by Web & Mobile clients).
func (h *HarnessService) GetSubscriberState(subscriberRef string) models.MockSubscriberWalletState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st, ok := h.walletsBySubscriber[strings.TrimSpace(subscriberRef)]
	if !ok {
		return models.MockSubscriberWalletState{
			SubscriberRef:    strings.TrimSpace(subscriberRef),
			Brand:            "Shortical",
			HasActiveVIP:     false,
			CoinsBalance:     0,
			EpisodeUnlocked:  false,
			ReceivedRCEvents: []string{},
			UpdatedAtUTC:     time.Now().UTC().Format(time.RFC3339),
		}
	}
	return st
}

// HandleRevenueCatWebhook receives POST /webhooks/revenuecat from alt-payments,
// applies fault-injection rules (to test the 5-retry exponential backoff), and updates VIP/coin state.
func (h *HarnessService) HandleRevenueCatWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify HMAC or Bearer authorization header
	sig := strings.TrimSpace(r.Header.Get("X-Billing-Signature"))
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if sig == "" && !strings.HasPrefix(auth, "Bearer ") {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing signature or bearer token"}`))
		return
	}

	h.mu.Lock()
	mode := h.faultConfig.Mode
	if mode == "always_500_error" {
		h.faultConfig.FailedAttemptsCounter++
		h.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"simulated always_500_error from mock-api-service"}`))
		return
	}
	if mode == "fail_3_times_then_200" && h.faultConfig.FailedAttemptsCounter < 3 {
		h.faultConfig.FailedAttemptsCounter++
		attempt := h.faultConfig.FailedAttemptsCounter
		h.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"simulated HTTP 500 on attempt %d/3"}`, attempt)))
		return
	}
	h.faultConfig.FailedAttemptsCounter = 0
	h.faultConfig.TotalWebhooksReceived++
	h.mu.Unlock()

	var env models.IncomingRCWebhookEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	subRef := strings.TrimSpace(env.Event.AppUserID)
	if subRef == "" {
		subRef = "uid_rc_webhook_user"
	}
	evtType := strings.ToUpper(strings.TrimSpace(env.Event.Type))
	nowStr := time.Now().UTC().Format(time.RFC3339)

	h.mu.Lock()
	st := h.walletsBySubscriber[subRef]
	st.SubscriberRef = subRef
	st.Brand = "Shortical"
	st.LastWebhookID = env.Event.ID
	st.ReceivedRCEvents = append(st.ReceivedRCEvents, evtType)

	switch evtType {
	case "INITIAL_PURCHASE", "RENEWAL":
		st.HasActiveVIP = true
		st.EpisodeUnlocked = true
		st.ActivePlanID = env.Event.ProductID
		st.PendingPlanID = ""
	case "NON_RENEWING_PURCHASE":
		st.CoinsBalance += 100
		st.EpisodeUnlocked = true
	case "PRODUCT_CHANGE":
		st.HasActiveVIP = true
		if env.Event.NewProductID != "" {
			st.PendingPlanID = env.Event.NewProductID
		}
	case "CANCELLATION", "EXPIRATION":
		if strings.EqualFold(env.Event.CancelReason, "CUSTOMER_SUPPORT") {
			st.HasActiveVIP = false
			st.EpisodeUnlocked = false
			if st.CoinsBalance > 0 {
				st.CoinsBalance = 0
				st.WalletClawbackApplied = true
			}
		}
	}
	st.UpdatedAtUTC = nowStr
	h.walletsBySubscriber[subRef] = st
	h.mu.Unlock()

	// When an external RevenueCat cloud webhook arrives directly at mock-api-service (no X-Billing-Signature from PaymentGW),
	// shadow-forward it to PaymentGW (/api/v1/billing/webhooks/revenuecat) so INVOICE_ISSUANCE (rcbin..., $9.99) & renewals stay 100% synced!
	if sig == "" && strings.TrimSpace(h.gatewayURL) != "" {
		go func(raw []byte, gw string) {
			client := &http.Client{Timeout: 4 * time.Second}
			fwdReq, fwdErr := http.NewRequest(http.MethodPost, gw+"/api/v1/billing/webhooks/revenuecat", bytes.NewReader(raw))
			if fwdErr == nil {
				fwdReq.Header.Set("Content-Type", "application/json")
				if resp, doErr := client.Do(fwdReq); doErr == nil && resp != nil {
					_ = resp.Body.Close()
				}
			}
		}(append([]byte(nil), body...), h.gatewayURL)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         "ok",
		"subscriber_ref": subRef,
		"rc_event_type":  evtType,
		"vip_active":     st.HasActiveVIP,
		"coins_balance":  st.CoinsBalance,
	})
}

// wrapperDecision is the same gate as GatewaySessionConfig.canRouteChargeToInhouse.
type wrapperDecision struct {
	Answered bool
	Inhouse  bool
	Session  map[string]any
}

func (h *HarnessService) lookupWrapperRoute(subRef, brand, platform string) wrapperDecision {
	body, _ := json.Marshal(map[string]any{
		"subscriber_ref":   subRef,
		"brand":            brand,
		"contract_version": "1.4.0",
		"client_platform":  platform,
	})
	req, err := http.NewRequest(http.MethodPost, h.gatewayURL+"/api/v1/billing/client/initialize", bytes.NewReader(body))
	if err != nil {
		return wrapperDecision{}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+subRef)
	req.Header.Set("X-Billing-SDK-Version", "1.4.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return wrapperDecision{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return wrapperDecision{}
	}
	var session map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return wrapperDecision{}
	}
	return wrapperDecision{Answered: true, Inhouse: routeChargesInhouse(session), Session: session}
}

func routeChargesInhouse(session map[string]any) bool {
	if session["mode"] != "transition" || session["routed_target"] != "inhouse_gateway" {
		return false
	}
	if fallback, ok := session["fallback_to_legacy_rc"].(bool); ok && fallback {
		return false
	}
	caps, _ := session["server_capabilities"].([]any)
	for _, cap := range caps {
		if cap == "charge" {
			return true
		}
	}
	return false
}

func (h *HarnessService) mirrorCompletedPurchase(subRef, brand, provider, offering, planID string, amountCents int64, currency, countryCode, invoiceID string) map[string]any {
	expires := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	if amountCents <= 0 {
		amountCents = 999
	}
	if strings.TrimSpace(currency) == "" {
		currency = "USD"
	}
	body, _ := json.Marshal(map[string]any{
		"subscriber_ref":       subRef,
		"brand":                brand,
		"provider":             provider,
		"mirror_kind":          "completed_purchase",
		"offering_tag":         offering,
		"plan_id":              planID,
		"amount_cents":         amountCents,
		"currency":             strings.ToUpper(strings.TrimSpace(currency)),
		"country_code":         strings.ToUpper(strings.TrimSpace(countryCode)),
		"invoice_id":           strings.TrimSpace(invoiceID),
		"rc_entitlement_state": "active",
		"rc_expires_at_utc":    expires,
	})
	req, err := http.NewRequest(http.MethodPost, h.gatewayURL+"/api/v1/billing/client/mirror-to-test", bytes.NewReader(body))
	if err != nil {
		return map[string]any{"status": "mirror_not_sent", "error": err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+subRef)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]any{"status": "mirror_not_sent", "error": err.Error()}
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out == nil {
		out = map[string]any{}
	}
	out["http_status"] = resp.StatusCode
	return out
}

func (h *HarnessService) recordContainedPurchase(subRef, brand, planID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.walletsBySubscriber[subRef]
	st.SubscriberRef = subRef
	st.Brand = brand
	st.HasActiveVIP = true
	st.EpisodeUnlocked = true
	st.ActivePlanID = planID
	st.ReceivedRCEvents = append(st.ReceivedRCEvents, "INITIAL_PURCHASE")
	st.UpdatedAtUTC = time.Now().UTC().Format(time.RFC3339)
	h.walletsBySubscriber[subRef] = st
}

// ExecuteS2SScenario runs one of the 3 S2S upstream modes against billing-web (/api/v1/billing/s2s/authorize).
func (h *HarnessService) ExecuteS2SScenario(req models.S2SSimulatorRequest) (models.S2SSimulatorResponse, error) {
	subRef := strings.TrimSpace(req.SubscriberRef)
	if subRef == "" {
		subRef = "uid_mock_s2s_user"
	}
	brand := strings.TrimSpace(req.Brand)
	if brand == "" {
		brand = "Shortical"
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "stripe"
	}
	offTag := strings.TrimSpace(req.OfferingTag)
	if offTag == "" {
		offTag = "vip"
	}

	route := wrapperDecision{}
	if strings.ToLower(strings.TrimSpace(req.Scenario)) != "broken_deploy_missing_contract" {
		route = h.lookupWrapperRoute(subRef, brand, "web")
	}

	var payload map[string]any
	switch strings.ToLower(strings.TrimSpace(req.Scenario)) {
	case "broken_deploy_missing_contract":
		// Intentionally omit currency, amount_cents, and plan_id with require_upstream_contract=true
		payload = map[string]any{
			"subscriber_ref":            subRef,
			"brand":                     brand,
			"provider":                  provider,
			"offering_tag":              offTag,
			"require_upstream_contract": true,
		}
	default:
		amt := req.AmountCents
		if amt <= 0 {
			amt = 999
		}
		curr := strings.ToUpper(strings.TrimSpace(req.Currency))
		if curr == "" {
			curr = "USD"
		}
		planID := strings.TrimSpace(req.PlanID)
		if planID == "" {
			planID = strings.ToLower(brand) + "_vip_weekly_999"
		}
		tax := int64(80)
		if req.UpstreamTaxCents != nil {
			tax = *req.UpstreamTaxCents
		}
		payload = map[string]any{
			"subscriber_ref":            subRef,
			"brand":                     brand,
			"origin":                    req.Origin,
			"provider":                  provider,
			"offering_tag":              offTag,
			"plan_id":                   planID,
			"amount_cents":              amt,
			"upstream_tax_cents":        tax,
			"currency":                  curr,
			"is_amex_routing":           req.IsAmexRouting,
			"require_upstream_contract": true,
			"commit_simulation":         route.Inhouse,
		}
	}

	rawBody, _ := json.Marshal(payload)
	path := "/api/v1/billing/s2s/authorize"
	tsStr := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	sig := ComputeS2SSignature(h.s2sSecret, tsStr, http.MethodPost, path, rawBody)

	httpReq, err := http.NewRequest(http.MethodPost, h.gatewayURL+path, bytes.NewReader(rawBody))
	if err != nil {
		return models.S2SSimulatorResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Billing-Timestamp", tsStr)
	httpReq.Header.Set("X-Billing-Signature", sig)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return models.S2SSimulatorResponse{}, err
	}
	defer resp.Body.Close()

	var gwBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&gwBody)
	if gwBody == nil {
		gwBody = map[string]any{}
	}
	if route.Inhouse {
		gwBody["wrapper_route"] = "inhouse_gateway"
		gwBody["charge_committed"] = resp.StatusCode >= 200 && resp.StatusCode < 300
	} else if route.Answered {
		gwBody["wrapper_route"] = route.Session["routed_target"]
		gwBody["charge_committed"] = false
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			planID, _ := payload["plan_id"].(string)
			offering, _ := payload["offering_tag"].(string)
			amtCents, _ := payload["amount_cents"].(int64)
			currStr, _ := payload["currency"].(string)
			if mirror, _ := route.Session["mirror_to_test"].(bool); mirror {
				gwBody["mirror"] = h.mirrorCompletedPurchase(subRef, brand, provider, offering, planID, amtCents, currStr, "", "")
			}
			h.recordContainedPurchase(subRef, brand, planID)
		}
	} else if strings.ToLower(strings.TrimSpace(req.Scenario)) != "broken_deploy_missing_contract" {
		gwBody["wrapper_route"] = "legacy_rc_offline_fallback"
		gwBody["charge_committed"] = false
	}

	return models.S2SSimulatorResponse{
		Scenario:       req.Scenario,
		GatewayStatus:  resp.StatusCode,
		GatewayPayload: gwBody,
		ExecutedAtUTC:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// ComputeS2SSignature computes HMAC-SHA256(secret, timestamp + "." + method + "." + path + "." + sha256(body)).
func ComputeS2SSignature(secret, timestamp, method, path string, body []byte) string {
	sum := sha256.Sum256(body)
	canonical := fmt.Sprintf("%s.%s.%s.%s", timestamp, strings.ToUpper(method), path, hex.EncodeToString(sum[:]))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// SignStoreKit2JWS signs a 3-part Apple StoreKit 2 / ASSN v2 JWS token for Clean-Room testing.
func SignStoreKit2JWS(secret string, payload map[string]any) string {
	headerBytes, _ := json.Marshal(map[string]any{
		"alg": "HS256",
		"typ": "JWT",
		"x5c": []string{"MIIB+TCCAZ+gAwIBAgIJAJStoreKit2SandboxCert"},
	})
	payloadBytes, _ := json.Marshal(payload)
	input := base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(payloadBytes)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
