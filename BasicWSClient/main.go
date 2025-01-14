package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WSClient struct {
	url         string
	id          int
	conn        *websocket.Conn
	done        chan struct{}
	interrupt   chan os.Signal
	ticker      *time.Ticker
	retryDelay  time.Duration
	retryCount  int
	maxRetries  int
	originalURL string
	isClosed    bool
	mu          sync.Mutex // Mutex to protect isClosed
}

func NewWSClient(url string, id int) *WSClient {
	return &WSClient{
		url:         url,
		originalURL: url,
		id:          id,
		done:        make(chan struct{}),
		interrupt:   make(chan os.Signal, 1),
		retryDelay:  5 * time.Second,
		maxRetries:  3,
		isClosed:    false,
	}
}

func (c *WSClient) handleRedirect() (*websocket.Conn, *http.Response, error) {
	conn, resp, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil && resp != nil {
		if resp.StatusCode == http.StatusTemporaryRedirect ||
			resp.StatusCode == http.StatusMovedPermanently ||
			resp.StatusCode == http.StatusFound {
			redirectURL := resp.Header.Get("Location")
			if redirectURL != "" {
				log.Printf("Following redirect to: %s", redirectURL)
				return websocket.DefaultDialer.Dial(redirectURL, nil)
			}
		}
	}
	return conn, resp, err
}

func (c *WSClient) connect() error {
	var resp *http.Response
	var err error

	c.conn, resp, err = c.handleRedirect()
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial %d: %v (status: %d)", c.id, err, resp.StatusCode)
		}
		return fmt.Errorf("dial %d: %v", c.id, err)
	}
	return nil
}

func (c *WSClient) closeOnce() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isClosed {
		close(c.done)
		c.isClosed = true
	}
}

func (c *WSClient) readPump() {
	defer c.closeOnce()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("client %d: connection closed normally", c.id)
			} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("read %d: unexpected close error: %v", c.id, err)
			} else {
				var netErr *websocket.CloseError
				if errors.As(err, &netErr) {
					log.Printf("read %d: websocket close error: %v", c.id, netErr)
				}
			}
			return
		}
		log.Printf("recv %d: %s", c.id, message)
	}
}

func (c *WSClient) writePump() error {
	message := fmt.Sprintf("hello from client %d", c.id)
	return c.conn.WriteMessage(websocket.TextMessage, []byte(message))
}

func (c *WSClient) cleanup() {
	if c.ticker != nil {
		c.ticker.Stop()
	}
	// Close connection after stopping ticker
	if c.conn != nil {
		// Attempt graceful closure first
		c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.conn.Close()
	}
}

func (c *WSClient) Run(wg *sync.WaitGroup) {
	defer wg.Done()
	signal.Notify(c.interrupt, os.Interrupt)

	for {
		// Reset done channel and isClosed flag for new connection attempt
		c.mu.Lock()
		if c.isClosed {
			c.done = make(chan struct{})
			c.isClosed = false
		}
		c.mu.Unlock()

		if err := c.connect(); err != nil {
			log.Printf("%v, retrying in %v...", err, c.retryDelay)
			c.retryCount++

			if c.retryCount >= c.maxRetries {
				log.Printf("Retry count exceeded for client %d, reconnecting to proxy...", c.id)
				c.url = c.originalURL
				c.retryCount = 0
			}

			time.Sleep(c.retryDelay)
			continue
		}

		c.retryCount = 0
		c.ticker = time.NewTicker(5 * time.Second)
		go c.readPump()

	loop:
		for {
			select {
			case <-c.done:
				if closeErr := c.conn.CloseHandler()(websocket.CloseAbnormalClosure, ""); closeErr != nil {
					log.Printf("connection %d closed abnormally: %v", c.id, closeErr)
				}
				break loop

			case <-c.ticker.C:
				if err := c.writePump(); err != nil {
					log.Printf("write %d: %v", c.id, err)
					break loop
				}

			case <-c.interrupt:
				log.Printf("interrupt %d", c.id)
				err := c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					log.Printf("write close %d: %v", c.id, err)
				}
				select {
				case <-c.done:
				case <-time.After(time.Second):
				}
				return
			}
		}

		c.cleanup()
		log.Printf("Connection %d lost, reconnecting...", c.id)
		time.Sleep(time.Second)
	}
}

func main() {
	numConnections := 5
	if len(os.Args) > 1 {
		var err error
		numConnections, err = strconv.Atoi(os.Args[1])
		if err != nil {
			log.Fatal("Invalid number of connections:", err)
		}
	}

	fmt.Println("connecting...")
	url := "ws://shailxeta-proxy-lb-240109791.us-west-2.elb.amazonaws.com/ws"
	if len(os.Args) > 2 {
		url = os.Args[2]
	}

	fmt.Println("connecting...")
	var wg sync.WaitGroup
	for i := 1; i <= numConnections; i++ {
		wg.Add(1)
		client := NewWSClient(url, i)
		go client.Run(&wg)
	}

	wg.Wait()
	fmt.Println("All connections finished.")
}
