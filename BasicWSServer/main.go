package main

import (
	"encoding/json"
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
	"github.com/aws/aws-sdk-go/service/ec2"
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
	// Connection tracking
	activeConnection          int32 // Using atomic int32 for thread-safe operations
	droppedRequests           int32
	cumulativeDroppedRequests int64
	lastDroppedRequestLog     time.Time

	// Server info
	hostname, _ = os.Hostname()

	// AWS Service Discovery related
	serviceDiscoveryClient *servicediscovery.ServiceDiscovery
	serviceID              string
	instanceID             string
	publicIP               string
	namespaceId            string
)

// WebSocket configuration
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (for testing; restrict in production)
	},
}

// Initialization
func init() {
	initializeAWS()
	go monitorStats()
}

func initializeAWS() {
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

	// Get public IP
	var err error
	publicIP, err = getPublicIPFromPrivateIP()
	if err != nil {
		log.Printf("Failed to get public IP: %v", err)
	}

	log.Printf("Hostname: %s - instanceId: %s, namespaceId: %s, serviceId: %s, publicIP: %s", hostname, instanceID, namespaceId, serviceID, publicIP)
}

// Monitoring and stats
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
			hostname, memoryUtilizationPercent, cpuUsage, connections, droppedRequests, cumulativeDroppedRequests)

		// Reset counters
		atomic.StoreInt32(&droppedRequests, 0)
		if atomic.LoadInt64(&cumulativeDroppedRequests) > (1<<63 - 1000000) {
			atomic.StoreInt64(&cumulativeDroppedRequests, 0)
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

// AWS Service Discovery functions
func getPublicIPFromPrivateIP() (string, error) {
	metadataURI := os.Getenv("ECS_CONTAINER_METADATA_URI_V4")
	if metadataURI == "" {
		return "", fmt.Errorf("ECS_CONTAINER_METADATA_URI_V4 environment variable is not set")
	}

	// Fetch metadata
	resp, err := http.Get(metadataURI + "/task")
	if err != nil {
		return "", fmt.Errorf("failed to fetch task metadata: %v", err)
	}
	defer resp.Body.Close()

	var metadata struct {
		Containers []struct {
			Networks []struct {
				IPv4Addresses []string `json:"IPv4Addresses"`
			} `json:"Networks"`
		} `json:"Containers"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read metadata response: %v", err)
	}

	err = json.Unmarshal(body, &metadata)
	if err != nil {
		return "", fmt.Errorf("failed to decode task metadata: %v", err)
	}

	if len(metadata.Containers) == 0 || len(metadata.Containers[0].Networks) == 0 {
		return "", fmt.Errorf("no network data found in metadata")
	}

	privateIP := metadata.Containers[0].Networks[0].IPv4Addresses[0]
	if privateIP == "" {
		return "", fmt.Errorf("no private IP found in metadata")
	}

	return getPublicIPFromEC2(privateIP)
}

func getPublicIPFromEC2(privateIP string) (string, error) {
	sess := session.Must(session.NewSession())
	ec2Client := ec2.New(sess)

	result, err := ec2Client.DescribeNetworkInterfaces(&ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("addresses.private-ip-address"),
				Values: []*string{aws.String(privateIP)},
			},
		},
	})

	resultJSON, _ := json.Marshal(result)
	log.Printf("DescribeNetworkInterfaces result: %s, privateIP: %s", string(resultJSON), privateIP)
	if err != nil {
		return "", fmt.Errorf("failed to describe network interfaces: %v", err)
	}

	if len(result.NetworkInterfaces) == 0 || result.NetworkInterfaces[0].Association == nil {
		return "", fmt.Errorf("no public IP found for private IP: %s", privateIP)
	}

	return *result.NetworkInterfaces[0].Association.PublicIp, nil
}

func updateServiceDiscovery(connections int32) {
	attributes := make(map[string]*string)
	attributes["ACTIVE_CONNECTIONS"] = aws.String(fmt.Sprintf("%d", connections))
	attributes["INSTANCE_PUBLIC_IPV4"] = aws.String(publicIP)

	_, err := serviceDiscoveryClient.RegisterInstance(&servicediscovery.RegisterInstanceInput{
		ServiceId:  aws.String(serviceID),
		InstanceId: aws.String(instanceID),
		Attributes: attributes,
	})
	if err != nil {
		log.Printf("Failed to update service discovery: %v", err)
	}
}

// Connection handling
func handleConnections(w http.ResponseWriter, r *http.Request) {
	if !checkMemoryUsage() {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		atomic.AddInt32(&droppedRequests, 1)
		atomic.AddInt64(&cumulativeDroppedRequests, 1)
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
