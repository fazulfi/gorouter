package middleware

import (
	"context"
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

	parsedCIDRs := make([]netip.Prefix, 0, len(config.TrustedProxies))
	for _, cidr := range config.TrustedProxies {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			panic("trustedproxy: invalid CIDR: " + cidr + ": " + err.Error())
		}
		parsedCIDRs = append(parsedCIDRs, p)
	}

	realIPFunc := config.RealIPFunc
	if realIPFunc == nil {
		realIPFunc = defaultRealIP
	}

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
