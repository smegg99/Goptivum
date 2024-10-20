// sse/sse.go
package sse

import (
    "fmt"
    "net/http"
    "sync"
    "time"
)

type Client struct {
    MessageChan chan string
    Done        chan struct{}
}

type Hub struct {
    clients    map[*Client]bool
    mutex      sync.RWMutex
    broadcast  chan string
    register   chan *Client
    unregister chan *Client
    retryDelay int
}

func NewHub() *Hub {
    return &Hub{
        clients:    make(map[*Client]bool),
        broadcast:  make(chan string),
        register:   make(chan *Client),
        unregister: make(chan *Client),
        retryDelay: 3000,
    }
}

func (h *Hub) Run() {
    for {
        select {
        case client := <-h.register:
            h.mutex.Lock()
            h.clients[client] = true
            h.mutex.Unlock()
        case client := <-h.unregister:
            h.mutex.Lock()
            if _, ok := h.clients[client]; ok {
                delete(h.clients, client)
                close(client.MessageChan)
                close(client.Done)
            }
            h.mutex.Unlock()
        case message := <-h.broadcast:
            h.mutex.RLock()
            for client := range h.clients {
                select {
                case client.MessageChan <- message:
                default:
                    delete(h.clients, client)
                    close(client.MessageChan)
                    close(client.Done)
                }
            }
            h.mutex.RUnlock()
        }
    }
}

func (h *Hub) Broadcast(message string) {
    h.broadcast <- message
}

func (h *Hub) SetRetryDelay(ms int) {
    h.retryDelay = ms
}

func (h *Hub) Handler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        w.Header().Set("Access-Control-Allow-Origin", "*")

        if h.retryDelay > 0 {
            fmt.Fprintf(w, "retry: %d\n\n", h.retryDelay)
            w.(http.Flusher).Flush()
        }

        client := &Client{
            MessageChan: make(chan string),
            Done:        make(chan struct{}),
        }

        h.register <- client
        notify := r.Context().Done()

        go func() {
            <-notify
            h.unregister <- client
        }()

        for {
            select {
            case message, ok := <-client.MessageChan:
                if !ok {
                    return
                }
                fmt.Fprintf(w, "data: %s\n\n", message)
                w.(http.Flusher).Flush()
            case <-client.Done:
                return
            }
        }
    }
}

func (h *Hub) SendMessage(message string) {
    h.Broadcast(message)
}

func (h *Hub) SendMessageWithID(id int, message string) {
    formattedMessage := fmt.Sprintf("id: %d\ndata: %s\n\n", id, message)
    h.broadcast <- formattedMessage
}

func (h *Hub) SendPeriodicMessages(interval time.Duration, messageFunc func() string) {
    ticker := time.NewTicker(interval)
    go func() {
        for {
            <-ticker.C
            message := messageFunc()
            h.Broadcast(message)
        }
    }()
}
