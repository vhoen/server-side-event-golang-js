// https://thoughtbot.com/blog/writing-a-server-sent-events-server-in-go

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/fsnotify/fsnotify"
)

type Server struct {
	Notifier       chan []byte
	newClients     chan chan []byte
	closingClients chan chan []byte
	clients        map[chan []byte]bool
}

func NewServer() (server *Server) {
	server = &Server{
		Notifier:       make(chan []byte, 1),
		newClients:     make(chan chan []byte),
		closingClients: make(chan chan []byte),
		clients:        make(map[chan []byte]bool),
	}

	go server.listen()

	return
}

func (server *Server) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	userId := req.URL.Query().Get("userId")
	log.Printf("userId: %s", userId)

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("Access-Control-Allow-Origin", "*")

	messageChan := make(chan []byte)

	server.newClients <- messageChan
	defer func() {
		server.closingClients <- messageChan
	}()

	notify := req.Context().Done()

	go func() {
		<-notify
		server.closingClients <- messageChan
	}()

	for {
		fmt.Fprintf(rw, "data: %s\n\n", <-messageChan)
		flusher.Flush()
	}

}

func (server *Server) listen() {
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

func main() {
	server := NewServer()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Println("ERROR", err)
	}
	defer watcher.Close()

	done := make(chan bool)

	fileName := "./data.log"

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Println("event:", event)
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Println("modified file:", event.Name)
					d, err := ioutil.ReadFile(fileName)
					if err != nil {
						continue
					}

					log.Println("Receiving event: ", string(d))
					server.Notifier <- d
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()

	if err := watcher.Add(fileName); err != nil {
		log.Println("ERROR", err)
	} else {
		log.Println("Watcher started")
	}
	<-done

	log.Fatal("HTTP server error: ", http.ListenAndServe("localhost:8000", server))

}
