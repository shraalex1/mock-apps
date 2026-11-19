#!/usr/bin/env bash
# =============================================================================
# mock-apps/mobile/scripts/run-on-simulator-or-device.sh
#
# Launches and verifies the KMP Mobile SDK & Interactive Mobile App UI (:5395)
# against either your LOCAL environment (http://localhost:4460) or the deployed
# AWS DEV environment (TARGET_ENV=dev or BILLING_GATEWAY_URL=<url>):
#
#   1. ios-sim      -> Boots Xcode iOS Simulator (Simulator.app) + StoreKit 2 .storekit
#   2. android-emu  -> Maps ports via adb reverse and opens Mobile App UI on Android Emulator
#   3. usb-device   -> Maps ports via USB cable (adb -d reverse / devicectl) on physical phone
#   4. ota-manifest -> Generates private Over-The-Air (OTA) install page + iOS manifest.plist
#
# Usage Examples:
#   make ios-sim                              (defaults to TARGET_ENV=local -> http://localhost:4460)
#   make ios-sim TARGET_ENV=dev               (points simulator at Shortical AWS Dev CloudFront/ALB)
#   make android-emu TARGET_ENV=dev           (points Android Emulator at Shortical AWS Dev)
#   make usb-device TARGET_ENV=dev            (points physical USB phone at Shortical AWS Dev)
# =============================================================================
set -euo pipefail

MODE="${1:-ota-manifest}"
TARGET_ENV="${2:-${TARGET_ENV:-local}}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MOBILE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${MOBILE_DIR}/dist-ota"
MOBILE_PORT="${MOCK_MOBILE_PORT:-5395}"

if [[ -z "${BILLING_GATEWAY_URL:-}" ]]; then
  case "${TARGET_ENV}" in
    dev|aws-dev|shortical-dev)
      BILLING_GATEWAY_URL="https://d1or4hheo2gkkq.cloudfront.net"
      MOCK_API_SERVICE_URL="${MOCK_API_SERVICE_URL:-http://rev-tiger-dev-alb-363906758.us-east-1.elb.amazonaws.com}"
      ;;
    dev-ex|entlora-dev)
      BILLING_GATEWAY_URL="https://dev-api.entlora.com"
      MOCK_API_SERVICE_URL="${MOCK_API_SERVICE_URL:-https://dev-mockserver.entlora.com}"
      ;;
    *)
      BILLING_GATEWAY_URL="http://localhost:4460"
      MOCK_API_SERVICE_URL="${MOCK_API_SERVICE_URL:-http://localhost:4470}"
      ;;
  esac
fi

export BILLING_GATEWAY_URL
export MOCK_API_SERVICE_URL="${MOCK_API_SERVICE_URL:-http://localhost:4470}"
MOBILE_UI_URL="http://localhost:${MOBILE_PORT}"

ensure_mobile_ui_server() {
  pkill -f "mobileHarness.mjs" 2>/dev/null || true
  sleep 0.3
  echo "==> [Mobile App Server :${MOBILE_PORT}] Target Env: ${TARGET_ENV} -> Gateway: ${BILLING_GATEWAY_URL}"
  nohup env BILLING_GATEWAY_URL="${BILLING_GATEWAY_URL}" MOCK_API_SERVICE_URL="${MOCK_API_SERVICE_URL}" node "${MOBILE_DIR}/src/mobileHarness.mjs" >/tmp/shortical-mock-mobile.log 2>&1 &
  sleep 1
  echo "==> [Mobile App Server :${MOBILE_PORT}] Live at ${MOBILE_UI_URL}"
}

