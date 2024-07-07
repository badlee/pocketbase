package waitgroup

import (
	"sync"
	"sync/atomic"
)

type Waiter struct {
	waiter sync.WaitGroup
	count  int64
}

func (wg *Waiter) Inc() {
	atomic.AddInt64(&wg.count, 1)
	wg.waiter.Add(1)
}

func (wg *Waiter) Dec() {
	atomic.AddInt64(&wg.count, -1)
	wg.waiter.Done()
}

func (wg *Waiter) Count() int {
	return int(atomic.LoadInt64(&wg.count))
}
func (wg *Waiter) Wait() {
	wg.waiter.Wait()
}

func Create() *Waiter {
	return &Waiter{}
}
