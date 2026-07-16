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

func newRateLimiter(perMin int) *rateLimiter {
	if perMin <= 0 {
		perMin = 600
	}
	return &rateLimiter{
		limit:   perMin,
		window:  time.Minute,
		hits:    map[string]int{},
		resetAt: map[string]time.Time{},
		maxKeys: 50_000,
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	// periodic prune
	if len(rl.hits) > rl.maxKeys {
		for k, t := range rl.resetAt {
			if now.After(t) {
				delete(rl.resetAt, k)
				delete(rl.hits, k)
			}
		}
		// still over: drop oldest half
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

// trustedProxies holds parsed CIDRs/IPs from TRACE_TRUSTED_PROXIES.
var trustedProxyNets []*net.IPNet

// ConfigureTrustedProxies parses comma-separated IPs/CIDRs. Empty = never trust XFF.
func ConfigureTrustedProxies(list string) {
	trustedProxyNets = nil
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
			trustedProxyNets = append(trustedProxyNets, n)
		}
	}
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
	if len(trustedProxyNets) == 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range trustedProxyNets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func clientIPFrom(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// Only honor X-Forwarded-For when the immediate peer is a trusted proxy.
	if isTrustedProxy(host) {
		if x := r.Header.Get("X-Forwarded-For"); x != "" {
			// left-most is original client when proxies append
			return strings.TrimSpace(strings.Split(x, ",")[0])
		}
		if x := r.Header.Get("X-Real-IP"); x != "" {
			return strings.TrimSpace(x)
		}
	}
	return host
}

func withSecurity(next http.Handler, ratePerMin, maxConns int) http.Handler {
	rl := newRateLimiter(ratePerMin)
	cl := newConnLimiter(maxConns)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIPFrom(r)
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
