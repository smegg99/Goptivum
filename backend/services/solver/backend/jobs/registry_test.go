// jobs/registry_test.go

package jobs

import (
	"context"
	"testing"
	"time"
)

func TestPublishFanoutAndReplay(t *testing.T) {
	r := NewRegistry()
	job := r.Create(func() {})

	ch, replay, unsub := job.Subscribe()
	defer unsub()
	if replay != nil {
		t.Fatalf("expected no replay before first publish")
	}

	job.Publish([]byte("one"))
	select {
	case got := <-ch:
		if string(got) != "one" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no update received")
	}

	// A late subscriber replays the latest payload.
	_, replay2, unsub2 := job.Subscribe()
	defer unsub2()
	if string(replay2) != "one" {
		t.Fatalf("replay = %q", replay2)
	}
}

func TestFinishClosesSubscribersAndKeepsLastUpdate(t *testing.T) {
	r := NewRegistry()
	job := r.Create(func() {})
	ch, _, _ := job.Subscribe()

	job.Publish([]byte("final"))
	job.Finish(StateDone)

	<-ch // drains "final"
	if _, open := <-ch; open {
		t.Fatal("channel should be closed after Finish")
	}
	if job.State() != StateDone {
		t.Fatalf("state = %v", job.State())
	}

	// Subscribing after the end still yields the final payload.
	ch2, replay, _ := job.Subscribe()
	if string(replay) != "final" {
		t.Fatalf("replay = %q", replay)
	}
	if _, open := <-ch2; open {
		t.Fatal("post-finish channel should start closed")
	}
}

func TestCancelInvokesContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := NewRegistry()
	job := r.Create(cancel)
	job.Cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled")
	}
}
