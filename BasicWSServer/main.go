package main

import (
	"fmt"
	"github.com/gorilla/mux" // Added for routing
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"
)

var activeConnection int32 // Using atomic int32 for thread-safe operations
var loadSheddingThreshold uint64 = 70

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (for testing; restrict in production)
	},
}

func init() {
	// Start a goroutine to log memory utilization and connection count
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for range ticker.C {
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			memoryUtilizationPercent := (memStats.Alloc * 100) / memStats.Sys
			connections := logConnectionCount()
			fmt.Printf("Memory utilization: %d%%, Active connections: %d\n",
				memoryUtilizationPercent, connections)
		}
	}()
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	if !checkMemoryUsage() {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		fmt.Printf("Memory load shedder: Dropping request - %v\n", r.RemoteAddr)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()
	incrementConnections()
	defer decrementConnections()
	for {
		messageType, p, err := ws.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}
		fmt.Printf("Message Received: %s\n", p)

		if err := ws.WriteMessage(messageType, p); err != nil {
			log.Println(err)
			return
		}
	}
}

func checkMemoryUsage() bool {
	// Memory load shedding check
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memoryUtilizationPercent := (memStats.Alloc * 100) / memStats.Sys

	if memoryUtilizationPercent > loadSheddingThreshold {
		return false
	}
	return true
}

func logConnectionCount() int32 {
	return atomic.LoadInt32(&activeConnection)
}

func incrementConnections() {
	atomic.AddInt32(&activeConnection, 1)
}

func decrementConnections() {
	atomic.AddInt32(&activeConnection, -1)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK) // Respond with 200 for healthy status
	fmt.Fprintf(w, "OK")
}

func main() {
	router := mux.NewRouter() // Use a router for cleaner URL handling
	router.HandleFunc("/ws", handleConnections)
	router.HandleFunc("/health", healthCheck) // Add health check endpoint

	fmt.Println("Server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}
