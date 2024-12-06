// hub/hub.go
package hub

import (
	"context"
	"fmt"
	"net/http"
	"crypto/tls"
	"sync"
	"time"

	"smuggr.xyz/goptivum/core/observer"
)

type Hub struct {
	observers   map[int64]*observer.Observer
	addCh       chan *observer.Observer
	removeCh    chan interface{}
	tasksCh     chan *observer.Observer
	workerCount int64
	wg          sync.WaitGroup
	mu          sync.RWMutex
	quitCh      chan struct{}
	client      *http.Client
}

func NewHub(workerCount int64, ignoreCertificates bool) *Hub {
	transport := &http.Transport{
		MaxIdleConns:        2000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:    &tls.Config{
			// #nosec G402
			InsecureSkipVerify: ignoreCertificates,
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return &Hub{
		observers:   make(map[int64]*observer.Observer),
		addCh:       make(chan *observer.Observer),
		removeCh:    make(chan interface{}),
		tasksCh:     make(chan *observer.Observer, 1000),
		workerCount: workerCount,
		quitCh:      make(chan struct{}),
		client:      client,
	}
}

func (h *Hub) Start() {
	for i := int64(0); i < h.workerCount; i++ {
		h.wg.Add(1)
		go h.worker(i)
	}

	h.wg.Add(1)
	go h.scheduler()

	fmt.Printf("hub started with %d worker(s)\n", h.workerCount)
}

func (h *Hub) Stop() {
	close(h.quitCh)
	h.wg.Wait()
	fmt.Println("hub has been stopped gracefully")
}

func (h *Hub) AddObserver(o *observer.Observer) {
	h.addCh <- o
}

func (h *Hub) RemoveObserver(index int64) {
	h.removeCh <- index
}

func (h *Hub) GetObserver(index int64) *observer.Observer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.observers[index]
}

func (h *Hub) GetAllObservers(ignoreFirst bool) map[int64]*observer.Observer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	copyMap := make(map[int64]*observer.Observer, len(h.observers))
	if ignoreFirst {
		delete(copyMap, 0)
	}
	for k, v := range h.observers {
		copyMap[k] = v
	}
	return copyMap
}

func (h *Hub) worker(id int64) {
	defer h.wg.Done()
	fmt.Printf("worker %d started\n", id)

	for {
		select {
		case o := <-h.tasksCh:
			fmt.Printf("worker %d: processing observer with URL: %s\n", id, o.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

			// It's safe to defer cancel here because it's called before the next iteration
			// However, to avoid multiple defers in a loop, I'll call cancel manually
			changed := o.CompareHashWithClient(ctx, h.client)
			cancel()

			if changed {
				if o.Callback != nil {
					go o.Callback(o)
				} else {
					fmt.Printf("worker %d: no callback for observer with URL: %s\n", id, o.URL)
				}
			}

		case <-h.quitCh:
			fmt.Printf("stopping worker of id %d\n", id)
			return
		}
	}
}

func (h *Hub) scheduler() {
	defer h.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case o := <-h.addCh:
			h.mu.Lock()
			if _, exists := h.observers[o.Index]; exists {
				fmt.Printf("observer of index %d already exists\n", o.Index)
			} else {
				o.Mu.Lock()
				o.Hash = ""
				o.Mu.Unlock()
				h.observers[o.Index] = o
				fmt.Printf("added observer of index: %d\n", o.Index)
			}
			h.mu.Unlock()

		case _index := <-h.removeCh:
			index, ok := _index.(int64)
			if !ok {
				fmt.Printf("invalid type for index: %T\n", _index)
				continue
			}

			h.mu.Lock()
			if o, exists := h.observers[index]; exists {
				o.Mu.Lock()
				delete(h.observers, index)
				fmt.Printf("removed observer of index: %d\n", index)
				o.Hash = ""
				o.Mu.Unlock()
			} else {
				fmt.Printf("observer of index %d does not exist\n", index)
			}
			h.mu.Unlock()

		case <-ticker.C:
			now := time.Now()
			h.mu.RLock()
			for _, o := range h.observers {
				o.Mu.Lock()
				if o.NextRun.Before(now) || o.NextRun.Equal(now) {
					o.NextRun = now.Add(o.Interval)
					o.FirstRun = false
					select {
					case h.tasksCh <- o:
					case <-time.After(10 * time.Second):
						fmt.Printf("timed out sending observer with URL: %s to tasksCh\n", o.URL)
					}
				}
				o.Mu.Unlock()
			}
			h.mu.RUnlock()

		case <-h.quitCh:
			fmt.Println("stopping scheduler")
			return
		}
	}
}
