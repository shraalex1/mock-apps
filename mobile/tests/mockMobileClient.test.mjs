import assert from 'node:assert/strict';
import test from 'node:test';
import { KmpBillingClientSimulator } from '../src/mobileHarness.mjs';

test('shortical-mock-mobile-client simulates KMP Wrapper SDK offline fallback, 24h mirror_to_test dedup, and 409 restore conflict guard', async () => {
  // 1. Offline safety net: when gateway throws network error, stays on contained RevenueCat SDK and disables mirror_to_test
  const offlineClient = new KmpBillingClientSimulator({
    fetchImpl: async () => {
      throw new Error('ECONNREFUSED');
    },
  });
  const offlineSession = await offlineClient.initialize('uid_mobile_offline_01');
  assert.equal(offlineSession.routed_target, 'legacy_rc_offline_fallback');
  assert.equal(offlineSession.mirror_to_test, false);
  assert.equal(offlineClient.canRouteChargeToInhouse(), false);

  // 2. Online shadow mode: skips free users, deduplicates 24h fingerprint, and enforces 409 on cross-user restore
  const owners = new Map();
  const onlineClient = new KmpBillingClientSimulator({
    fetchImpl: async (url, opts) => {
      const body = opts?.body ? JSON.parse(opts.body) : {};
      if (url.endsWith('/api/v1/billing/client/initialize')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            mode: 'test',
            routed_target: 'legacy_rc_with_shadow_mirror',
            mirror_to_test: true,
            fallback_to_legacy_rc: false,
            server_capabilities: ['mirror_to_test', 'charge', 'invoice'],
          }),
        };
      }
      if (url.endsWith('/api/v1/billing/client/mirror-to-test')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ status: 'recorded_match', parity_matched: true }),
        };
      }
      if (url.endsWith('/api/v1/billing/client/restore')) {
        const existing = owners.get(body.store_token_ref);
        if (existing && existing !== body.subscriber_ref) {
          return {
            ok: false,
            status: 409,
            json: async () => ({ code: 'store_link_owner_conflict' }),
          };
        }
        owners.set(body.store_token_ref, body.subscriber_ref);
        return {
          ok: true,
          status: 200,
          json: async () => ({ has_active_vip: true, had_subscription_history: true }),
        };
      }
      return { ok: false, status: 404, json: async () => ({}) };
    },
  });

  await onlineClient.initialize('uid_mobile_shadow_01');

  // Free user skipped
  const freeRes = await onlineClient.mirrorToTestIfNeeded({
    subscriberRef: 'uid_mobile_shadow_01',
    rcEntitlementState: 'none',
  });
  assert.equal(freeRes.status, 'skipped_free_user_locally');

  // 1st active snapshot sent -> recorded_match; 2nd within 24h -> deduplicated_locally_24h
  const now = 1762640000000;
  const m1 = await onlineClient.mirrorToTestIfNeeded({
    subscriberRef: 'uid_mobile_shadow_01',
    rcEntitlementState: 'active',
    nowMs: now,
  });
  assert.equal(m1.status, 'recorded_match');

  const m2 = await onlineClient.mirrorToTestIfNeeded({
    subscriberRef: 'uid_mobile_shadow_01',
    rcEntitlementState: 'active',
    nowMs: now + 60_000,
  });
  assert.equal(m2.status, 'deduplicated_locally_24h');

  // Restore purchases: Owner A succeeds (200), Attacker B gets 409 Conflict
  const r1 = await onlineClient.restorePurchases({
    subscriberRef: 'uid_owner_a',
    productId: 'com.shortical.vip.weekly',
    storeTokenRef: 'orig_tx_9001',
  });
  assert.equal(r1.status, 200);
  assert.equal(r1.body.had_subscription_history, true);

  const r2 = await onlineClient.restorePurchases({
    subscriberRef: 'uid_attacker_b',
    productId: 'com.shortical.vip.weekly',
    storeTokenRef: 'orig_tx_9001',
  });
  assert.equal(r2.status, 409);
  assert.equal(r2.body.code, 'store_link_owner_conflict');
});

test('purchase stays on contained RevenueCat until mode is transition, then charges only the gateway', async () => {
  const calls = [];
  function clientFor(session) {
    return new KmpBillingClientSimulator({
      fetchImpl: async (url) => {
        calls.push(url);
        if (url.endsWith('/api/v1/billing/client/initialize')) {
          return { ok: true, status: 200, json: async () => session };
        }
        if (url.endsWith('/api/v1/billing/client/mirror-to-test')) {
          return { ok: true, status: 200, json: async () => ({ status: 'recorded_match' }) };
        }
        if (url.endsWith('/api/v1/billing/client/mobile/verify-receipt')) {
          return { ok: true, status: 200, json: async () => ({ acknowledged: true }) };
        }
        return { ok: false, status: 404, json: async () => ({}) };
      },
    });
  }

  calls.length = 0;
  const contained = await clientFor({
    mode: 'test',
    routed_target: 'legacy_rc_with_shadow_mirror',
    mirror_to_test: true,
    fallback_to_legacy_rc: false,
    server_capabilities: ['mirror_to_test', 'charge'],
  }).completePurchase({
    subscriberRef: 'uid_still_on_rc',
    provider: 'google',
    productId: 'vip_sub:weekly-base',
  });
  assert.equal(contained.charge_committed, false);
  assert.equal(contained.mirror.status, 'recorded_match');
  assert.equal(calls.some((url) => url.endsWith('/verify-receipt')), false);

  calls.length = 0;
  const moved = await clientFor({
    mode: 'transition',
    routed_target: 'inhouse_gateway',
    mirror_to_test: true,
    fallback_to_legacy_rc: false,
    server_capabilities: ['mirror_to_test', 'charge'],
  }).completePurchase({
    subscriberRef: 'uid_moved_inhouse',
    provider: 'google',
    productId: 'vip_sub:weekly-base',
  });
  assert.equal(moved.charge_committed, true);
  assert.equal(moved.wrapper_route, 'inhouse_gateway');
  assert.equal(calls.some((url) => url.endsWith('/verify-receipt')), true);
  assert.equal(calls.some((url) => url.endsWith('/mirror-to-test')), false);
});
