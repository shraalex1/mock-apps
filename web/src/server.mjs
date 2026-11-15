import http from 'node:http';

const PORT = Number(process.env.MOCK_STOREFRONT_PORT || 5390);
const GATEWAY_URL = process.env.BILLING_GATEWAY_URL || 'http://localhost:4460';
const MOCK_SERVER_URL = process.env.MOCK_API_SERVICE_URL || 'http://localhost:4470';
const PADDLE_CLIENT_TOKEN = process.env.PADDLE_CLIENT_TOKEN || 'test_36986053c7726600811db7016ac';

const SAMPLE_GOOGLE_RTDN_JSON = JSON.stringify({
  version: '1.0',
  packageName: 'com.shortical.app',
  eventTimeMillis: '1794470000000',
  subscriber_ref: 'uid_android_pubsub_01',
  brand: 'Shortical',
  offering_tag: 'vip',
  amount_cents: 999,
  currency: 'USD',
  subscriptionNotification: {
    version: '1.0',
    notificationType: 2,
    purchaseToken: 'gp_v8_pubsub_live_tok_01',
    subscriptionId: 'shortical_vip:weekly',
  },
});

export function renderStorefrontPage(
  gatewayUrl = GATEWAY_URL,
  mockServerUrl = MOCK_SERVER_URL,
  paddleClientToken = PADDLE_CLIENT_TOKEN,
) {
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Shortical Mock Web Client (:5390) — @shortical/web-billing-sdk Demo</title>
  <script src="https://cdn.paddle.com/paddle/v2/paddle.js"></script>
  <style>
    body { margin: 0; background: #0d0e26; color: #f8fafc; font-family: system-ui, -apple-system, sans-serif; padding: 28px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: 18px; max-width: 1240px; margin: 0 auto; }
    .card { background: #141638; border: 1px solid rgba(255,255,255,0.12); border-radius: 14px; padding: 20px; }
    .badge { display: inline-block; padding: 3px 9px; border-radius: 999px; font-size: 11px; font-weight: 700; background: rgba(174,64,246,0.22); color: #d9aaff; }
    button { background: #7538fa; color: #fff; border: 0; border-radius: 10px; padding: 9px 14px; font-size: 12px; font-weight: 600; cursor: pointer; margin-right: 8px; margin-top: 8px; }
    button:hover { background: #8c52ff; }
    button.green { background: #00a859; }
    button.secondary { background: #232554; border: 1px solid rgba(255,255,255,0.18); }
    input { background: #090a1a; color: #fff; border: 1px solid rgba(255,255,255,0.18); border-radius: 8px; padding: 8px 10px; font-size: 12px; font-family: monospace; width: 100%; box-sizing: border-box; margin-top: 6px; }
    pre { background: #090a1a; padding: 14px; border-radius: 10px; font-size: 11px; overflow-x: auto; color: #00e676; border: 1px solid rgba(255,255,255,0.1); max-width: 1240px; margin: 18px auto 0; white-space: pre-wrap; }
  </style>
</head>
<body>
  <div style="max-width:1240px;margin:0 auto 20px;">
    <span class="badge">shortical-mock-web-client • Port :5390</span>
    <h1 style="margin:8px 0 4px;font-size:22px;">Shortical &amp; OneMore Clean-Room Web Storefront Demo</h1>
    <p style="margin:0;color:#94a3b8;font-size:13px;">
      Powered by <code>@shortical/web-billing-sdk@0.5.0</code> • Gateway: <code>${gatewayUrl}</code> • Mock Server: <code>${mockServerUrl}</code>
    </p>
  </div>
  <div class="grid">
    <div class="card">
      <h3 style="margin-top:0;">1. Locked Episode Checkout (Stripe 4242, Stripe 3DS2 Challenge &amp; Paddle Fallback)</h3>
      <p style="font-size:12px;color:#cbd5e1;">Tests Brand Routing (Shortical / OneMore), Stripe Frictionless (<code>4242</code>), <strong>Stripe 3D-Secure 2 Challenge (<code>4000 0025 0000 3155</code> -&gt; <code>pending</code> -&gt; 3DS Auth -&gt; <code>committed</code>)</strong>, and <strong>Amex -&gt; Paddle Checkout (<code>pending</code> -&gt; Card Entry -&gt; <code>committed</code> + Expired Link Guard)</strong>.</p>
      <button id="btn-shortical-vip">Checkout Shortical VIP ($9.99)</button>
      <button id="btn-stripe-3ds" class="secondary">Stripe 3D-Secure 2 Challenge ($9.99)</button>
      <button id="btn-amex-paddle">Amex -&gt; Paddle Fallback</button>
      <button id="btn-open-paddle-overlay" class="green">🔐 Open Pending Card / 3DS Verification Link ↗</button>
      <button id="btn-onemore-vip">Checkout OneMore VIP ($19.99)</button>
    </div>
    <div class="card">
      <h3 style="margin-top:0;">2. Cohort Rollout Simulator, 409 Quarantine &amp; Email Link</h3>
      <p style="font-size:12px;color:#cbd5e1;">Test all 3 operational modes (<code>test 0%</code> Contained RC + <code>mirror_to_test</code>, <code>transition X%</code> Cohort Split, <code>transition 100%</code> In-House), 409 Quarantine, or Web-to-App email link.</p>
      <button id="btn-cohort-batch" class="green">Run 4-Subscriber Cohort Rollout Batch</button>
      <button id="btn-quarantine">Trigger Broken Deploy (409 Quarantine)</button>
      <button id="btn-attach-web" class="secondary">Link Web Purchase via Email</button>
    </div>
    <div class="card">
      <h3 style="margin-top:0;">3. Contained RevenueCat Sandbox + mirror_to_test &amp; GCP Pub/Sub RTDN</h3>
      <p style="font-size:12px;color:#cbd5e1;">Note: RevenueCat Web Billing blocks dummy domains like <code>@test.com</code> (<code>code: 7834</code>). Use a valid MX domain email below (pre-filled with <code>shraalex0+rctest@gmail.com</code>).</p>
      <input id="rc-web-key" value="rcb_sb_awPqPAzCqwOatDODGiNQpEaqC" placeholder="RC Web Key (rcb_sb_...) or Stripe Key (strp_...)" />
      <input id="rc-web-email" value="shraalex0+rctest@gmail.com" placeholder="Buyer Email with valid MX (e.g. shraalex0+rctest@gmail.com)" />
      <button id="btn-rc-contained" class="green">Run Contained RC Checkout + mirror_to_test</button>
      <button id="btn-copy-pubsub" class="secondary">Copy Google Pub/Sub RTDN JSON</button>
    </div>
  </div>
  <pre id="web-out">Ready. Click any button above to run a live E2E request against Mock Server (${mockServerUrl}) &amp; Gateway (${gatewayUrl}).</pre>
  <script>
    const MOCK_SRV = ${JSON.stringify(mockServerUrl)};
    const GW_SRV = ${JSON.stringify(gatewayUrl)};
    const DEFAULT_PADDLE_TOK = ${JSON.stringify(paddleClientToken)};
    const PUBSUB_RTDN_JSON = ${JSON.stringify(SAMPLE_GOOGLE_RTDN_JSON)};
    const out = document.getElementById('web-out');
    let latestPaddleTxnId = '';
    let latestPaddleCheckoutUrl = '';
    let latestPaddleToken = DEFAULT_PADDLE_TOK;

    const params = new URLSearchParams(window.location.search);
    if (params.get('_ptxn')) {
      latestPaddleTxnId = params.get('_ptxn');
    }

    function initPaddleAndMaybeOpen(txnId, token) {
      if (!window.Paddle) return false;
      try {
        window.Paddle.Environment.set('sandbox');
        window.Paddle.Initialize({ token: token || DEFAULT_PADDLE_TOK });
        if (txnId) {
          window.Paddle.Checkout.open({ transactionId: txnId });
          return true;
        }
      } catch (err) {
        console.warn('Paddle init/open warning:', err);
      }
      return false;
    }

    window.addEventListener('load', () => {
      if (latestPaddleTxnId) {
        initPaddleAndMaybeOpen(latestPaddleTxnId, latestPaddleToken);
      }
    });

    function show(label, status, data) {
      out.textContent = '[' + new Date().toISOString().slice(11, 19) + '] ' + label + ' (HTTP ' + status + ')\\n' + JSON.stringify(data, null, 2);
    }

    async function triggerS2S(label, payload) {
      try {
        const res = await fetch(MOCK_SRV + '/api/v1/mock/s2s/trigger', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const data = await res.json();
        const gw = data.gateway_payload || {};
        if (gw.client_token) latestPaddleToken = gw.client_token;
        const steps = Array.isArray(gw.simulation_steps) ? gw.simulation_steps : [];
        for (const st of steps) {
          const m = String(st).match(/(?:txn_|pi_)[a-z0-9]+/i);
          if (m) latestPaddleTxnId = m[0];
          const urlMatch = String(st).match(/checkout_url=(\S+)/);
          if (urlMatch) latestPaddleCheckoutUrl = urlMatch[1];
        }
        show(label, res.status, data);
      } catch (e) {
        show(label + ' Error', 0, { error: String(e) });
      }
    }

    document.getElementById('btn-shortical-vip').onclick = () =>
      triggerS2S('Shortical VIP Checkout (Stripe Sandbox A)', {
        scenario: 'full_upstream_source_of_truth',
        subscriber_ref: 'uid_web_shortical_01',
        brand: 'Shortical',
        provider: 'stripe',
        offering_tag: 'vip',
        plan_id: 'shortical_vip_weekly_999',
        amount_cents: 999,
        currency: 'USD',
        discount_code: 'PROMO20'
      });

    document.getElementById('btn-stripe-3ds').onclick = () =>
      triggerS2S('Shortical Stripe 3D-Secure 2 Challenge (Pending until 3DS Authenticated)', {
        scenario: 'full_upstream_source_of_truth',
        subscriber_ref: 'uid_web_stripe_3ds_01',
        brand: 'Shortical',
        provider: 'stripe',
        offering_tag: 'vip',
        plan_id: 'shortical_vip_weekly_999_3ds',
        amount_cents: 999,
        currency: 'USD'
      });

    document.getElementById('btn-amex-paddle').onclick = () =>
      triggerS2S('Shortical Amex -> Paddle Fallback (Pending until Card Verified)', {
        scenario: 'full_upstream_source_of_truth',
        subscriber_ref: 'uid_web_amex_01',
        brand: 'Shortical',
        provider: 'stripe',
        offering_tag: 'vip',
        plan_id: 'shortical_vip_weekly_999',
        amount_cents: 999,
        currency: 'USD',
        is_amex_routing: true
      });

    document.getElementById('btn-open-paddle-overlay').onclick = () => {
      if (!latestPaddleTxnId && !latestPaddleCheckoutUrl) {
        show('Card / 3DS Verification Link', 400, { hint: 'Click "Stripe 3D-Secure 2 Challenge" or "Amex -> Paddle Fallback" first to create a pending transaction with a verification link.' });
        return;
      }
      if (latestPaddleCheckoutUrl) {
        window.open(latestPaddleCheckoutUrl, '_blank', 'noopener');
        show('Opened One-Time Card / 3D-Secure Verification Link (Expires immediately upon completion)', 200, {
          provider_transaction_id: latestPaddleTxnId,
          checkout_url: latestPaddleCheckoutUrl
        });
        return;
      }
      const opened = initPaddleAndMaybeOpen(latestPaddleTxnId, latestPaddleToken);
      show('Opened Paddle.js Checkout Overlay', opened ? 200 : 500, { transaction_id: latestPaddleTxnId });
    };

    document.getElementById('btn-onemore-vip').onclick = () =>
      triggerS2S('OneMore VIP Checkout (Stripe Sandbox B)', {
        scenario: 'full_upstream_source_of_truth',
        subscriber_ref: 'uid_web_onemore_01',
        brand: 'OneMore',
        provider: 'stripe',
        offering_tag: 'vip',
        plan_id: 'onemore_vip_monthly_1999',
        amount_cents: 1999,
        currency: 'USD'
      });

    document.getElementById('btn-cohort-batch').onclick = async () => {
      const cohortUsers = ['uid_cohort_alpha_01', 'uid_cohort_beta_02', 'uid_cohort_gamma_03', 'uid_cohort_delta_04'];
      const results = [];
      for (const u of cohortUsers) {
        const r = await fetch(MOCK_SRV + '/api/v1/mock/s2s/trigger', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            scenario: 'full_upstream_source_of_truth',
            subscriber_ref: u,
            brand: 'Shortical',
            provider: 'stripe',
            offering_tag: 'vip',
            plan_id: 'shortical_vip_weekly_999',
            amount_cents: 999,
            currency: 'USD'
          })
        });
        const d = await r.json();
        const gw = d.gateway_payload || {};
        results.push({
          subscriber_ref: u,
          routed_target: gw.wrapper_route || gw.routed_target,
          routing_reason: gw.routing_reason,
          inhouse_charge_committed: gw.charge_committed,
          inhouse_tx_id: gw.simulation_transaction_id || null,
          rc_mirror_to_test: gw.mirror || null
        });
      }
      show('4-Subscriber Cohort Rollout Batch (In-House vs Legacy RC + mirror_to_test)', 200, { cohort_results: results });
    };

    document.getElementById('btn-quarantine').onclick = () =>
      triggerS2S('Broken Deploy -> 409 Zero-Guess Quarantine', {
        scenario: 'broken_deploy_missing_contract',
        subscriber_ref: 'uid_web_quarantine_01',
        brand: 'Shortical',
        provider: 'stripe',
        offering_tag: 'vip'
      });

    document.getElementById('btn-attach-web').onclick = async () => {
      try {
        const res = await fetch(MOCK_SRV + '/api/v1/monetization/subscription/web/link', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ subscriber_ref: 'uid_web_shortical_01', email: 'viewer@shortical.com' })
        });
        show('Web-to-App Email Link', res.status, await res.json());
      } catch (e) {
        show('Email Link Error', 0, { error: String(e) });
      }
    };

    document.getElementById('btn-rc-contained').onclick = async () => {
      const apiKey = document.getElementById('rc-web-key').value.trim();
      const customerEmail = (document.getElementById('rc-web-email')?.value || 'shraalex0+rctest@gmail.com').trim();
      const subRef = 'uid_rc_cleanroom_' + Math.floor(Date.now() / 1000);
      try {
        if (apiKey.startsWith('rcb_')) {
          const mod = await import('https://esm.sh/@revenuecat/purchases-js@1.19.0');
          const purchases = mod.Purchases.configure(apiKey, subRef);
          const offerings = await purchases.getOfferings();
          const vipOff = offerings.all['vip'] || offerings.current;
          const pkg = vipOff && vipOff.availablePackages && vipOff.availablePackages[0];
          if (!pkg) {
            show('Contained RC SDK', 404, { error: 'No available package found in RC offering vip', offerings: Object.keys(offerings.all || {}) });
            return;
          }
          const purchaseRes = await purchases.purchase({ rcPackage: pkg, customerEmail });
          const priceObj = pkg.rcBillingProduct?.currentPrice || pkg.webBillingProduct?.currentPrice || {};
          const amountCents = priceObj.amountMicros ? Math.round(priceObj.amountMicros / 10000) : (priceObj.amount || 999);
          const currency = priceObj.currency || 'USD';
          const mirrorRes = await fetch(MOCK_SRV + '/api/v1/mock/rc/contained-mirror', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              subscriber_ref: subRef,
              brand: 'Shortical',
              provider: 'stripe',
              offering_tag: 'vip',
              plan_id: pkg.rcBillingProduct?.identifier || 'shortical_vip_weekly',
              amount_cents: amountCents,
              currency
            })
          });
          show('Contained RC Checkout + mirror_to_test Completed', 200, { rc_customer_info: purchaseRes.customerInfo, mirror_to_test: await mirrorRes.json() });
          return;
        }
        const syncRes = await fetch(GW_SRV + '/api/v1/billing/sandbox/rc-contained-sync', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ subscriber_ref: subRef, brand: 'Shortical', rc_api_key: apiKey })
        });
        const syncData = await syncRes.json();
        const mirrorRes = await fetch(MOCK_SRV + '/api/v1/mock/rc/contained-mirror', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ subscriber_ref: subRef, brand: 'Shortical', provider: 'stripe', offering_tag: 'vip', plan_id: 'shortical_vip_weekly' })
        });
        show('Contained RC Stripe Receipt Sync + mirror_to_test', syncRes.status, { rc_sync: syncData, mirror_to_test: await mirrorRes.json() });
      } catch (e) {
        show('Contained RC Checkout Error', 500, { error: String(e) });
      }
    };

    document.getElementById('btn-copy-pubsub').onclick = async () => {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(PUBSUB_RTDN_JSON);
      }
      show('Copied Google Cloud Pub/Sub RTDN Message Body (Paste into Topic -> Messages -> Publish message)', 200, JSON.parse(PUBSUB_RTDN_JSON));
    };
  </script>
</body>
</html>`;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const srv = http.createServer((req, res) => {
    if (req.url === '/healthz') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ status: 'ok', service: 'shortical-mock-web-client', port: PORT }));
      return;
    }
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    res.end(renderStorefrontPage());
  });
  srv.listen(PORT, () => {
    console.log(`==> [shortical-mock-web-client] Listening on http://localhost:${PORT}`);
  });
}
