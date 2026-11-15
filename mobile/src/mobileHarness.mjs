import crypto from 'node:crypto';
import http from 'node:http';
import { renderMobileHarnessPage } from './mobileHarnessPage.mjs';

export { renderMobileHarnessPage };

/**
 * shortical-mock-mobile-client (Port :5395)
 *
 * Interactive & programmatic simulator of the KMP Wrapper SDK (`com.shortical.billing:kmp-billing-wrapper:0.5.0`).
 */

const PORT = Number(process.env.MOCK_MOBILE_PORT || 5395);
const GATEWAY_URL = (process.env.BILLING_GATEWAY_URL || 'http://localhost:4460').replace(/\/+$/, '');
const MOCK_SERVER_URL = (process.env.MOCK_API_SERVICE_URL || 'http://localhost:4470').replace(/\/+$/, '');
const APPLE_BUNDLE_ID = process.env.APPLE_BUNDLE_ID || 'com.shortical.app';
const APPLE_SIGNING_SECRET = process.env.APPLE_OFFER_SIGNING_SECRET || 'p8_sand_apple_storekit2_keyid_7721f';

export function signStoreKit2JWS(secret, payload) {
  const header = {
    alg: 'HS256',
    typ: 'JWT',
    x5c: ['MIIB+TCCAZ+gAwIBAgIJAJStoreKit2SandboxCert'],
  };
  const hB64 = Buffer.from(JSON.stringify(header)).toString('base64url');
  const pB64 = Buffer.from(JSON.stringify(payload)).toString('base64url');
  const signingInput = `${hB64}.${pB64}`;
  const sigB64 = crypto.createHmac('sha256', secret).update(signingInput).digest('base64url');
  return `${signingInput}.${sigB64}`;
}

export class KmpBillingClientSimulator {
  constructor({
    gatewayBaseUrl = 'http://localhost:4460',
    mockServerUrl = 'http://localhost:4470',
    brand = 'Shortical',
    contractVersion = '1.4.0',
    fetchImpl = globalThis.fetch.bind(globalThis),
  } = {}) {
    this.gatewayBaseUrl = gatewayBaseUrl.replace(/\/+$/, '');
    this.mockServerUrl = mockServerUrl.replace(/\/+$/, '');
    this.brand = brand;
    this.contractVersion = contractVersion;
    this.fetchImpl = fetchImpl;
    this.session = {
      mode: 'test',
      routed_target: 'legacy_rc_with_shadow_mirror',
      mirror_to_test: true,
      fallback_to_legacy_rc: true,
      server_capabilities: [],
    };
    this.lastFingerprint = null;
    this.lastMirrorTimeMs = 0;
  }

