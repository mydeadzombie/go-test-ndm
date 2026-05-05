package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type queue struct {
	msgs    []string
	waiters []chan string
}

var (
	mu sync.Mutex
	qs = map[string]*queue{}
)

func get(name string) *queue {
	q, ok := qs[name]
	if !ok {
		q = &queue{}
		qs[name] = q
	}
	return q
}

func handler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[1:]
	if name == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		v := r.URL.Query().Get("v")
		if v == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		q := get(name)
		for len(q.waiters) > 0 {
			ch := q.waiters[0]
			q.waiters = q.waiters[1:]
			select {
			case ch <- v:
				mu.Unlock()
				return
			default:
				// ...
			}
		}
		q.msgs = append(q.msgs, v)
		mu.Unlock()
	case http.MethodGet:
		mu.Lock()
		q := get(name)
		if len(q.msgs) > 0 {
			m := q.msgs[0]
			q.msgs = q.msgs[1:]
			mu.Unlock()
			fmt.Fprint(w, m)
			return
		}
		t, _ := strconv.Atoi(r.URL.Query().Get("timeout"))
		if t <= 0 {
			mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ch := make(chan string, 1)
		q.waiters = append(q.waiters, ch)
		mu.Unlock()
		select {
		case m := <-ch:
			fmt.Fprint(w, m)
		case <-time.After(time.Duration(t) * time.Second):
			mu.Lock()
			for i, c := range q.waiters {
				if c == ch {
					q.waiters = append(q.waiters[:i], q.waiters[i+1:]...)
					break
				}
			}
			mu.Unlock()
			select {
			case m := <-ch:
				fmt.Fprint(w, m)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "для запуска: <binary> <port>")
		os.Exit(1)
	}
	http.HandleFunc("/", handler)
	if err := http.ListenAndServe(":"+os.Args[1], nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
