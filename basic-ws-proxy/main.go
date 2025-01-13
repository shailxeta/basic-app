package main

import (
	"log"
	"net/http"
	"net/url"
	"os"
)

var (
	// Target load balancer URL
	targetURL = func() string {
		if url := os.Getenv("TARGET_URL"); url != "" {
			return url
		}
		return "ws://localhost:8080/ws"
	}()
)

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(targetURL)
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

	// Serve the request
	//proxy.ServeHTTP(w, r)
	//log.Println("Request served")
}

func main() {
	// Configure the HTTP server
	http.HandleFunc("/ws", proxyHandler)

	// Start the server
	serverAddr := ":8081"
	log.Printf("Starting WebSocket proxy server on %s", serverAddr)
	if err := http.ListenAndServe(serverAddr, nil); err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}
