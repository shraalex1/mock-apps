import assert from 'node:assert/strict';
import test from 'node:test';
import { ShorticalWebBillingClient } from '../../../alt-payments/sdks/web-billing-sdk/src/index.mjs';
import { renderStorefrontPage } from '../src/server.mjs';

test('shortical-mock-web-client renders storefront and executes E2E web checkout + Quarantine flows via @shortical/web-billing-sdk', async () => {
  const html = renderStorefrontPage('http://localhost:4460', 'http://localhost:4470');
  assert.ok(html.includes('@shortical/web-billing-sdk@0.5.0'));
  assert.ok(html.includes('Trigger Broken Deploy (409 Quarantine)'));

  const sdk = new ShorticalWebBillingClient({
    gatewayBaseUrl: 'http://localhost:4460',
    apiServiceBaseUrl: 'http://localhost:4470',
    brand: 'Shortical',
    fetchImpl: async (url, opts) => {
      const body = opts?.body ? JSON.parse(opts.body) : {};
      if (body.scenario === 'broken_deploy_missing_contract') {
        return {
          ok: false,
          status: 409,
          json: async () => ({
            code: 'contract_mismatch_quarantined',
            quarantine_id: 'quar_web_demo_01',
          }),
        };
      }
      return {
        ok: true,
        status: 200,
        json: async () => ({
          session_id: 'bcs_web_demo_01',
          provider: 'stripe',
          final_amount_cents: 799,
          tax_source: 'shortical_upstream_source_of_truth',
        }),
      };
    },
  });

  const okRes = await sdk.createWebCheckout({
    subscriberRef: 'uid_web_storefront_01',
    planId: 'shortical_vip_weekly_999',
    discountCode: 'PROMO20',
  });
  assert.equal(okRes.status, 200);
  assert.equal(okRes.payload.final_amount_cents, 799);

  const quarRes = await sdk.createWebCheckout({
    subscriberRef: 'uid_web_storefront_02',
    scenario: 'broken_deploy_missing_contract',
  });
  assert.equal(quarRes.status, 409);
  assert.equal(quarRes.payload.code, 'contract_mismatch_quarantined');
});
