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
		// Gestion des nouveaux clients
		case s := <-server.newClients:
			server.clients[s] = true
			log.Printf("Client added. %d registered clients", len(server.clients))

		// gestion des deconnexion de client
		case s := <-server.closingClients:
			delete(server.clients, s)
			log.Printf("Removed client. %d registered clients", len(server.clients))

		// gestion des nouveaux messages
		case event := <-server.Notifier:
			// pour chaque client on envoie le message
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
		server.Notifier <- []byte(txt) // Envoi du message
		time.Sleep(time.Second * 2)
	}
}

func main() {
	users := map[string]string{
		"azer": "Mr Azer",
	}

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

		currentUserName := "Stranger"

		vars := mux.Vars(r)
		userId := vars["userId"]
		if userId != "" {
			userName, ok := users[userId]
			if ok {
				currentUserName = userName
			}
		}

		// nouveau serveur
		server := NewEventServer()

		// On lance une routine qui va envoyer des messages à intervalles régulier
		go startMockEvents(server)

		messageChan := make(chan []byte)
		// gestion des nouvelles connexions
		server.newClients <- messageChan

		// gestion des deconnexions
		defer func() {
			server.closingClients <- messageChan
		}()
		notify := r.Context().Done()
		go func() {
			<-notify
			server.closingClients <- messageChan
		}()

		// on envoie les message au ResponseWriter
		for {
			msg := fmt.Sprintf("Hello %s, it's : %s", currentUserName, <-messageChan)
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}

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
