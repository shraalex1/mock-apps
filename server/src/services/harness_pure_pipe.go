package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ExecuteSDKModeHandshake tests SDK mode alignment (standalone_pure_pipe vs wrapper_mirror) against POST /api/v1/billing/client/initialize.
func (h *HarnessService) ExecuteSDKModeHandshake(subRef, brand, sdkMode, contractVersion, platform string) (int, map[string]any, error) {
	if strings.TrimSpace(subRef) == "" {
		subRef = "uid_sdk_mode_probe_01"
	}
	if strings.TrimSpace(brand) == "" {
		brand = "Shortical"
	}
	if strings.TrimSpace(sdkMode) == "" {
		sdkMode = "standalone_pure_pipe"
	}
	if strings.TrimSpace(contractVersion) == "" {
		if sdkMode == "standalone_pure_pipe" {
			contractVersion = "1.4.0-pure-pipe"
		} else {
			contractVersion = "1.4.0"
		}
	}
	if strings.TrimSpace(platform) == "" {
		platform = "ios"
	}
	body, _ := json.Marshal(map[string]any{
		"subscriber_ref":   subRef,
		"brand":            brand,
		"sdk_mode":         sdkMode,
		"contract_version": contractVersion,
		"client_platform":  platform,
	})
	req, err := http.NewRequest(http.MethodPost, h.gatewayURL+"/api/v1/billing/client/initialize", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+SignClientBearerJWT(h.s2sSecret, subRef, time.Now().UTC()))
	req.Header.Set("X-Billing-SDK-Mode", sdkMode)
	req.Header.Set("X-Billing-SDK-Version", contractVersion)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var session map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&session)
	if session == nil {
		session = map[string]any{}
	}
	return resp.StatusCode, session, nil
}

// ExecuteRCPipeCoinVerify simulates Scenario 1 (api-service calls RevTiger POST /v1/receipts + GET /v1/subscribers/{app_user_id} for coin operations with an opaque unmapped SKU)
// and optionally passes X-Billing-Expected-Operational-Mode / expected_operational_mode to verify the S2S Mode-Attestation layer.
func (h *HarnessService) ExecuteRCPipeCoinVerify(subRef, productID, currency, platform, brand, expectedOpMode string, price float64) (int, map[string]any, error) {
	if strings.TrimSpace(subRef) == "" {
		subRef = "uid_coin_pipe_buyer_01"
	}
	if strings.TrimSpace(productID) == "" {
		productID = "com.shortical.coins.unmapped_pack_9999"
	}
	if strings.TrimSpace(currency) == "" {
		currency = "USD"
	}
	if strings.TrimSpace(platform) == "" {
		platform = "android"
	}
	if strings.TrimSpace(brand) == "" {
		brand = "Shortical"
	}
	if price <= 0 {
		price = 19.99
	}
	payloadMap := map[string]any{
		"app_user_id": subRef,
		"fetch_token": fmt.Sprintf("pt_pipe_%s_%d", subRef, time.Now().UnixMilli()%100000),
		"product_id":  productID,
		"price":       price,
		"currency":    currency,
		"platform":    platform,
		"brand":       brand,
	}
	if strings.TrimSpace(expectedOpMode) != "" {
		payloadMap["expected_operational_mode"] = strings.TrimSpace(expectedOpMode)
	}
	payload, _ := json.Marshal(payloadMap)
	path := "/v1/receipts"
	tsStr := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	sig := ComputeS2SSignature(h.s2sSecret, tsStr, http.MethodPost, path, payload)
	req, err := http.NewRequest(http.MethodPost, h.gatewayURL+path, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Billing-Timestamp", tsStr)
	req.Header.Set("X-Billing-Signature", sig)
	if strings.TrimSpace(expectedOpMode) != "" {
		req.Header.Set("X-Billing-Expected-Operational-Mode", strings.TrimSpace(expectedOpMode))
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var receiptOut map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&receiptOut)

	var subOut map[string]any
	if resp.StatusCode == http.StatusOK {
		subReq, subErr := http.NewRequest(http.MethodGet, h.gatewayURL+"/v1/subscribers/"+subRef, nil)
		if subErr == nil {
			subReq.Header.Set("Authorization", "Bearer "+h.s2sSecret)
			if subResp, doErr := h.httpClient.Do(subReq); doErr == nil && subResp != nil {
				_ = json.NewDecoder(subResp.Body).Decode(&subOut)
				_ = subResp.Body.Close()
			}
		}
		h.mu.Lock()
		st := h.walletsBySubscriber[subRef]
		st.SubscriberRef = subRef
		st.Brand = brand
		st.CoinsBalance += 100
		st.EpisodeUnlocked = true
		st.ActivePlanID = productID
		st.ReceivedRCEvents = append(st.ReceivedRCEvents, "NON_RENEWING_PURCHASE:"+productID)
		st.UpdatedAtUTC = time.Now().UTC().Format(time.RFC3339)
		h.walletsBySubscriber[subRef] = st
		h.mu.Unlock()
	}
	return resp.StatusCode, map[string]any{
		"scenario":                  "api_service_rc_v1_receipts_coin_pipe",
		"gateway_status":            resp.StatusCode,
		"opaque_product_id":         productID,
		"expected_operational_mode": strings.TrimSpace(expectedOpMode),
		"server_operational_mode":   resp.Header.Get("X-Billing-Operational-Mode"),
		"server_required_contract":  resp.Header.Get("X-Billing-Required-SDK-Contract"),
		"receipt_response":          receiptOut,
		"subscriber_v1":             subOut,
		"mock_wallet":               h.GetSubscriberState(subRef),
	}, nil
}

// ExecuteS2SModeAttestation tests the Cross-System S2S Mode-Attestation Contract (POST /api/v1/billing/s2s/mode-attestation)
// both when expected_operational_mode is sent (enforced match vs 409 mismatch) and when omitted (passive discovery).
func (h *HarnessService) ExecuteS2SModeAttestation(expectedOpMode, targetGatewayOverride string) (int, map[string]any, error) {
	gw := h.gatewayURL
	if strings.TrimSpace(targetGatewayOverride) != "" {
		gw = strings.TrimRight(strings.TrimSpace(targetGatewayOverride), "/")
	}
	reqMap := map[string]any{
		"caller_service": "shortical-mock-api-service",
	}
	if strings.TrimSpace(expectedOpMode) != "" {
		reqMap["expected_operational_mode"] = strings.TrimSpace(expectedOpMode)
	}
	payload, _ := json.Marshal(reqMap)
	path := "/api/v1/billing/s2s/mode-attestation"
	tsStr := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	sig := ComputeS2SSignature(h.s2sSecret, tsStr, http.MethodPost, path, payload)
	req, err := http.NewRequest(http.MethodPost, gw+path, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Billing-Timestamp", tsStr)
	req.Header.Set("X-Billing-Signature", sig)
	if strings.TrimSpace(expectedOpMode) != "" {
		req.Header.Set("X-Billing-Expected-Operational-Mode", strings.TrimSpace(expectedOpMode))
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out == nil {
		out = map[string]any{}
	}
	out["response_headers"] = map[string]string{
		"X-Billing-Operational-Mode":      resp.Header.Get("X-Billing-Operational-Mode"),
		"X-Billing-SDK-Distribution-Mode": resp.Header.Get("X-Billing-SDK-Distribution-Mode"),
		"X-Billing-Required-SDK-Contract": resp.Header.Get("X-Billing-Required-SDK-Contract"),
	}
	return resp.StatusCode, out, nil
}
