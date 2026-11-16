package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxMockJSONBodyBytes    = 64 * 1024
	maxMockWebhookBodyBytes = 1024 * 1024
	mockReplayWindowSec     = int64(300)
	maxMockRateLimitKeys    = 4096
)

type mockRateLimiter struct {
	mu       sync.Mutex
	windowMS int64
	maxReq   int
	counters map[string]*mockWindowEntry
}

type mockWindowEntry struct {
	count     int
	expiresAt int64
}

func newMockRateLimiter(windowMS int64, maxReq int) *mockRateLimiter {
	return &mockRateLimiter{
		windowMS: windowMS,
		maxReq:   maxReq,
		counters: make(map[string]*mockWindowEntry),
	}
}

func (rl *mockRateLimiter) allow(key string, now time.Time) (bool, int) {
	if key == "" {
		key = "anonymous"
	}
	nowMS := now.UnixMilli()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if len(rl.counters) >= maxMockRateLimitKeys {
		for k, v := range rl.counters {
			if nowMS >= v.expiresAt {
				delete(rl.counters, k)
			}
		}
	}
	entry, ok := rl.counters[key]
	if !ok || nowMS >= entry.expiresAt {
		if len(rl.counters) >= maxMockRateLimitKeys && !ok {
			return false, 0
		}
		rl.counters[key] = &mockWindowEntry{count: 1, expiresAt: nowMS + rl.windowMS}
		return true, rl.maxReq - 1
	}
	if entry.count >= rl.maxReq {
		return false, 0
	}
	entry.count++
	return true, rl.maxReq - entry.count
}

// SignClientBearerJWT mints an HS256-signed client session JWT matching alt-payments VerifyClientBearerToken.
func SignClientBearerJWT(secret, subscriberRef string, now time.Time) string {
	headerBytes, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claimsBytes, _ := json.Marshal(map[string]any{
		"iss": "shortical-billing-gateway",
		"sub": strings.TrimSpace(subscriberRef),
		"iat": now.UTC().Unix(),
		"exp": now.UTC().Add(15 * time.Minute).Unix(),
	})
	input := base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(claimsBytes)
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(secret)))
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyConstantTimeToken(provided, expected string) bool {
	p := strings.TrimSpace(provided)
	e := strings.TrimSpace(expected)
	if e == "" || len(p) != len(e) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(p), []byte(e)) == 1
}

// VerifyInboundWebhookAuth enforces constant-time S2S HMAC (with replay window) or constant-time Bearer secret verification.
func (h *HarnessService) VerifyInboundWebhookAuth(now time.Time, method, path, tsHeader, sigHeader, authHeader string, body []byte) error {
	cleanSig := strings.TrimPrefix(strings.TrimSpace(sigHeader), "sha256=")
	cleanTS := strings.TrimSpace(tsHeader)
	if cleanSig != "" {
		if cleanTS != "" {
			tsUnix, err := strconv.ParseInt(cleanTS, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid X-Billing-Timestamp header")
			}
			skew := now.UTC().Unix() - tsUnix
			if skew < 0 {
				skew = -skew
			}
			if skew > mockReplayWindowSec {
				return fmt.Errorf("webhook timestamp outside replay window")
			}
			expected := ComputeS2SSignature(h.s2sSecret, cleanTS, method, path, body)
			if hmac.Equal([]byte(cleanSig), []byte(expected)) {
				return nil
			}
		}
		return fmt.Errorf("invalid X-Billing-Signature HMAC")
	}

	cleanAuth := strings.TrimSpace(authHeader)
	if len(cleanAuth) > 7 && strings.EqualFold(cleanAuth[:7], "bearer ") {
		tok := strings.TrimSpace(cleanAuth[7:])
		if verifyConstantTimeToken(tok, h.s2sSecret) {
			return nil
		}
		if rcBearer := strings.TrimSpace(os.Getenv("RC_WEBHOOK_BEARER_SECRET")); rcBearer != "" && verifyConstantTimeToken(tok, rcBearer) {
			return nil
		}
	}
	return fmt.Errorf("missing or invalid webhook signature or bearer secret")
}

func readBoundedMockBody(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, error) {
	limited := http.MaxBytesReader(w, r.Body, maxBytes)
	defer limited.Close()
	return io.ReadAll(limited)
}

func applyMockSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'")
}

func resolveAllowedMockOrigins(gatewayURL string) []string {
	var allowed []string
	for _, raw := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if trimmed := strings.TrimRight(strings.TrimSpace(raw), "/"); trimmed != "" && !slices.Contains(allowed, trimmed) {
			allowed = append(allowed, trimmed)
		}
	}
	for _, envKey := range []string{"MOCK_WEB_STOREFRONT_URL", "CLIENT_URL"} {
		if v := strings.TrimRight(strings.TrimSpace(os.Getenv(envKey)), "/"); v != "" && !slices.Contains(allowed, v) {
			allowed = append(allowed, v)
		}
	}
	if u, err := url.Parse(strings.TrimSpace(gatewayURL)); err == nil && u.Scheme != "" && u.Host != "" {
		gwOrigin := u.Scheme + "://" + u.Host
		if !slices.Contains(allowed, gwOrigin) {
			allowed = append(allowed, gwOrigin)
		}
	}
	return allowed
}

func (h *HarnessService) isAllowedOrigin(origin string) bool {
	clean := strings.TrimRight(strings.TrimSpace(origin), "/")
	if clean == "" {
		return false
	}
	if slices.Contains(h.allowedOrigins, clean) {
		return true
	}
	return (h.appEnv == "" || h.appEnv == "local") &&
		(strings.HasPrefix(clean, "http://localhost:") || strings.HasPrefix(clean, "http://127.0.0.1:"))
}

func extractMockClientIP(r *http.Request) string {
	peer := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	peerIP := net.ParseIP(peer)
	trusted := peer == "" || (peerIP != nil && (peerIP.IsLoopback() || peerIP.IsPrivate() || peerIP.IsUnspecified()))
	if trusted {
		if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
			hops := strings.Split(fwd, ",")
			for i := len(hops) - 1; i >= 0; i-- {
				if ip := net.ParseIP(strings.TrimSpace(hops[i])); ip != nil {
					return ip.String()
				}
			}
		}
	}
	if peerIP != nil {
		return peerIP.String()
	}
	return peer
}

func newSafeMockHTTPClient(timeout time.Duration, appEnv string) *http.Client {
	blockPrivate := appEnv != "" && appEnv != "local"
	if raw := strings.ToLower(strings.TrimSpace(os.Getenv("OUTBOUND_BLOCK_PRIVATE_IPS"))); raw != "" {
		blockPrivate = raw == "true" || raw == "1" || raw == "yes"
	}
	dialer := &net.Dialer{Timeout: min(timeout, 4*time.Second)}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				for _, resolved := range ips {
					ip := resolved.IP
					if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || strings.EqualFold(ip.String(), "fd00:ec2::254") {
						return nil, fmt.Errorf("blocked link-local or metadata outbound IP %s", ip.String())
					}
					if blockPrivate {
						if ip.IsPrivate() || ip.IsLoopback() {
							return nil, fmt.Errorf("blocked private or loopback outbound IP %s", ip.String())
						}
						if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && (ip4[1]&0xc0) == 64 {
							return nil, fmt.Errorf("blocked CGNAT outbound IP %s", ip.String())
						}
					}
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
			},
		},
	}
}
