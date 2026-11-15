.PHONY: test test-server test-web test-mobile dev-server dev-web dev-mobile ios-sim android-emu usb-device ota-manifest

test: test-server test-web test-mobile
	@echo "==> [repos/mock-apps] All 3 Clean-Room Mock Apps (server :4470, web :5390, mobile :5395) passed!"

test-server:
	cd server && env -u GOROOT go test -v ./tests/...

test-web:
	npm --prefix web test

test-mobile:
	npm --prefix mobile test

dev-server:
	cd server && env -u GOROOT go run ./src/cmd/harness

dev-web:
	npm --prefix web start

dev-mobile:
	npm --prefix mobile start

ios-sim:
	bash mobile/scripts/run-on-simulator-or-device.sh ios-sim

android-emu:
	bash mobile/scripts/run-on-simulator-or-device.sh android-emu

usb-device:
	bash mobile/scripts/run-on-simulator-or-device.sh usb-device

ota-manifest:
	bash mobile/scripts/run-on-simulator-or-device.sh ota-manifest
