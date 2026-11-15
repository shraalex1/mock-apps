const GATEWAY_URL = (process.env.BILLING_GATEWAY_URL || 'http://localhost:4460').replace(/\/+$/, '');
const MOCK_SERVER_URL = (process.env.MOCK_API_SERVICE_URL || 'http://localhost:4470').replace(/\/+$/, '');

export function renderMobileHarnessPage(gatewayUrl = GATEWAY_URL, mockServerUrl = MOCK_SERVER_URL) {
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, viewport-fit=cover" />
  <title>Shortical Mobile KMP App Simulator (:5395)</title>
  <style>
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: #08091a;
      color: #f8fafc;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      padding: max(44px, env(safe-area-inset-top, 44px)) 12px 24px;
    }
    .phone-shell {
      max-width: 440px;
      margin: 0 auto;
      background: #11132e;
      border: 1px solid rgba(174, 64, 246, 0.4);
      border-radius: 22px;
      padding: 16px;
      box-shadow: 0 12px 32px rgba(0, 0, 0, 0.55);
    }
    .top-bar {
      display: flex;
      align-items: center;
      justify-content: space-between;
      border-bottom: 1px solid rgba(255,255,255,0.1);
      padding-bottom: 10px;
      margin-bottom: 12px;
    }
    .badge {
      display: inline-block;
      padding: 3px 9px;
      border-radius: 999px;
      font-size: 10px;
      font-weight: 700;
      background: rgba(255, 0, 94, 0.2);
      color: #ff4f8a;
      border: 1px solid rgba(255, 0, 94, 0.4);
    }
    .badge-green {
      background: rgba(0, 230, 118, 0.15);
      color: #00e676;
      border-color: rgba(0, 230, 118, 0.4);
    }
    .section {
      background: #17193b;
      border: 1px solid rgba(255,255,255,0.1);
      border-radius: 14px;
      padding: 12px;
      margin-bottom: 12px;
    }
    h2 { margin: 0 0 6px; font-size: 14px; color: #fff; }
    p { margin: 0 0 8px; font-size: 11px; color: #cbd5e1; line-height: 1.4; }
    .row { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 8px; }
    input, select {
      width: 100%;
      background: #0b0c22;
      color: #fff;
      border: 1px solid rgba(255,255,255,0.18);
      border-radius: 8px;
      padding: 7px 9px;
      font-size: 12px;
      font-family: monospace;
    }
    button {
      background: linear-gradient(90deg, #ff005e, #ae40f6);
      color: #fff;
      border: 0;
      border-radius: 10px;
      padding: 9px 11px;
      font-size: 11px;
      font-weight: 700;
      cursor: pointer;
      flex: 1 1 auto;
    }
    button.secondary {
      background: #222556;
      border: 1px solid rgba(255,255,255,0.18);
    }
    button.green {
      background: rgba(0, 230, 118, 0.2);
      color: #00e676;
      border: 1px solid rgba(0, 230, 118, 0.45);
    }
    pre {
      background: #070816;
      border: 1px solid rgba(255,255,255,0.12);
      padding: 10px;
      border-radius: 10px;
      font-size: 10px;
      overflow-x: auto;
      color: #00e676;
      max-height: 240px;
      margin: 0;
      white-space: pre-wrap;
      word-break: break-all;
    }
  </style>
</head>
<body>
  <div class="phone-shell">
    <div class="top-bar">
      <div>
        <div style="font-weight:800;font-size:15px;">Shortical KMP Mobile App</div>
        <div style="font-size:10px;color:#94a3b8;">kmp-billing-wrapper@0.5.0 • Port :5395</div>
      </div>
      <span id="platform-badge" class="badge badge-green">iOS / Android</span>
    </div>

    <div class="section">
      <h2>Subscriber &amp; Platform Identity</h2>
      <div class="row">
        <input id="sub-ref" value="uid_mobile_emu_01" placeholder="subscriber_ref" />
      </div>
      <div class="row">
        <select id="platform-select">
          <option value="android">Android (Google Play Billing v8 / RTDN)</option>
          <option value="ios">iOS (Apple StoreKit 2 JWS / ASSN v2)</option>
        </select>
      </div>
      <div class="row">
        <button id="btn-init">1. Initialize KMP SDK</button>
        <button id="btn-wallet" class="secondary">Check VIP / Wallet (:4470)</button>
      </div>
    </div>

    <div class="section">
      <h2>2. StoreKit 2 JWS &amp; Play Billing v8 Purchase</h2>
      <p>Signs a sandbox StoreKit 2 JWS receipt or Play v8 token, verifies with Gateway (:4460), and unlocks episode on Mock Server (:4470).</p>
      <div class="row">
        <button id="btn-buy-ios">Buy iOS StoreKit 2 GOLD ($19.99)</button>
        <button id="btn-buy-android">Buy Android Play v8 VIP ($9.99)</button>
      </div>
    </div>

    <div class="section">
      <h2>3. Shadow Parity (mirror_to_test) &amp; Restore Guard</h2>
      <p>Tests 24h fingerprint deduplication on <code>/client/mirror-to-test</code> and <code>409 store_link_owner_conflict</code> on <code>/client/restore</code>.</p>
      <div class="row">
        <button id="btn-mirror" class="green">Send mirror_to_test Snapshot</button>
        <button id="btn-restore-ok" class="secondary">Restore (Owner 200)</button>
        <button id="btn-restore-409" class="secondary">Restore (Hijack 409)</button>
      </div>
    </div>

    <div class="section" style="margin-bottom:0;">
      <h2>Live Telemetry &amp; Gateway Response</h2>
      <pre id="out-log">Ready. Tap any action button above to test against Gateway (${gatewayUrl}) &amp; Mock Server (${mockServerUrl}).</pre>
    </div>
  </div>

  <script>
    const out = document.getElementById('out-log');
    const subInput = document.getElementById('sub-ref');
    const platSelect = document.getElementById('platform-select');
    const platBadge = document.getElementById('platform-badge');

    const params = new URLSearchParams(window.location.search);
    if (params.get('platform') === 'ios' || /iPhone|iPad/i.test(navigator.userAgent)) {
      platSelect.value = 'ios';
      platBadge.textContent = 'Xcode iOS Sim';
    } else {
      platSelect.value = 'android';
      platBadge.textContent = 'Android Emu';
    }

    let lastFp = null;

    function logResult(title, status, data) {
      out.textContent = '[' + new Date().toISOString().slice(11, 19) + '] ' + title + ' (HTTP ' + status + ')\\n' + JSON.stringify(data, null, 2);
    }

    document.getElementById('btn-init').onclick = async () => {
      const sub = subInput.value.trim() || 'uid_mobile_emu_01';
      try {
        const res = await fetch('/proxy/gateway/api/v1/billing/client/initialize', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + sub, 'X-Billing-SDK-Version': '1.4.0' },
          body: JSON.stringify({ subscriber_ref: sub, brand: 'Shortical', contract_version: '1.4.0', client_platform: platSelect.value })
        });
        logResult('Initialize KMP SDK', res.status, await res.json());
      } catch (e) {
        logResult('Initialize Offline Fallback', 0, { error: String(e), fallback: 'legacy_rc_offline_fallback' });
      }
    };

    document.getElementById('btn-wallet').onclick = async () => {
      const sub = subInput.value.trim() || 'uid_mobile_emu_01';
      try {
        const res = await fetch('/api/mobile/wallet/' + encodeURIComponent(sub));
        logResult('Subscriber Wallet & VIP State', res.status, await res.json());
      } catch (e) {
        logResult('Wallet Check Error', 0, { error: String(e) });
      }
    };

    document.getElementById('btn-buy-ios').onclick = async () => {
      const sub = subInput.value.trim() || 'uid_mobile_emu_01';
      try {
        const res = await fetch('/api/mobile/simulate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ subscriber_ref: sub, brand: 'Shortical', provider: 'apple', product_id: 'com.shortical.coins.gold' })
        });
        logResult('iOS StoreKit 2 JWS Purchase Verified', res.status, await res.json());
      } catch (e) {
        logResult('iOS Purchase Error', 0, { error: String(e) });
      }
    };

    document.getElementById('btn-buy-android').onclick = async () => {
      const sub = subInput.value.trim() || 'uid_mobile_emu_01';
      try {
        const res = await fetch('/api/mobile/simulate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ subscriber_ref: sub, brand: 'Shortical', provider: 'google', product_id: 'vip_sub:weekly-base' })
        });
        logResult('Android Play Billing v8 Verified', res.status, await res.json());
      } catch (e) {
        logResult('Android Purchase Error', 0, { error: String(e) });
      }
    };

    document.getElementById('btn-mirror').onclick = async () => {
      const sub = subInput.value.trim() || 'uid_mobile_emu_01';
      const provider = platSelect.value === 'ios' ? 'apple' : 'google';
      const fp = 'vip:com.shortical.vip.weekly:active:2026-11-17T00:00:00Z:' + sub;
      if (lastFp === fp) {
        logResult('mirror_to_test (Client 24h Cache)', 200, { status: 'deduplicated_locally_24h', fingerprint: fp });
        return;
      }
      try {
        const res = await fetch('/proxy/gateway/api/v1/billing/client/mirror-to-test', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + sub },
          body: JSON.stringify({
            subscriber_ref: sub,
            brand: 'Shortical',
            provider,
            mirror_kind: 'init_snapshot',
            snapshot_fingerprint: fp,
            offering_tag: 'vip',
            plan_id: 'com.shortical.vip.weekly',
            rc_entitlement_state: 'active',
            rc_expires_at_utc: '2026-11-17T00:00:00Z'
          })
        });
        lastFp = fp;
        logResult('mirror_to_test Dispatched to Gateway', res.status, await res.json());
      } catch (e) {
        logResult('mirror_to_test Error', 0, { error: String(e) });
      }
    };

    document.getElementById('btn-restore-ok').onclick = async () => {
      const sub = subInput.value.trim() || 'uid_mobile_emu_01';
      try {
        const res = await fetch('/proxy/gateway/api/v1/billing/client/restore', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + sub },
          body: JSON.stringify({ subscriber_ref: sub, brand: 'Shortical', provider: 'apple', offering_tag: 'vip', product_id: 'com.shortical.vip.weekly', store_token_ref: 'orig_tx_shared_991' })
        });
        logResult('Restore Purchases (Owner)', res.status, await res.json());
      } catch (e) {
        logResult('Restore Error', 0, { error: String(e) });
      }
    };

    document.getElementById('btn-restore-409').onclick = async () => {
      try {
        const res = await fetch('/proxy/gateway/api/v1/billing/client/restore', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer uid_other_attacker_99' },
          body: JSON.stringify({ subscriber_ref: 'uid_other_attacker_99', brand: 'Shortical', provider: 'apple', offering_tag: 'vip', product_id: 'com.shortical.vip.weekly', store_token_ref: 'orig_tx_shared_991' })
        });
        logResult('Restore Purchases (Cross-Account 409 Test)', res.status, await res.json());
      } catch (e) {
        logResult('Restore Error', 0, { error: String(e) });
      }
    };
  </script>
</body>
</html>`;
}
