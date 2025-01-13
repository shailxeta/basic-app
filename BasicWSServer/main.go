package main

import (
	"fmt"
	"github.com/gorilla/mux" // Added for routing
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

var activeConnection int32 // Using atomic int32 for thread-safe operations
var loadSheddingThreshold uint64 = 50
var droppedRequests int32
var cummulativeDroppedRequests int64
var lastDroppedRequestLog time.Time
var hostname, _ = os.Hostname()

const statsLogDuration = 10

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
		ticker := time.NewTicker(statsLogDuration * time.Second)
		for range ticker.C {
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			memoryUtilizationPercent := (memStats.Alloc * 100) / memStats.Sys
			connections := logConnectionCount()
			// Get CPU utilization percentage
			// Get an estimate of CPU usage by sampling over a short interval
			var cpuUsage float64
			startTime := time.Now()
			startCPU := runtime.NumCPU()
			time.Sleep(100 * time.Millisecond) // Sample for 100ms
			duration := time.Since(startTime).Seconds()
			cpuUsage = float64(runtime.NumGoroutine()) / (float64(startCPU) * duration) * 100
			if cpuUsage > 100 {
				cpuUsage = 100
			}
			log.Printf("Hostname: %s - Stats - Memory utilization: %d%%, CPU utilization: %f, Active connections: %d, Dropped requests %d, Cumulative Dropped Requests: %d",
				hostname, memoryUtilizationPercent, cpuUsage, connections, droppedRequests, cummulativeDroppedRequests)
			atomic.StoreInt32(&droppedRequests, 0) // Reset counter
			// Reset cumulative dropped requests if it exceeds int64 range to prevent overflow
			if atomic.LoadInt64(&cummulativeDroppedRequests) > (1<<63 - 1000000) {
				atomic.StoreInt64(&cummulativeDroppedRequests, 0)
			}
		}
	}()
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	if !checkMemoryUsage() {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		atomic.AddInt32(&droppedRequests, 1) // Increment dropped request counter
		atomic.AddInt64(&cummulativeDroppedRequests, 1)
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
			//log.Printf("HostName: %s, error: %s", hostname, err)
			return
		}
		//log.Printf("Hostname: %s - Message Received: %s", hostname, p)

		if err := ws.WriteMessage(messageType, p); err != nil {
			//log.Printf("Hostname: %s - Error: %s", hostname, err)
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
	//log.Printf("HostName: %s - Health check request received...", hostname)
	w.WriteHeader(http.StatusOK) // Respond with 200 for healthy status
	fmt.Fprintf(w, "OK")
}

func healthCheckWithLoadShedding(w http.ResponseWriter, r *http.Request) {
	if checkMemoryUsage() {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintf(w, "Service Unavailable")
}

func main() {
	router := mux.NewRouter() // Use a router for cleaner URL handling
	router.HandleFunc("/ws", handleConnections)
	router.HandleFunc("/health", healthCheck) // Add health check endpoint
	router.HandleFunc("/loadshedding", healthCheckWithLoadShedding)
	log.Printf("Hostname: %s - Server listening on :8080", hostname)
	log.Fatal(http.ListenAndServe(":8080", router))
}
