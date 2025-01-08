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

	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Printf("dial %d: %v", id, err)
		return
	}
	defer c.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Printf("read %d: %v", id, err)
				return
			}
			log.Printf("recv %d: %s", id, message)
		}
	}()

	ticker := time.NewTicker(5 * time.Second) // Send "hello" every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			message := fmt.Sprintf("hello from client %d", id)
			err := c.WriteMessage(websocket.TextMessage, []byte(message))
			if err != nil {
				log.Printf("write %d: %v", id, err)
				return
			}
		case <-interrupt:
			log.Printf("interrupt %d", id)

			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Printf("write close %d: %v", id, err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}

func main() {
	numConnections := 50 // Default number of connections
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
