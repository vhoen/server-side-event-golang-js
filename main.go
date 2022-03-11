package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/mux"

	"github.com/sirupsen/logrus"
)

type EventServer struct {
	Notifier       chan []byte
	newClients     chan chan []byte
	closingClients chan chan []byte
	clients        map[chan []byte]bool
}

func NewEventServer() (server *EventServer) {
	server = &EventServer{
		Notifier:       make(chan []byte, 1),
		newClients:     make(chan chan []byte),
		closingClients: make(chan chan []byte),
		clients:        make(map[chan []byte]bool),
	}

	go server.listen()

	return
}

func (server *EventServer) listen() {
	for {
		select {
		case s := <-server.newClients:
			server.clients[s] = true
			log.Printf("Client added. %d registered clients", len(server.clients))

		case s := <-server.closingClients:
			delete(server.clients, s)
			log.Printf("Removed client. %d registered clients", len(server.clients))

		case event := <-server.Notifier:
			for clientMessageChan := range server.clients {
				clientMessageChan <- event
			}
		}
	}
}

func startMockEvents(server *EventServer) {
	for {
		txt := time.Now().Format("2006-01-02 15:04:05")
		logrus.Info("sending event: ", txt)
		server.Notifier <- []byte(txt)
		time.Sleep(time.Second * 2)
	}
}

func main() {
	Timeout := time.Minute * 1

	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/server-side-event/{userId}", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			err := fmt.Errorf("streaming unsupported")
			logrus.Info(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		messageChan := make(chan []byte)

		server := NewEventServer()

		go startMockEvents(server)

		server.newClients <- messageChan
		defer func() {
			server.closingClients <- messageChan
		}()

		for {
			fmt.Fprintf(w, "data: %s\n\n", <-messageChan)
			flusher.Flush()
		}

		notify := r.Context().Done()
		go func() {
			<-notify
			server.closingClients <- messageChan
		}()

	}).Methods(http.MethodGet, http.MethodOptions)

	server := &http.Server{
		Addr:         "0.0.0.0:8000",
		WriteTimeout: 0,
		ReadTimeout:  0,
		IdleTimeout:  0,
		Handler:      router,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil {
			logrus.Fatal(err)
		}
	}()

	// Process signals channel
	sigChannel := make(chan os.Signal, 1)

	// Graceful shutdown via SIGINT
	signal.Notify(sigChannel, os.Interrupt)

	logrus.Info("Service running...")
	<-sigChannel // Block until SIGINT received

	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	server.Shutdown(ctx)

	logrus.Info("Http Service shutdown")

}