case "${MODE}" in
  ios-sim)
    ensure_mobile_ui_server
    echo "==> [iOS Simulator] Opening Xcode Simulator.app & checking booted device..."
    open -a Simulator || true
    if command -v xcrun >/dev/null 2>&1; then
      BOOTED_COUNT="$(xcrun simctl list devices booted 2>/dev/null | grep -c "(Booted)" || true)"
      if [ "${BOOTED_COUNT}" -eq 0 ]; then
        FIRST_IPHONE_UDID="$(xcrun simctl list devices available 2>/dev/null | grep -E "iPhone " | head -n 1 | sed -E 's/.*\(([0-9A-F-]{36})\).*/\1/' || true)"
        if [ -n "${FIRST_IPHONE_UDID}" ]; then
          echo "==> [iOS Simulator] Booting iPhone Simulator (${FIRST_IPHONE_UDID})..."
          xcrun simctl boot "${FIRST_IPHONE_UDID}" 2>/dev/null || true
          sleep 3
        fi
      fi
      echo "==> [iOS Simulator] StoreKit 2 configuration ready at: ${MOBILE_DIR}/ShorticalProducts.storekit"
      echo "==> [iOS Simulator] Launching Mobile App UI (${MOBILE_UI_URL}/?platform=ios&env=${TARGET_ENV}) inside iOS Simulator..."
      xcrun simctl openurl booted "${MOBILE_UI_URL}/?platform=ios&env=${TARGET_ENV}" 2>/dev/null || true
    fi
    npm --prefix "${MOBILE_DIR}" test
    ;;

  android-emu)
    ensure_mobile_ui_server
    echo "==> [Android Emulator] Checking connected Android Studio Emulator via adb..."
    if command -v adb >/dev/null 2>&1; then
      adb devices || true
      echo "==> [Android Emulator] Forwarding localhost ports 4460, 4470, and ${MOBILE_PORT} via adb reverse..."
      adb reverse tcp:4460 tcp:4460 2>/dev/null || true
      adb reverse tcp:4470 tcp:4470 2>/dev/null || true
      adb reverse "tcp:${MOBILE_PORT}" "tcp:${MOBILE_PORT}" 2>/dev/null || true
      echo "==> [Android Emulator] Launching Mobile App UI (${MOBILE_UI_URL}/?platform=android&env=${TARGET_ENV}) on Emulator screen..."
      adb shell am start -a android.intent.action.VIEW -d "${MOBILE_UI_URL}/?platform=android&env=${TARGET_ENV}" 2>/dev/null || true
    fi
    npm --prefix "${MOBILE_DIR}" test
    ;;

  usb-device)
    ensure_mobile_ui_server
    echo "==> [USB Physical Device] Checking connected iPhone (devicectl) and Android (adb -d)..."
    if command -v xcrun >/dev/null 2>&1; then
      xcrun devicectl list devices 2>/dev/null || true
    fi
    if command -v adb >/dev/null 2>&1; then
      adb -d devices 2>/dev/null || true
      adb -d reverse tcp:4460 tcp:4460 2>/dev/null || true
      adb -d reverse tcp:4470 tcp:4470 2>/dev/null || true
      adb -d reverse "tcp:${MOBILE_PORT}" "tcp:${MOBILE_PORT}" 2>/dev/null || true
      adb -d shell am start -a android.intent.action.VIEW -d "${MOBILE_UI_URL}/?platform=android&env=${TARGET_ENV}" 2>/dev/null || true
    fi
    npm --prefix "${MOBILE_DIR}" test
    ;;

  ota-manifest)
    mkdir -p "${DIST_DIR}"
    cat > "${DIST_DIR}/manifest.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>items</key>
  <array>
    <dict>
      <key>assets</key>
      <array>
        <dict>
          <key>kind</key>
          <string>software-package</string>
          <key>url</key>
          <string>https://billing-admin-dev.shortical.com/ota/shortical-kmp-mock-0.5.0.ipa</string>
        </dict>
      </array>
      <key>metadata</key>
      <dict>
        <key>bundle-identifier</key>
        <string>com.shortical.app</string>
        <key>bundle-version</key>
        <string>0.5.0</string>
        <key>kind</key>
        <string>software</string>
        <key>title</key>
        <string>Shortical KMP Billing Mock (Internal Closed Group)</string>
      </dict>
    </dict>
  </array>
</dict>
</plist>
EOF
    echo "==> [OTA Internal Distribution] Generated ${DIST_DIR}/manifest.plist (itms-services://) & Android APK sideload metadata."
    ;;

  *)
    echo "Usage: $0 {ios-sim|android-emu|usb-device|ota-manifest} [local|dev|dev-ex]" >&2
    exit 1
    ;;
esac
