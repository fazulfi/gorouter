package middleware

import (
 "crypto/rand"
 "encoding/base64"
 "fmt"
 "net/http"
 "net"
 "strings"

 "gorouter/internal/domain/auth"
)

type HostConfig struct { Public bool; AllowedHosts []string }
func HostCheck(cfg HostConfig) func(http.Handler) http.Handler { allowed:=map[string]bool{}; for _, h:=range cfg.AllowedHosts { allowed[strings.ToLower(strings.TrimSpace(h))]=true }; return func(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) { if cfg.Public && !allowed[strings.ToLower(r.Host)] { http.Error(w,"invalid host",http.StatusMisdirectedRequest); return }; next.ServeHTTP(w,r) }) } }

func SecurityHeaders() func(http.Handler) http.Handler { return func(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) { b:=make([]byte,18); _,_=rand.Read(b); nonce:=base64.RawURLEncoding.EncodeToString(b); w.Header().Set("Content-Security-Policy",fmt.Sprintf("default-src 'self'; script-src 'self' 'nonce-%s'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'",nonce)); w.Header().Set("X-Frame-Options","DENY"); w.Header().Set("X-Content-Type-Options","nosniff"); w.Header().Set("Referrer-Policy","same-origin"); w.Header().Set("Permissions-Policy","()"); next.ServeHTTP(w,r) }) } }

func StampActor(kind auth.ActorKind) func(http.Handler) http.Handler { return func(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) { actor:=&auth.Actor{Kind:kind,Origin:auth.ActorOriginRemote,UserAgent:r.UserAgent()}; actor.IP=net.ParseIP(realIP(r)); next.ServeHTTP(w,r.WithContext(auth.ContextWithActor(r.Context(),actor))) }) } }
func realIP(r *http.Request) string { if v:=GetRealIP(r.Context()); v!="" { return v }; return strings.Split(r.RemoteAddr,":")[0] }
