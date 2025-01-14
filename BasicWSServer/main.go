package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/servicediscovery"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// Constants
const (
	statsLogDuration      = 10
	loadSheddingThreshold = 50
)

// Global variables
var (
	activeConnection           int32 // Using atomic int32 for thread-safe operations
	droppedRequests            int32
	cummulativeDroppedRequests int64
	lastDroppedRequestLog      time.Time
	hostname, _                = os.Hostname()

	// AWS Service Discovery related
	serviceDiscoveryClient *servicediscovery.ServiceDiscovery
	serviceID              string
	instanceID             string
	publicIP               string // Add publicIP variable
	namespaceId            string
)

// WebSocket upgrader configuration
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (for testing; restrict in production)
	},
}

func init() {
	// Initialize AWS Service Discovery client
	sess := session.Must(session.NewSession())
	serviceDiscoveryClient = servicediscovery.New(sess)

	// Get service and instance IDs from environment variables
	serviceID = os.Getenv("CLOUD_MAP_SERVICE_ID")
	namespaceId = os.Getenv("CLOUD_MAP_NAMESPACE_ID")
	metadataURI := os.Getenv("ECS_CONTAINER_METADATA_URI_V4")

	if metadataURI == "" {
		log.Printf("Hostname: %s - ECS_CONTAINER_METADATA_URI_V4 environment variable must be set", hostname)
	}

	// Extract task ID from metadata URI - format is http://<ip>/<version>/<id>
	parts := strings.Split(metadataURI, "/")
	instanceID = strings.Split(parts[len(parts)-1], "-")[0] // First part before hyphen is the instance ID

	if serviceID == "" || instanceID == "" {
		log.Printf("Hostname: %s - SERVICE_ID and INSTANCE_ID environment variables must be set", hostname)
	}
	log.Printf("Hostname: %s - instanceId: %s, serviceId: %s", hostname, instanceID, serviceID)

	metadataURL := os.Getenv("ECS_CONTAINER_METADATA_URI_V4") + "/task"
	resp, err := http.Get(metadataURL)
	if err != nil {
		log.Fatalf("failed to get task metadata: %w", err)
	}
	defer resp.Body.Close()

	//var metadata struct {
	//	Containers []struct {
	//		Networks []struct {
	//			IPv4Addresses        []string `json:"IPv4Addresses"`
	//			PublicIPv4Address    string   `json:"PublicIPv4Address"` // Get public IP here
	//			ServiceDiscoveryInfo []struct {
	//				InstanceId string `json:"InstanceId"`
	//			} `json:"ServiceDiscoveryInfo"`
	//		} `json:"Networks"`
	//	} `json:"Containers"`
	//}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("failed to read task metadata: %v", err)
	}
	log.Printf("Task metadata response: %s", string(body))
	//if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
	//	log.Fatalf("failed to decode task metadata: %w", err)
	//}
	//
	//if len(metadata.Containers) == 0 || len(metadata.Containers[0].Networks) == 0 {
	//	log.Fatalf("networks not found in metadata")
	//}
	//
	//instanceID = metadata.Containers[0].Networks[0].ServiceDiscoveryInfo[0].InstanceId
	//
	//ipAddress := metadata.Containers[0].Networks[0].IPv4Addresses[0]
	//publicIP = metadata.Containers[0].Networks[0].PublicIPv4Address // Assign public IP
	//
	//fmt.Println("public ip:", publicIP)     // Add this line for debugging
	//fmt.Println("private ip:", ipAddress)   // Add this line for debugging
	//fmt.Println("instance id:", instanceID) // Add this line for debugging
	// Start stats monitoring goroutine
	go monitorStats()
}

func monitorStats() {
	ticker := time.NewTicker(statsLogDuration * time.Second)
	for range ticker.C {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		memoryUtilizationPercent := (memStats.Alloc * 100) / memStats.Sys
		connections := logConnectionCount()

		// Calculate CPU usage
		cpuUsage := calculateCPUUsage()

		// Update AWS Service Discovery
		updateServiceDiscovery(connections)

		// Log stats
		log.Printf("Hostname: %s - Stats - Memory utilization: %d%%, CPU utilization: %f, Active connections: %d, Dropped requests %d, Cumulative Dropped Requests: %d",
			hostname, memoryUtilizationPercent, cpuUsage, connections, droppedRequests, cummulativeDroppedRequests)

		// Reset counters
		atomic.StoreInt32(&droppedRequests, 0)
		if atomic.LoadInt64(&cummulativeDroppedRequests) > (1<<63 - 1000000) {
			atomic.StoreInt64(&cummulativeDroppedRequests, 0)
		}
	}
}

func calculateCPUUsage() float64 {
	startTime := time.Now()
	startCPU := runtime.NumCPU()
	time.Sleep(100 * time.Millisecond)
	duration := time.Since(startTime).Seconds()
	cpuUsage := float64(runtime.NumGoroutine()) / (float64(startCPU) * duration) * 100
	if cpuUsage > 100 {
		cpuUsage = 100
	}
	return cpuUsage
}

func updateServiceDiscovery(connections int32) {
	getInstanceResp, err := serviceDiscoveryClient.GetInstance(&servicediscovery.GetInstanceInput{
		ServiceId:  aws.String(serviceID),
		InstanceId: aws.String(instanceID),
	})
	if err != nil {
		log.Printf("Failed to get instance: %v", err)
		return
	}

	attributes := getInstanceResp.Instance.Attributes
	if attributes == nil {
		attributes = make(map[string]*string)
	}
	attributes["ACTIVE_CONNECTIONS"] = aws.String(fmt.Sprintf("%d", connections))
	attributes["AWS_INSTANCE_PUBLIC_IPV4"] = aws.String(fmt.Sprintf("%d", connections))

	_, err = serviceDiscoveryClient.RegisterInstance(&servicediscovery.RegisterInstanceInput{
		ServiceId:  aws.String(serviceID),
		InstanceId: aws.String(instanceID),
		Attributes: attributes,
	})
	if err != nil {
		log.Printf("Failed to update service discovery: %v", err)
	}
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	if !checkMemoryUsage() {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		atomic.AddInt32(&droppedRequests, 1)
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
			return
		}

		if err := ws.WriteMessage(messageType, p); err != nil {
			return
		}
	}
}

// Helper functions
func checkMemoryUsage() bool {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memoryUtilizationPercent := (memStats.Alloc * 100) / memStats.Sys
	return memoryUtilizationPercent <= loadSheddingThreshold
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

// HTTP Handlers
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
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
	router := mux.NewRouter()
	router.HandleFunc("/ws", handleConnections)
	router.HandleFunc("/health", healthCheck)
	router.HandleFunc("/load-shedding", healthCheckWithLoadShedding)

	log.Printf("Hostname: %s - Server listening on :8080", hostname)
	log.Fatal(http.ListenAndServe(":8080", router))
}
