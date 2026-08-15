package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/laststate/trace/internal/tracectx"
)

// rateLimiter is a simple per-IP token bucket (fixed window) with bounded memory.
type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	hits    map[string]int
	resetAt map[string]time.Time
	maxKeys int
}

func newRateLimiter(perMin, maxKeys int) *rateLimiter {
	if perMin <= 0 {
		perMin = 600
	}
	if maxKeys <= 0 {
		maxKeys = 50_000
	}
	return &rateLimiter{
		limit:   perMin,
		window:  time.Minute,
		hits:    map[string]int{},
		resetAt: map[string]time.Time{},
		maxKeys: maxKeys,
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	// Periodically prune expired entries when map grows large.
	if len(rl.hits) > rl.maxKeys {
		for k, t := range rl.resetAt {
			if now.After(t) {
				delete(rl.resetAt, k)
				delete(rl.hits, k)
			}
		}
		// Still over: drop oldest half
		if len(rl.hits) > rl.maxKeys {
			i := 0
			for k := range rl.hits {
				delete(rl.hits, k)
				delete(rl.resetAt, k)
				i++
				if i > rl.maxKeys/2 {
					break
				}
			}
		}
	}
	if t, ok := rl.resetAt[ip]; !ok || now.After(t) {
		rl.resetAt[ip] = now.Add(rl.window)
		rl.hits[ip] = 1
		return true
	}
	rl.hits[ip]++
	return rl.hits[ip] <= rl.limit
}

type connLimiter struct {
	mu    sync.Mutex
	max   int
	conns map[string]int
}

func newConnLimiter(max int) *connLimiter {
	if max <= 0 {
		max = 64
	}
	return &connLimiter{max: max, conns: map[string]int{}}
}

func (c *connLimiter) acquire(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conns[ip] >= c.max {
		return false
	}
	c.conns[ip]++
	return true
}

func (c *connLimiter) release(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conns[ip] > 0 {
		c.conns[ip]--
	}
	if c.conns[ip] == 0 {
		delete(c.conns, ip)
	}
}

// TrustedProxyStore holds parsed CIDRs/IPs from TRACE_TRUSTED_PROXIES.
// This is server-scoped so it can be updated on config reload.
type TrustedProxyStore struct {
	mu        sync.RWMutex
	proxies   []*net.IPNet
	allowXFF  bool // whether to honor X-Forwarded-For at all
}

var globalProxyStore = &TrustedProxyStore{}

// ConfigureTrustedProxies parses comma-separated IPs/CIDRs and updates the
// server's trusted proxy list. Called on startup and on config reload.
func ConfigureTrustedProxies(list string) {
	store := &TrustedProxyStore{allowXFF: list != ""}
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			if ip := net.ParseIP(p); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				p = ip.String() + "/" + itoa(bits)
			}
		}
		_, n, err := net.ParseCIDR(p)
		if err == nil {
			store.proxies = append(store.proxies, n)
		}
	}
	globalProxyStore.mu.Lock()
	globalProxyStore.proxies = store.proxies
	globalProxyStore.allowXFF = store.allowXFF
	globalProxyStore.mu.Unlock()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func isTrustedProxy(ip string) bool {
	globalProxyStore.mu.RLock()
	defer globalProxyStore.mu.RUnlock()
	if !globalProxyStore.allowXFF {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range globalProxyStore.proxies {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// clientIPFrom extracts the client IP from the request, honoring X-Forwarded-For
// only when the immediate peer is a trusted proxy. Supports configurable parsing
// strategy (leftmost or rightmost original client).
func clientIPFrom(r *http.Request, xffStrategy string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// Only honor X-Forwarded-For when the immediate peer is a trusted proxy.
	if isTrustedProxy(host) {
		if x := r.Header.Get("X-Forwarded-For"); x != "" {
			// Parse X-Forwarded-For based on strategy:
			// - "leftmost" (default): leftmost IP is original client (most proxies append right)
			// - "rightmost": rightmost IP is original client (some proxies prepend left)
			if xffStrategy == "rightmost" {
				parts := strings.Split(x, ",")
				return strings.TrimSpace(parts[len(parts)-1])
			}
			return strings.TrimSpace(strings.Split(x, ",")[0])
		}
		if x := r.Header.Get("X-Real-IP"); x != "" {
			return strings.TrimSpace(x)
		}
	}
	return host
}

func withSecurity(next http.Handler, ratePerMin, maxConns int) http.Handler {
	rl := newRateLimiter(ratePerMin, 50_000)
	cl := newConnLimiter(maxConns)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIPFrom(r, "leftmost")
		if !cl.acquire(ip) {
			http.Error(w, `{"error":{"code":"too_many_connections","message":"connection limit"}}`, http.StatusServiceUnavailable)
			return
		}
		defer cl.release(ip)
		if !rl.allow(ip) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":{"code":"rate_limited","message":"too many requests"}}`, http.StatusTooManyRequests)
			return
		}
		tid := r.Header.Get("X-Trace-ID")
		sid := r.Header.Get("X-Span-ID")
		if tid == "" {
			tid, sid = tracectx.New()
		} else if sid == "" {
			_, sid = tracectx.New()
		}
		ctx := tracectx.With(r.Context(), tid, sid)
		w.Header().Set("X-Trace-ID", tid)
		w.Header().Set("X-Span-ID", sid)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
