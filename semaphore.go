package main

// This is the semapohre implementation from golang.org/x/sync/semaphore,
// modified to add a Count() method and a SetSize() method.

import (
	"container/list"
	"context"
	"sync"
)

// NewWeighted creates a new weighted semaphore with the given
// maximum combined weight for concurrent access.
func NewWeighted(n int64) *Weighted {
	w := &Weighted{size: n}
	return w
}

// Weighted provides a way to bound concurrent access to a resource.
// The callers can request access with a given weight.
type Weighted struct {
	size    int64
	cur     int64
	mu      sync.Mutex
	waiters list.List
}

func (s *Weighted) Count() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *Weighted) SetSize(n int64) {
	s.mu.Lock()
	s.size = n
	s.notifyWaiters()
	s.mu.Unlock()
}

// Acquire acquires the semaphore with a weight of n, blocking until resources
// are available or ctx is done. On success, returns nil. On failure, returns
// ctx.Err() and leaves the semaphore unchanged.
func (s *Weighted) Acquire(ctx context.Context) error {
	done := ctx.Done()

	s.mu.Lock()
	select {
	case <-done:
		// ctx becoming done has "happened before" acquiring the semaphore,
		// whether it became done before the call began or while we were
		// waiting for the mutex. We prefer to fail even if we could acquire
		// the mutex without blocking.
		s.mu.Unlock()
		return ctx.Err()
	default:
	}
	if s.size-s.cur >= 1 && s.waiters.Len() == 0 {
		// Since we hold s.mu and haven't synchronized since checking done, if
		// ctx becomes done before we return here, it becoming done must have
		// "happened concurrently" with this call - it cannot "happen before"
		// we return in this branch. So, we're ok to always acquire here.
		s.cur++
		s.mu.Unlock()
		return nil
	}

	ready := make(chan struct{})
	elem := s.waiters.PushBack(ready)
	s.mu.Unlock()

	select {
	case <-done:
		s.mu.Lock()
		select {
		case <-ready:
			// Acquired the semaphore after we were canceled.
			// Pretend we didn't and put the tokens back.
			s.cur--
			s.notifyWaiters()
		default:
			isFront := s.waiters.Front() == elem
			s.waiters.Remove(elem)
			// If we're at the front and there're extra tokens left, notify other waiters.
			if isFront && s.size > s.cur {
				s.notifyWaiters()
			}
		}
		s.mu.Unlock()
		return ctx.Err()

	case <-ready:
		// Acquired the semaphore. Check that ctx isn't already done.
		// We check the done channel instead of calling ctx.Err because we
		// already have the channel, and ctx.Err is O(n) with the nesting
		// depth of ctx.
		select {
		case <-done:
			s.Release()
			return ctx.Err()
		default:
		}
		return nil
	}
}

// Release releases the semaphore with a weight of n.
func (s *Weighted) Release() {
	s.mu.Lock()
	s.cur--
	if s.cur < 0 {
		s.mu.Unlock()
		panic("semaphore: released more than held")
	}
	s.notifyWaiters()
	s.mu.Unlock()
}

func (s *Weighted) notifyWaiters() {
	for {
		next := s.waiters.Front()
		if next == nil {
			break // No more waiters blocked.
		}

		w := next.Value.(chan struct{})
		if s.size-s.cur < 1 {
			// Not enough tokens for the next waiter.  We could keep going (to try to
			// find a waiter with a smaller request), but under load that could cause
			// starvation for large requests; instead, we leave all remaining waiters
			// blocked.
			//
			// Consider a semaphore used as a read-write lock, with N tokens, N
			// readers, and one writer.  Each reader can Acquire(1) to obtain a read
			// lock.  The writer can Acquire(N) to obtain a write lock, excluding all
			// of the readers.  If we allow the readers to jump ahead in the queue,
			// the writer will starve — there is always one token available for every
			// reader.
			break
		}

		s.cur++
		s.waiters.Remove(next)
		close(w)
	}
}
