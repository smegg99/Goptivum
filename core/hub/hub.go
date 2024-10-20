// hub/hub.go
package hub

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"smuggr.xyz/optivum-bsf/core/observer"
)

type Hub struct {
	observers   map[int64]*observer.Observer
	addCh       chan *observer.Observer
	removeCh    chan int64
	tasksCh     chan *observer.Observer
	workerCount int
	wg          sync.WaitGroup
	quitCh      chan struct{}
	client      *http.Client
}

func NewHub(workerCount int) *Hub {
	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return &Hub{
		observers:   make(map[int64]*observer.Observer),
		addCh:       make(chan *observer.Observer),
		removeCh:    make(chan int64),
		tasksCh:     make(chan *observer.Observer, 1000),
		workerCount: workerCount,
		quitCh:      make(chan struct{}),
		client:      client,
	}
}

func (h *Hub) Start() {
	for i := 0; i < h.workerCount; i++ {
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
	return h.observers[index]
}

func (h *Hub) GetAllObservers() map[int64]*observer.Observer {
	return h.observers
}

func (h *Hub) worker(id int) {
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
				go o.Callback()
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
			if _, exists := h.observers[o.Index]; exists {
				fmt.Printf("observer of index %d already exists\n", o.Index)
			} else {
				o.Hash = ""
				h.observers[o.Index] = o
				fmt.Printf("added observer of index: %d\n", o.Index)
			}

		case index := <-h.removeCh:
			if o, exists := h.observers[index]; exists {
				delete(h.observers, index)
				fmt.Printf("removed observer of index: %d\n", index)
				// Clean from DB here maybe?
				o.Hash = ""
			} else {
				fmt.Printf("observer of index %d does not exist\n", index)
			}

		case <-ticker.C:
			now := time.Now()
			for _, o := range h.observers {
				if o.NextRun.Before(now) || o.NextRun.Equal(now) {
					o.NextRun = now.Add(o.Interval)
					h.tasksCh <- o
				}
			}

		case <-h.quitCh:
			fmt.Println("stopping scheduler")
			return
		}
	}
}