  async initialize(subscriberRef, { platform = 'ios', clientFeatureFlagEnabled = null, appsFlyerId = '' } = {}) {
    try {
      const res = await this.fetchImpl(`${this.gatewayBaseUrl}/api/v1/billing/client/initialize`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${subscriberRef}`,
          'X-Billing-SDK-Version': this.contractVersion,
        },
        body: JSON.stringify({
          subscriber_ref: subscriberRef,
          brand: this.brand,
          contract_version: this.contractVersion,
          client_platform: platform,
          client_feature_flag_enabled: clientFeatureFlagEnabled,
          appsflyer_id: appsFlyerId,
        }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      this.session = await res.json();
      return this.session;
    } catch {
      this.session = {
        mode: 'test',
        routed_target: 'legacy_rc_offline_fallback',
        mirror_to_test: false,
        fallback_to_legacy_rc: true,
        server_capabilities: [],
      };
      return this.session;
    }
  }

  canRouteChargeToInhouse() {
    const caps = this.session.server_capabilities || [];
    return (
      this.session.mode === 'transition' &&
      !this.session.fallback_to_legacy_rc &&
      this.session.routed_target === 'inhouse_gateway' &&
      caps.includes('charge')
    );
  }

  async mirrorToTestIfNeeded({
    subscriberRef,
    provider = 'apple',
    mirrorKind = 'init_snapshot',
    offeringTag = 'vip',
    planId = 'com.shortical.vip.weekly',
    rcEntitlementState = 'active',
    rcExpiresAtUtc = '2026-11-15T00:00:00Z',
    nowMs = Date.now(),
  }) {
    if (!this.session.mirror_to_test || this.canRouteChargeToInhouse()) {
      return { status: 'skipped_not_in_mirror_mode' };
    }
    if (!rcEntitlementState || rcEntitlementState === 'none' || rcEntitlementState === 'free') {
      return { status: 'skipped_free_user_locally' };
    }
    const fp = `${offeringTag}:${planId}:${rcEntitlementState}:${rcExpiresAtUtc}`;
    if (fp === this.lastFingerprint && nowMs - this.lastMirrorTimeMs < 24 * 60 * 60 * 1000) {
      return { status: 'deduplicated_locally_24h' };
    }
    this.lastFingerprint = fp;
    this.lastMirrorTimeMs = nowMs;

    const res = await this.fetchImpl(`${this.gatewayBaseUrl}/api/v1/billing/client/mirror-to-test`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${subscriberRef}`,
      },
      body: JSON.stringify({
        subscriber_ref: subscriberRef,
        brand: this.brand,
        provider,
        mirror_kind: mirrorKind,
        snapshot_fingerprint: fp,
        offering_tag: offeringTag,
        plan_id: planId,
        rc_entitlement_state: rcEntitlementState,
        rc_expires_at_utc: rcExpiresAtUtc,
      }),
    });
    return res.json();
  }

  async restorePurchases({ subscriberRef, provider = 'apple', offeringTag = 'vip', productId, storeTokenRef }) {
    const res = await this.fetchImpl(`${this.gatewayBaseUrl}/api/v1/billing/client/restore`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${subscriberRef}`,
      },
      body: JSON.stringify({
        subscriber_ref: subscriberRef,
        brand: this.brand,
        provider,
        offering_tag: offeringTag,
        product_id: productId,
        store_token_ref: storeTokenRef,
      }),
    });
    const body = await res.json();
    return { status: res.status, body };
  }

  /**
   * One purchase, same rule as the KMP wrapper.
   * Contained RevenueCat path: no gateway charge, mirror the completed purchase.
   * In-house path: verify the receipt with the gateway and do not mirror.
   */
  async completePurchase({
    subscriberRef,
    provider = 'apple',
    productId,
    offeringTag = 'vip',
    platform,
  }) {
    const session = await this.initialize(subscriberRef, {
      platform: platform || (provider === 'google' ? 'android' : 'ios'),
    });
    if (!this.canRouteChargeToInhouse()) {
      const mirror = await this.mirrorToTestIfNeeded({
        subscriberRef,
        provider,
        mirrorKind: 'completed_purchase',
        offeringTag,
        planId: productId,
        rcEntitlementState: 'active',
      });
      return {
        wrapper_route: session.routed_target,
        charge_committed: false,
        mirror,
      };
    }

    const verifyPayload = {
      subscriber_ref: subscriberRef,
      brand: this.brand,
      provider,
      product_id: productId,
    };
    if (provider === 'apple') {
      verifyPayload.signed_jws = signStoreKit2JWS(APPLE_SIGNING_SECRET, {
        transactionId: `2000000_${subscriberRef}_${Date.now() % 1000000}`,
        bundleId: APPLE_BUNDLE_ID,
        productId,
        appAccountToken: subscriberRef,
        environment: 'Sandbox',
        price: 19990,
        currency: 'USD',
      });
    } else {
      verifyPayload.purchase_token = `gp_v8_harness_tok_${subscriberRef}_${Date.now() % 1000000}`;
    }
    const res = await this.fetchImpl(`${this.gatewayBaseUrl}/api/v1/billing/client/mobile/verify-receipt`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${subscriberRef}`,
      },
      body: JSON.stringify(verifyPayload),
    });
    const body = await res.json();
    return {
      status: res.status,
      wrapper_route: 'inhouse_gateway',
      charge_committed: res.ok,
      gateway: body,
    };
  }
}

