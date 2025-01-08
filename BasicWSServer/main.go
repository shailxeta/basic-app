package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux" // Added for routing
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (for testing; restrict in production)
	},
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

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
