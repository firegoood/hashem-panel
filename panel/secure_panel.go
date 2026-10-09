package main

import (
	"fmt"
	"net"
	"net/http"
)

func securePanelHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if isRequestHTTPS(r) || (ip != nil && ip.IsLoopback()) {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host == "" {
			http.Error(w, "HTTPS required", http.StatusBadRequest)
			return
		}
		u := *r.URL
		u.Scheme = "https"
		u.Host = net.JoinHostPort(host, fmt.Sprint(effectiveTLSPort()))
		http.Redirect(w, r, u.String(), http.StatusSeeOther)
	})
}
