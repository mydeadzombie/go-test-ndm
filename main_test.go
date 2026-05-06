package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func startServer(t *testing.T) *httptest.Server {
	t.Helper()
	mu.Lock()
	qs = map[string]*queue{}
	mu.Unlock()
	return httptest.NewServer(http.HandlerFunc(handler))
}

func doPut(base, q, v string) int {
	req, _ := http.NewRequest("PUT", base+"/"+q+"?v="+v, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

func doGet(base, q, timeout string) (int, string) {
	u := base + "/" + q
	if timeout != "" {
		u += "?timeout=" + timeout
	}
	resp, err := http.Get(u)
	if err != nil {
		return 0, ""
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, string(b)
}

// Concurrent producers and consumers on the same queue; every message must
// be delivered exactly once. Run with `go test -race`.
func TestRace_ProducersConsumers(t *testing.T) {
	s := startServer(t)
	defer s.Close()
	const P, M = 8, 100
	var wg sync.WaitGroup
	for i := range P {
		wg.Go(func() {
			for j := range M {
				if c := doPut(s.URL, "q", fmt.Sprintf("p%d-%d", i, j)); c != 200 {
					t.Errorf("PUT got %d", c)
				}
			}
		})
	}
	received := make(chan string, P*M)
	var got int64
	for range P {
		wg.Go(func() {
			for atomic.LoadInt64(&got) < P*M {
				if code, body := doGet(s.URL, "q", "1"); code == 200 {
					received <- body
					atomic.AddInt64(&got, 1)
				}
			}
		})
	}
	wg.Wait()
	close(received)
	seen := map[string]int{}
	for v := range received {
		seen[v]++
	}
	if len(seen) != P*M {
		t.Fatalf("got %d unique messages, want %d", len(seen), P*M)
	}
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("duplicate delivery: %s x%d", k, n)
		}
	}
}

// Many waiters block first, then PUTs hand them off via the waiter channel —
// stresses the waiter slice / channel handoff path.
func TestRace_WaiterHandoff(t *testing.T) {
	s := startServer(t)
	defer s.Close()
	const K = 50
	received := make(chan string, K)
	var wg sync.WaitGroup
	for range K {
		wg.Go(func() {
			code, body := doGet(s.URL, "wq", "5")
			if code != 200 {
				t.Errorf("waiter got %d", code)
				return
			}
			received <- body
		})
	}
	time.Sleep(200 * time.Millisecond) // all waiters register
	for i := range K {
		wg.Go(func() {
			doPut(s.URL, "wq", fmt.Sprintf("v%d", i))
		})
	}
	wg.Wait()
	close(received)
	seen := map[string]bool{}
	for v := range received {
		seen[v] = true
	}
	if len(seen) != K {
		t.Fatalf("delivered %d unique, want %d", len(seen), K)
	}
}

// Tightest race: many GETs with short timeout running concurrently with PUTs.
// PUT may pop a waiter whose timeout has just fired; the post-timeout recovery
// branch must still drain the buffered channel. Invariant: sent == 200s + leftover.
func TestRace_TimeoutVsPut(t *testing.T) {
	s := startServer(t)
	defer s.Close()
	const K = 200
	var got200 int64
	var wg sync.WaitGroup
	for range K {
		wg.Go(func() {
			if code, _ := doGet(s.URL, "rq", "1"); code == 200 {
				atomic.AddInt64(&got200, 1)
			}
		})
	}
	for i := range K {
		wg.Go(func() {
			doPut(s.URL, "rq", fmt.Sprintf("x%d", i))
		})
	}
	wg.Wait()
	leftover := 0
	for {
		code, _ := doGet(s.URL, "rq", "")
		if code != 200 {
			break
		}
		leftover++
	}
	if int64(leftover)+got200 != K {
		t.Fatalf("accounting failed: delivered=%d leftover=%d sent=%d", got200, leftover, K)
	}
}
