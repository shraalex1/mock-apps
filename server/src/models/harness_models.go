package models

// MockSubscriberWalletState represents Shortical api-service's view of a subscriber's VIP access and coin wallet.
type MockSubscriberWalletState struct {
	SubscriberRef         string   `json:"subscriber_ref"`
	Brand                 string   `json:"brand"`
	HasActiveVIP          bool     `json:"has_active_vip"`
	ActivePlanID          string   `json:"active_plan_id"`
	PendingPlanID         string   `json:"pending_plan_id,omitempty"`
	CoinsBalance          int64    `json:"coins_balance"`
	WalletClawbackApplied bool     `json:"wallet_clawback_applied"`
	EpisodeUnlocked       bool     `json:"episode_unlocked"`
	ReceivedRCEvents      []string `json:"received_rc_events"`
	LastWebhookID         string   `json:"last_webhook_id"`
	UpdatedAtUTC          string   `json:"updated_at_utc"`
}

// WebhookFaultInjectionConfig controls how mock-api-service responds to POST /webhooks/revenuecat.
type WebhookFaultInjectionConfig struct {
	Mode                  string `json:"mode"` // always_200_ok | fail_3_times_then_200 | always_500_error
	FailedAttemptsCounter int    `json:"failed_attempts_counter"`
	TotalWebhooksReceived int    `json:"total_webhooks_received"`
}

// IncomingRCWebhookEnvelope matches the 1:1 RevenueCat v1 JSON webhook emitted by alt-payments Phase 4.
type IncomingRCWebhookEnvelope struct {
	APIVersion string `json:"api_version"`
	Event      struct {
		ID                       string   `json:"id"`
		Type                     string   `json:"type"`
		AppID                    string   `json:"app_id"`
		AppUserID                string   `json:"app_user_id"`
		ProductID                string   `json:"product_id"`
		NewProductID             string   `json:"new_product_id,omitempty"`
		EntitlementIDs           []string `json:"entitlement_ids"`
		Store                    string   `json:"store"`
		Price                    float64  `json:"price"`
		PriceInPurchasedCurrency float64  `json:"price_in_purchased_currency"`
		TaxPercentage            float64  `json:"tax_percentage,omitempty"`
		InvoiceID                string   `json:"invoice_id,omitempty"`
		CountryCode              string   `json:"country_code,omitempty"`
		Currency                 string   `json:"currency"`
		TransactionID            string   `json:"transaction_id"`
		CancelReason             string   `json:"cancel_reason,omitempty"`
	} `json:"event"`
}

// S2SSimulatorRequest triggers an S2S call from mock-api-service to billing-web (:4460).
type S2SSimulatorRequest struct {
	Scenario         string `json:"scenario"` // full_upstream_source_of_truth | broken_deploy_missing_contract | inventory_sync
	SubscriberRef    string `json:"subscriber_ref"`
	Brand            string `json:"brand"`
	Origin           string `json:"origin,omitempty"`
	Provider         string `json:"provider"`
	OfferingTag      string `json:"offering_tag"`
	PlanID           string `json:"plan_id,omitempty"`
	AmountCents      int64  `json:"amount_cents,omitempty"`
	UpstreamTaxCents *int64 `json:"upstream_tax_cents,omitempty"`
	Currency         string `json:"currency,omitempty"`
	IsAmexRouting    bool   `json:"is_amex_routing,omitempty"`
}

// S2SSimulatorResponse wraps the HTTP status and response payload returned by billing-web.
type S2SSimulatorResponse struct {
	Scenario       string         `json:"scenario"`
	GatewayStatus  int            `json:"gateway_status"`
	GatewayPayload map[string]any `json:"gateway_payload"`
	ExecutedAtUTC  string         `json:"executed_at_utc"`
}

// MobileHarnessSimulateRequest triggers a mobile StoreKit 2 JWS or Play Billing v8 / RTDN flow against billing-web.
type MobileHarnessSimulateRequest struct {
	SubscriberRef string `json:"subscriber_ref"`
	Brand         string `json:"brand"`
	Provider      string `json:"provider"`  // apple | google
	FlowKind      string `json:"flow_kind"` // verify_receipt | assn_v2_downgrade | assn_v2_renew | rtdn_grace_period | mirror_to_test | restore_conflict
	ProductID     string `json:"product_id"`
	NextProductID string `json:"next_product_id,omitempty"`
	OfferToken    string `json:"offer_token,omitempty"`
	AmountCents   int64  `json:"amount_cents,omitempty"`
}
