package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type contextKey string

func (c contextKey) String() string { return "gorouter.middleware." + string(c) }

const realIPKey contextKey = "real_ip"

var forwardedHeaders = []string{
	"X-Forwarded-For",
	"X-Real-IP",
	"X-Forwarded-Proto",
	"X-Forwarded-Host",
	"X-Forwarded-Prefix",
}

type TrustedProxyConfig struct {
	TrustedProxies []string
	RealIPFunc     func(r *http.Request) string
}

func GetRealIP(ctx context.Context) string {
	v, _ := ctx.Value(realIPKey).(string)
	return v
}

// NewTrustedProxy creates a trusted proxy middleware that validates CIDRs at
// construction time, returning an error for invalid configurations instead of
// panicking. Use this in production code.
func NewTrustedProxy(cfg TrustedProxyConfig) (func(http.Handler) http.Handler, error) {
	parsedCIDRs := make([]netip.Prefix, 0, len(cfg.TrustedProxies))
	for _, cidr := range cfg.TrustedProxies {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("trustedproxy: invalid CIDR %q: %w", cidr, err)
		}
		parsedCIDRs = append(parsedCIDRs, p)
	}

	realIPFunc := cfg.RealIPFunc
	if realIPFunc == nil {
		realIPFunc = defaultRealIP
	}

	return buildTrustedProxy(parsedCIDRs, realIPFunc), nil
}

// TrustedProxy is a backward-compatible wrapper that panics on invalid config.
// Prefer NewTrustedProxy for production code.
func TrustedProxy(cfg interface{}) func(http.Handler) http.Handler {
	var config *TrustedProxyConfig
	switch v := cfg.(type) {
	case *TrustedProxyConfig:
		config = v
	case TrustedProxyConfig:
		config = &v
	default:
		panic("trustedproxy: config must be *TrustedProxyConfig or TrustedProxyConfig")
	}

	if config == nil {
		panic("trustedproxy: config cannot be nil")
	}

	mw, err := NewTrustedProxy(*config)
	if err != nil {
		panic(err.Error())
	}
	return mw
}

func buildTrustedProxy(parsedCIDRs []netip.Prefix, realIPFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sourceIP := realIPFunc(r)
			trusted := false

			if addr, err := netip.ParseAddr(sourceIP); err == nil {
				for _, cidr := range parsedCIDRs {
					if cidr.Contains(addr) {
						trusted = true
						break
					}
				}
			}

			if !trusted {
				stripForwardedHeaders(r)
			}

			ctx := context.WithValue(r.Context(), realIPKey, sourceIP)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func stripForwardedHeaders(r *http.Request) {
	for _, h := range forwardedHeaders {
		r.Header.Del(h)
	}
}

func defaultRealIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	host = strings.Trim(host, "[]")
	return host
}
