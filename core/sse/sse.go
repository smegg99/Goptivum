package sse

import (
    "fmt"
    "net/http"
    "sync"
    "time"
)

type Client struct {
    MessageChan chan interface{}
    Done        chan struct{}
}

type Hub struct {
    clients            map[*Client]bool
    mu                 sync.RWMutex
    maxClients         int16
    broadcast          chan interface{}
    register           chan *Client
    unregister         chan *Client
    retryDelay         int
    unregisterCallback func()
    registerCallback   func()
}

func NewHub(maxClients int16, unregisterCallback func(), registerCallback func()) *Hub {
    return &Hub{
        clients:            make(map[*Client]bool),
        maxClients:         maxClients,
        broadcast:          make(chan interface{}, 100),
        register:           make(chan *Client),
        unregister:         make(chan *Client),
        retryDelay:         3000,
        unregisterCallback: unregisterCallback,
        registerCallback:   registerCallback,
    }
}

func (h *Hub) Run() {
    for {
        select {
        case client := <-h.register:
            h.mu.Lock()
            if len(h.clients) < int(h.maxClients) {
                h.clients[client] = true
                fmt.Println("client registered, total:", len(h.clients))
                h.onRegisterCallback()
            } else {
                fmt.Printf("max clients reached (%d), client rejected\n", h.maxClients)
                close(client.MessageChan)
                close(client.Done)
            }
            h.mu.Unlock()

        case client := <-h.unregister:
            h.mu.Lock()
            if _, ok := h.clients[client]; ok {
                delete(h.clients, client)
                close(client.MessageChan)
                close(client.Done)
                h.onUnregisterCallback()
                fmt.Println("client unregistered, total:", len(h.clients))
            }
            h.mu.Unlock()

        case message := <-h.broadcast:
            h.mu.RLock()
            for client := range h.clients {
                select {
                case client.MessageChan <- message:
                case <-time.After(10 * time.Second):
                    fmt.Println("client message timed out, removing client")
                    h.mu.RUnlock()
                    h.unregister <- client
                    h.onUnregisterCallback()
                    h.mu.RLock()
                }
            }
            h.mu.RUnlock()
        }
    }
}

func (h *Hub) Broadcast(message interface{}) {
    select {
    case h.broadcast <- message:
    default:
        fmt.Println("broadcast channel full, dropping message")
    }
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
            if f, ok := w.(http.Flusher); ok {
                f.Flush()
            }
        }

        client := &Client{
            MessageChan: make(chan interface{}, 10),
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
                fmt.Fprintf(w, "data: %v\n\n", message)
                if f, ok := w.(http.Flusher); ok {
                    f.Flush()
                }
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
    h.Broadcast(formattedMessage)
}

func (h *Hub) SendPeriodicMessages(interval time.Duration, messageFunc func() string) {
    ticker := time.NewTicker(interval)
    go func() {
        for {
            select {
            case <-ticker.C:
                message := messageFunc()
                h.Broadcast(message)
            case <-time.After(10 * time.Second):
                fmt.Println("periodic message timed out")
                return
            }
        }
    }()
}

func (h *Hub) GetConnectedClients() int64 {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return int64(len(h.clients))
}

func (h *Hub) onRegisterCallback() {
    if h.registerCallback != nil {
        h.registerCallback()
    }
}

func (h *Hub) onUnregisterCallback() {
    if h.unregisterCallback != nil {
        h.unregisterCallback()
    }
}