# `repos/mock-apps` — Consolidated Clean-Room Mock Monorepo (`server`, `web`, `mobile`)

Single consolidated repository containing all 3 Clean-Room Mock applications for standalone E2E verification of `repos/alt-payments` without touching `Shortical` production infrastructure:

```text
repos/mock-apps/
├── server/   # Go 1.23 (:4470) — Mock Shortical api-service (1:1 RC webhook receiver, 5-retry fault injection, S2S caller)
├── web/      # Node 24 (:5390) — Mock Web Storefront consuming @shortical/web-billing-sdk
├── mobile/   # Node 24 + KMP Simulator/Device/OTA Runner (:5395) + ShorticalProducts.storekit
├── docker-compose.yml
├── Makefile
└── .github/workflows/deploy-mock-apps.yml
```

## 1. Quick Commands (Local Laptop & Cloud CI)

```bash
cd mock-apps
make test             # Runs Go + Web + Mobile automated E2E suites
make ios-sim          # Verifies on local Xcode iOS Simulator against local Gateway (:4460)
make ios-sim-dev      # Verifies on local Xcode iOS Simulator pointed directly at AWS Dev (https://d1or4hheo2gkkq.cloudfront.net)
make android-emu      # Verifies on local Android Studio Emulator (adb) against local Gateway (:4460)
make android-emu-dev  # Verifies on local Android Studio Emulator pointed directly at AWS Dev
make usb-device       # Verifies on USB-connected iPhone (devicectl) or Android phone (adb -d) against local Gateway
make usb-device-dev   # Verifies on USB-connected phone pointed directly at AWS Dev
make ota-manifest     # Generates private Over-The-Air (itms-services://) manifest.plist & APK link for closed testers
```

## 2. Security & OWASP Top 10 Hardening

- **Fail-Closed Cloud Startup:** Outside `APP_ENV=local`, `mock-apps/server` refuses to boot if `S2S_HMAC_SECRET_CURRENT` is unset or uses a `REPLACE_*` placeholder, and configures HTTP socket header/read/write/idle timeouts from env.
- **Explicit CORS & Hardened Headers:** `mock-apps/server` only reflects origins listed in `CORS_ALLOWED_ORIGINS` (plus configured storefront/gateway origins, and `localhost` only when `APP_ENV=local`), applies HSTS/CSP/`nosniff`/`SAMEORIGIN`, enforces bounded request bodies (`64 KB` JSON / `1 MB` webhooks), and rate-limits by right-most ALB `X-Forwarded-For` IP.
- **Constant-Time Webhook & Client JWT Crypto:** `POST /webhooks/revenuecat` verifies S2S HMAC (`X-Billing-Timestamp` + `X-Billing-Signature` within `300s`) or constant-time `Authorization: Bearer` secrets. Outbound SDK calls from `server` and `mobile` mint short-lived HS256 Bearer JWTs (`SignClientBearerJWT` / `signClientBearerToken`), and `mobile` restricts `/proxy/gateway/*` strictly to `/api/v1/billing/client/*`.
- **Non-Root Containers & CI Audits:** Both `server/Dockerfile` (`USER appuser`) and `web/Dockerfile` (`USER node`) run unprivileged, and `.github/workflows/deploy-mock-apps.yml` enforces `permissions: contents: read`, `go vet`, `go test -race`, and `govulncheck` before pushing immutable SHA tags.

