package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/servicediscovery"
)

type Instance struct {
	ID                string
	Host              string
	ActiveConnections int
}

type WSProxy struct {
	serverAddr            string
	serviceDiscoveryID    string
	serviceDiscoveryNS    string
	serviceDiscoveryCache []Instance
	cacheMutex            sync.RWMutex
	sdClient              *servicediscovery.ServiceDiscovery
}

func NewWSProxy(serverAddr string) *WSProxy {
	sess := session.Must(session.NewSession())
	sdClient := servicediscovery.New(sess)

	return &WSProxy{
		serverAddr:         serverAddr,
		serviceDiscoveryID: os.Getenv("CLOUD_MAP_SERVICE_ID"),
		serviceDiscoveryNS: os.Getenv("CLOUD_MAP_NAMESPACE_ID"),
		sdClient:           sdClient,
	}
}

func (p *WSProxy) updateServiceDiscoveryCache() error {
	input := &servicediscovery.ListInstancesInput{
		ServiceId: aws.String(p.serviceDiscoveryID),
	}

	result, err := p.sdClient.ListInstances(input)
	if err != nil {
		return fmt.Errorf("failed to list instances: %v", err)
	}

	instances := make([]Instance, 0)
	for _, inst := range result.Instances {
		connections := 0
		if val, ok := inst.Attributes["ACTIVE_CONNECTIONS"]; ok {
			connections, _ = strconv.Atoi(*val)
		}

		instances = append(instances, Instance{
			ID:                *inst.Id,
			Host:              *inst.Attributes["AWS_INSTANCE_IPV4"],
			ActiveConnections: connections,
		})
	}

	p.cacheMutex.Lock()
	p.serviceDiscoveryCache = instances
	p.cacheMutex.Unlock()

	return nil
}

func (p *WSProxy) getLeastLoadedInstance() (*Instance, error) {
	p.cacheMutex.RLock()
	defer p.cacheMutex.RUnlock()

	if len(p.serviceDiscoveryCache) == 0 {
		return nil, fmt.Errorf("no instances available")
	}

	leastLoaded := p.serviceDiscoveryCache[0]
	for _, instance := range p.serviceDiscoveryCache {
		if instance.ActiveConnections < leastLoaded.ActiveConnections {
			leastLoaded = instance
		}
	}

	return &leastLoaded, nil
}

func (p *WSProxy) proxyHandler(w http.ResponseWriter, r *http.Request) {
	instance, err := p.getLeastLoadedInstance()
	if err != nil {
		log.Printf("Failed to get instance: %v", err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	targetURL := fmt.Sprintf("ws://%s/ws", instance.Host)
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
}

func (p *WSProxy) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (p *WSProxy) Start() error {
	// Start service discovery cache update routine
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for range ticker.C {
			if err := p.updateServiceDiscoveryCache(); err != nil {
				log.Printf("Failed to update service discovery cache: %v", err)
			}
		}
	}()

	http.HandleFunc("/ws", p.proxyHandler)
	http.HandleFunc("/health", p.healthHandler)

	log.Printf("Starting WebSocket proxy server on %s", p.serverAddr)
	return http.ListenAndServe(p.serverAddr, nil)
}

func main() {
	proxy := NewWSProxy(":8080")
	if err := proxy.Start(); err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}
