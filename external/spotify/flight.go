package spotify

import "sync"

// flight runs one call of an operation at a time. Callers that arrive while
// a call runs wait for it and share its result, so the UI's parallel fetches
// on a provider switch cost one round of requests, not one per caller.
// The shared result must be treated as read-only.
type flight[T any] struct {
	mu   sync.Mutex
	call *flightCall[T]
}

type flightCall[T any] struct {
	done chan struct{}
	val  T
	err  error
}

func (f *flight[T]) do(fn func() (T, error)) (T, error) {
	f.mu.Lock()
	if c := f.call; c != nil {
		f.mu.Unlock()
		<-c.done
		return c.val, c.err
	}
	c := &flightCall[T]{done: make(chan struct{})}
	f.call = c
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.call = nil
		f.mu.Unlock()
		close(c.done)
	}()
	c.val, c.err = fn()
	return c.val, c.err
}
