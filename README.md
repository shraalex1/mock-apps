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
cd repos/mock-apps
make test          # Runs Go + Web + Mobile automated E2E suites
make ios-sim       # Verifies on local Xcode iOS Simulator (xcrun simctl + ShorticalProducts.storekit)
make android-emu   # Verifies on local Android Studio Emulator (adb)
make usb-device    # Verifies on USB-connected iPhone (devicectl) or Android phone (adb -d)
make ota-manifest  # Generates private Over-The-Air (itms-services://) manifest.plist & APK link for closed testers
```
