package main

import (
	"log"
	"net/http"
	"net/url"
	"os"
)

type WSProxy struct {
	targetURL  string
	serverAddr string
}

func NewWSProxy(targetURL, serverAddr string) *WSProxy {
	return &WSProxy{
		targetURL:  targetURL,
		serverAddr: serverAddr,
	}
}

func getTargetURL() string {
	if url := os.Getenv("TARGET_URL"); url != "" {
		return url
	}
	return "ws://localhost:8080/ws"
}

func (p *WSProxy) proxyHandler(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(p.targetURL)
	if err != nil {
		log.Printf("Failed to parse target URL: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Update the request URL
	r.URL.Host = target.Host
	r.URL.Scheme = target.Scheme
	r.URL.Path = target.Path

	// Update headers
	r.Header.Set("Host", target.Host)
	r.Host = target.Host

	redirectUrl := url.URL{
		Scheme: r.URL.Scheme,
		Host:   r.URL.Host,
		Path:   r.URL.Path,
	}

	http.Redirect(w, r, redirectUrl.String(), http.StatusTemporaryRedirect)
}

func (p *WSProxy) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (p *WSProxy) Start() error {
	http.HandleFunc("/ws", p.proxyHandler)
	http.HandleFunc("/health", p.healthHandler)

	log.Printf("Starting WebSocket proxy server on %s", p.serverAddr)
	return http.ListenAndServe(p.serverAddr, nil)
}

func main() {
	proxy := NewWSProxy(getTargetURL(), ":8080")
	if err := proxy.Start(); err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}