function readReqBody(req) {
  return new Promise((resolve) => {
    const chunks = [];
    req.on('data', (c) => chunks.push(c));
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
  });
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const localWallets = new Map();

  const srv = http.createServer(async (req, res) => {
    const url = req.url || '/';

    if (url === '/healthz') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ status: 'ok', service: 'shortical-mock-mobile-client', port: PORT }));
      return;
    }

    if (url.startsWith('/proxy/gateway/')) {
      const targetPath = url.slice('/proxy/gateway'.length);
      const rawBody = await readReqBody(req);
      try {
        const gwRes = await fetch(`${GATEWAY_URL}${targetPath}`, {
          method: req.method || 'POST',
          headers: {
            'Content-Type': 'application/json',
            ...(req.headers.authorization ? { Authorization: req.headers.authorization } : {}),
            ...(req.headers['x-billing-sdk-version']
              ? { 'X-Billing-SDK-Version': req.headers['x-billing-sdk-version'] }
              : {}),
          },
          body: req.method === 'GET' ? undefined : rawBody,
        });
        const text = await gwRes.text();
        res.writeHead(gwRes.status, { 'Content-Type': 'application/json; charset=utf-8' });
        res.end(text);
      } catch (err) {
        res.writeHead(502, { 'Content-Type': 'application/json; charset=utf-8' });
        res.end(JSON.stringify({ error: String(err), fallback: 'legacy_rc_offline_fallback' }));
      }
      return;
    }

    if (url === '/api/mobile/simulate' && req.method === 'POST') {
      const rawBody = await readReqBody(req);
      const parsed = rawBody ? JSON.parse(rawBody) : {};
      const subRef = (parsed.subscriber_ref || 'uid_mobile_emu_01').trim();
      const brand = (parsed.brand || 'Shortical').trim();
      const provider = (parsed.provider || 'apple').toLowerCase().trim();
      const prodId = (parsed.product_id || (provider === 'apple' ? 'com.shortical.coins.gold' : 'vip_sub:weekly-base')).trim();

      try {
        const client = new KmpBillingClientSimulator({
          gatewayBaseUrl: GATEWAY_URL,
          mockServerUrl: MOCK_SERVER_URL,
          brand,
        });
        const outcome = await client.completePurchase({
          subscriberRef: subRef,
          provider,
          productId: prodId,
          offeringTag: prodId.includes('coin') ? 'coins' : 'vip',
        });
        if (outcome.charge_committed || outcome.mirror?.status === 'recorded_match' || outcome.mirror?.status === 'deduplicated_locally_24h' || outcome.wrapper_route) {
          localWallets.set(subRef, {
            subscriber_ref: subRef,
            brand,
            has_active_vip: true,
            episode_unlocked: true,
            active_plan_id: prodId,
            received_rc_events: [`INITIAL_PURCHASE:${prodId}`],
            updated_at_utc: new Date().toISOString(),
          });
        }
        const status = outcome.status || 200;
        res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' });
        res.end(JSON.stringify(outcome.gateway || outcome));
      } catch (err) {
        res.writeHead(502, { 'Content-Type': 'application/json; charset=utf-8' });
        res.end(JSON.stringify({ error: String(err) }));
      }
      return;
    }

    if (url.startsWith('/api/mobile/wallet/')) {
      const subRef = decodeURIComponent(url.slice('/api/mobile/wallet/'.length).split('?')[0]);
      try {
        const mockRes = await fetch(`${MOCK_SERVER_URL}/api/v1/mock/subscribers/${encodeURIComponent(subRef)}`);
        if (mockRes.ok) {
          const mockJson = await mockRes.json();
          if (mockJson.has_active_vip || !localWallets.has(subRef)) {
            res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
            res.end(JSON.stringify(mockJson));
            return;
          }
        }
      } catch {
        // Fallback to local wallet + gateway entitlements if :4470 is not running
      }

      try {
        const entRes = await fetch(`${GATEWAY_URL}/api/v1/billing/client/entitlements/${encodeURIComponent(subRef)}`, {
          headers: { Authorization: `Bearer ${subRef}` },
        });
        if (entRes.ok) {
          const entJson = await entRes.json();
          res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
          res.end(
            JSON.stringify({
              subscriber_ref: subRef,
              brand: entJson.brand || 'Shortical',
              has_active_vip: entJson.has_active_vip,
              episode_unlocked: entJson.has_active_vip,
              entitlements: entJson.entitlements,
              updated_at_utc: entJson.checked_at_utc,
            }),
          );
          return;
        }
      } catch {
        // ignore
      }

      const fallbackWallet = localWallets.get(subRef) || {
        subscriber_ref: subRef,
        brand: 'Shortical',
        has_active_vip: false,
        episode_unlocked: false,
        received_rc_events: [],
        updated_at_utc: new Date().toISOString(),
      };
      res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
      res.end(JSON.stringify(fallbackWallet));
      return;
    }

    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    res.end(renderMobileHarnessPage());
  });

  srv.listen(PORT, () => {
    console.log(`==> [shortical-mock-mobile-client] Interactive Mobile UI listening on http://localhost:${PORT}`);
  });
}
