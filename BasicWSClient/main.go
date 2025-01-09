package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func connectAndSend(url string, id int, wg *sync.WaitGroup) {
	defer wg.Done()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	for {
		c, resp, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			if resp != nil { // Check if resp is not nil before accessing StatusCode
				log.Printf("dial %d: %v (status: %d), retrying in 5 seconds...", id, err, resp.StatusCode)
			} else {
				log.Printf("dial %d: %v, retrying in 5 seconds...", id, err) // Handle nil resp
			}
			time.Sleep(5 * time.Second)
			continue // Retry connection on error
		}

		done := make(chan struct{})

		go func() {
			defer close(done)
			for {
				_, message, err := c.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("read %d: unexpected close error: %v", id, err)
					} else {
						log.Printf("read %d: %v", id, err)
					}
					return
				}
				log.Printf("recv %d: %s", id, message)
			}
		}()

		ticker := time.NewTicker(5 * time.Second) // Send "hello" every 5 seconds

		func() {
			defer ticker.Stop()
			defer c.Close()

		loop:
			for {
				select {
				case <-done:
					if closeErr := c.CloseHandler()(websocket.CloseAbnormalClosure, ""); closeErr != nil {
						log.Printf("connection %d closed abnormally: %v", id, closeErr)
					}
					break loop
				case <-ticker.C:
					message := fmt.Sprintf("hello from client %d", id)
					err := c.WriteMessage(websocket.TextMessage, []byte(message))
					if err != nil {
						log.Printf("write %d: %v", id, err)
						break loop // Break loop and retry on error
					}
				case <-interrupt:
					log.Printf("interrupt %d", id)

					err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
					if err != nil {
						log.Printf("write close %d: %v", id, err)
					}
					select {
					case <-done:
					case <-time.After(time.Second):
					}
					break loop
				}
			}
		}()

		log.Printf("Connection %d lost, reconnecting...", id)
		time.Sleep(time.Second) // Wait a bit before reconnecting
	}
}

func main() {
	numConnections := 1000 // Default number of connections
	if len(os.Args) > 1 {
		var err error
		numConnections, err = strconv.Atoi(os.Args[1])
		if err != nil {
			log.Fatal("Invalid number of connections:", err)
		}
	}
	fmt.Println("connecting...")
	url := "ws://shailxeta-lor-lb-2093919390.us-west-2.elb.amazonaws.com/ws"
	if len(os.Args) > 2 {
		url = os.Args[2]
	}

	var wg sync.WaitGroup
	for i := 1; i <= numConnections; i++ {
		wg.Add(1)
		go connectAndSend(url, i, &wg)
	}

	wg.Wait()
	fmt.Println("All connections finished.")
}
