package jsvm

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja_nodejs/buffer"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
	gojaUrl "github.com/dop251/goja_nodejs/url"
)

type job struct {
	cancelled bool
	fn        func()
}

type Timer struct {
	job
	timer *time.Timer
}

type Interval struct {
	job
	ticker   *time.Ticker
	stopChan chan struct{}
}

type Immediate struct {
	job
}

type EventLoop struct {
	vm       *goja.Runtime
	jobChan  chan func()
	jobCount int32
	canRun   int32

	immediates []*Immediate
	intervals  []*Interval
	timers     []*Timer

	auxJobsLock sync.Mutex
	wakeupChan  chan struct{}

	auxJobsSpare, auxJobs []func()

	stopLock sync.Mutex
	stopCond *sync.Cond

	runningChan chan bool
	running     bool

	enableConsole bool
	registry      *require.Registry
}

func NewEventLoop(opts ...Option) *EventLoop {
	vm := goja.New()
	return NewEventLoopWithVM(vm, opts...)
}
func NewEventLoopWithVM(vm *goja.Runtime, opts ...Option) *EventLoop {

	loop := &EventLoop{
		vm:            vm,
		jobChan:       make(chan func()),
		wakeupChan:    make(chan struct{}, 1),
		runningChan:   make(chan bool),
		enableConsole: true,
	}
	loop.stopCond = sync.NewCond(&loop.stopLock)

	for _, opt := range opts {
		opt(loop)
	}
	vm.SetParserOptions(parser.WithDisableSourceMaps)
	loop.registry = new(require.Registry)
	loop.registry.Enable(vm)
	console.Enable(vm)
	buffer.Enable(vm)
	gojaUrl.Enable(vm)
	ProcessEnable(vm, loop)
	vm.Set("setTimeout", loop.setTimeout)
	vm.Set("setInterval", loop.setInterval)
	vm.Set("setImmediate", loop.setImmediate)
	vm.Set("clearTimeout", loop.clearTimeout)
	vm.Set("clearInterval", loop.clearInterval)
	vm.Set("clearImmediate", loop.clearImmediate)

	vm.Set("runLoop", func(call goja.FunctionCall) goja.Value {
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			_loop := NewEventLoopWithVM(loop.vm)
			var args []goja.Value = []goja.Value{
				loop.vm.ToValue(func() {
					go _loop.StopNoWait()
				}),
			}
			if len(call.Arguments) > 1 {
				args = append(args, call.Arguments[1:]...)
			}
			_loop.RunOnLoop(func(r *goja.Runtime) {
				if _, err := fn(nil, args...); err != nil {
					panic(err)
				}
			})
			_loop.Start()
			return loop.vm.ToValue(_loop)
		}
		return nil
	})

	return loop
}

type Option func(*EventLoop)

// EnableConsole controls whether the "console" module is loaded into
// the runtime used by the loop.  By default, loops are created with
// the "console" module loaded, pass EnableConsole(false) to
// NewEventLoop to disable this behavior.
func EnableConsole(enableConsole bool) Option {
	return func(loop *EventLoop) {
		loop.enableConsole = enableConsole
	}
}

func WithRegistry(registry *require.Registry) Option {
	return func(loop *EventLoop) {
		loop.registry = registry
	}
}

func (loop *EventLoop) schedule(call goja.FunctionCall, repeating bool) goja.Value {
	if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
		delay := call.Argument(1).ToInteger()
		var args []goja.Value
		if len(call.Arguments) > 2 {
			args = append(args, call.Arguments[2:]...)
		}
		var t *Timer
		f := func() {
			if _, err := fn(nil, args...); err != nil {
				panic(err)
			}
			if !repeating {
				loop.clearTimeout(t)
			}
		}
		loop.jobCount++
		if repeating {
			i := loop.addInterval(f, time.Duration(delay)*time.Millisecond)
			loop.intervals = append(loop.intervals, i)
			return loop.vm.ToValue(i)
		} else {
			t = loop.addTimeout(f, time.Duration(delay)*time.Millisecond)
			loop.timers = append(loop.timers, t)
			return loop.vm.ToValue(t)
		}
	}
	return nil
}

func (loop *EventLoop) setTimeout(call goja.FunctionCall) goja.Value {
	return loop.schedule(call, false)
}

func (loop *EventLoop) setInterval(call goja.FunctionCall) goja.Value {
	return loop.schedule(call, true)
}

func (loop *EventLoop) setImmediate(call goja.FunctionCall) goja.Value {
	if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
		var args []goja.Value
		if len(call.Arguments) > 1 {
			args = append(args, call.Arguments[1:]...)
		}
		var i *Immediate
		f := func() {
			if _, err := fn(nil, args...); err != nil {
				panic(err)
			}
			loop.clearImmediate(i)
		}
		loop.jobCount++
		i = loop.addImmediate(f)
		loop.immediates = append(loop.immediates, i)
		return loop.vm.ToValue(i)
	}
	return nil
}

// SetTimeout schedules to run the specified function in the context
// of the loop as soon as possible after the specified timeout period.
// SetTimeout returns a Timer which can be passed to ClearTimeout.
// The instance of goja.Runtime that is passed to the function and any Values derived
// from it must not be used outside the function. SetTimeout is
// safe to call inside or outside the loop.
func (loop *EventLoop) SetTimeout(fn func(*goja.Runtime), timeout time.Duration) *Timer {
	t := loop.addTimeout(func() { fn(loop.vm) }, timeout)
	loop.addAuxJob(func() {
		loop.jobCount++
	})
	loop.timers = append(loop.timers, t)
	return t
}

// ClearTimeout cancels a Timer returned by SetTimeout if it has not run yet.
// ClearTimeout is safe to call inside or outside the loop.
func (loop *EventLoop) ClearTimeout(t *Timer) {
	loop.addAuxJob(func() {
		loop.clearTimeout(t)
	})
}

// SetInterval schedules to repeatedly run the specified function in
// the context of the loop as soon as possible after every specified
// timeout period.  SetInterval returns an Interval which can be
// passed to ClearInterval. The instance of goja.Runtime that is passed to the
// function and any Values derived from it must not be used outside
// the function. SetInterval is safe to call inside or outside the
// loop.
func (loop *EventLoop) SetInterval(fn func(*goja.Runtime), timeout time.Duration) *Interval {
	i := loop.addInterval(func() { fn(loop.vm) }, timeout)
	loop.addAuxJob(func() {
		loop.jobCount++
	})
	loop.intervals = append(loop.intervals, i)
	return i
}

// ClearInterval cancels an Interval returned by SetInterval.
// ClearInterval is safe to call inside or outside the loop.
func (loop *EventLoop) ClearInterval(i *Interval) {
	loop.addAuxJob(func() {
		loop.clearInterval(i)
	})
}

func (loop *EventLoop) setRunning() {
	loop.stopLock.Lock()
	defer loop.stopLock.Unlock()
	if loop.running {
		panic("Loop is already started")
	}
	loop._running(true)
	// loop.runningChan <- loop.running
	atomic.StoreInt32(&loop.canRun, 1)
}

// Run calls the specified function, starts the event loop and waits until there are no more delayed jobs to run
// after which it stops the loop and returns.
// The instance of goja.Runtime that is passed to the function and any Values derived from it must not be used
// outside the function.
// Do NOT use this function while the loop is already running. Use RunOnLoop() instead.
// If the loop is already started it will panic.
func (loop *EventLoop) Run(fn func(*goja.Runtime)) {
	loop.setRunning()
	fn(loop.vm)
	loop.run(false)
}

// Start the event loop in the background. The loop continues to run until Stop() is called.
// If the loop is already started it will panic.
func (loop *EventLoop) Start() {
	loop.setRunning()
	go loop.run(true)
}

// StartInForeground starts the event loop in the current goroutine. The loop continues to run until Stop() is called.
// If the loop is already started it will panic.
// Use this instead of Start if you want to recover from panics that may occur while calling native Go functions from
// within setInterval and setTimeout callbacks.
func (loop *EventLoop) StartInForeground() {
	loop.setRunning()
	go loop.run(true)
	println("loop.runningChan <<")
	<-loop.runningChan
	println("loop.runningChan >>")
}

// Stop the loop that was started with Start(). After this function returns there will be no more jobs executed
// by the loop. It is possible to call Start() or Run() again after this to resume the execution.
// Note, it does not cancel active timeouts.
// It is not allowed to run Start() (or Run()) and Stop() concurrently.
// Calling Stop() on a non-running loop has no effect.
// It is not allowed to call Stop() from the loop, because it is synchronous and cannot complete until the loop
// is not running any jobs. Use StopNoWait() instead.
// return number of jobs remaining
func (loop *EventLoop) Stop() int {
	loop.stopLock.Lock()
	loop.stopTimers()
	for loop.running {
		atomic.StoreInt32(&loop.canRun, 0)
		loop.wakeup()
		loop.stopCond.Wait()
	}
	loop.stopLock.Unlock()
	return int(loop.jobCount)
}

// StopNoWait tells the loop to stop and returns immediately. Can be used inside the loop. Calling it on a
// non-running loop has no effect.
func (loop *EventLoop) StopNoWait() {
	loop.stopLock.Lock()
	loop.stopTimers()
	if loop.running {
		atomic.StoreInt32(&loop.canRun, 0)
		loop.wakeup()
	}
	loop.stopLock.Unlock()
}

func (loop *EventLoop) stopTimers() {
	for _, i := range loop.immediates {
		loop.clearImmediate(i)
	}
	for _, i := range loop.intervals {
		loop.clearInterval(i)
	}
	for _, t := range loop.timers {
		loop.clearTimeout(t)
	}
}

// RunOnLoop schedules to run the specified function in the context of the loop as soon as possible.
// The order of the runs is preserved (i.e. the functions will be called in the same order as calls to RunOnLoop())
// The instance of goja.Runtime that is passed to the function and any Values derived from it must not be used
// outside the function. It is safe to call inside or outside the loop.
func (loop *EventLoop) RunOnLoop(fn func(*goja.Runtime)) {
	if loop.running {
		loop.addAuxJob(func() { fn(loop.vm) })
	} else {
		loop.Run(fn)
	}
}

func (loop *EventLoop) runAux() {
	loop.auxJobsLock.Lock()
	jobs := loop.auxJobs
	loop.auxJobs = loop.auxJobsSpare
	loop.auxJobsLock.Unlock()
	for i, job := range jobs {
		job()
		jobs[i] = nil
	}
	loop.auxJobsSpare = jobs[:0]
}

func (loop *EventLoop) run(inBackground bool) {
	loop.runAux()
	if inBackground {
		loop.jobCount++
	}
LOOP:
	for loop.jobCount > 0 {
		select {
		case job := <-loop.jobChan:
			job()
		case <-loop.wakeupChan:
			loop.runAux()
			if atomic.LoadInt32(&loop.canRun) == 0 {
				break LOOP
			}
		}
	}
	if inBackground {
		loop.jobCount--
	}

	loop.stopLock.Lock()
	loop._running(false)
	loop.stopLock.Unlock()
	loop.stopCond.Broadcast()
}

func (loop *EventLoop) wakeup() {
	select {
	case loop.wakeupChan <- struct{}{}:
	default:
	}
}

func (loop *EventLoop) _running(r bool) {
	loop.running = r
	select {
	case loop.runningChan <- r:
	default:
	}
}

func (loop *EventLoop) addAuxJob(fn func()) {
	loop.auxJobsLock.Lock()
	loop.auxJobs = append(loop.auxJobs, fn)
	loop.auxJobsLock.Unlock()
	loop.wakeup()
}

func (loop *EventLoop) addTimeout(f func(), timeout time.Duration) *Timer {
	t := &Timer{
		job: job{fn: f},
	}
	t.timer = time.AfterFunc(timeout, func() {
		loop.jobChan <- func() {
			loop.doTimeout(t)
		}
	})

	return t
}

func (loop *EventLoop) addInterval(f func(), timeout time.Duration) *Interval {
	// https://nodejs.org/api/timers.html#timers_setinterval_callback_delay_args
	if timeout <= 0 {
		timeout = time.Millisecond
	}

	i := &Interval{
		job:      job{fn: f},
		ticker:   time.NewTicker(timeout),
		stopChan: make(chan struct{}),
	}

	go i.run(loop)
	return i
}

func (loop *EventLoop) addImmediate(f func()) *Immediate {
	i := &Immediate{
		job: job{fn: f},
	}
	loop.addAuxJob(func() {
		loop.doImmediate(i)
	})
	return i
}

func (loop *EventLoop) doTimeout(t *Timer) {
	if !t.cancelled {
		t.cancelled = true
		loop.jobCount--
		t.fn()
	}
}

func (loop *EventLoop) doInterval(i *Interval) {
	if !i.cancelled {
		i.fn()
	}
}

func (loop *EventLoop) doImmediate(i *Immediate) {
	if !i.cancelled {
		i.cancelled = true
		loop.jobCount--
		i.fn()
	}
}

func (loop *EventLoop) clearTimeout(t *Timer) {
	if t != nil && !t.cancelled {
		t.timer.Stop()
		t.cancelled = true
		loop.jobCount--
		loop.timers = findAndDelete(loop.timers, t)
	}
}

func (loop *EventLoop) clearInterval(i *Interval) {
	if i != nil && !i.cancelled {
		i.cancelled = true
		close(i.stopChan)
		loop.jobCount--
		loop.intervals = findAndDelete(loop.intervals, i)

	}
}

func (loop *EventLoop) clearImmediate(i *Immediate) {
	if i != nil && !i.cancelled {
		i.cancelled = true
		loop.jobCount--
		loop.immediates = findAndDelete(loop.immediates, i)
	}
}

func (i *Interval) run(loop *EventLoop) {
L:
	for {
		select {
		case <-i.stopChan:
			i.ticker.Stop()
			break L
		case <-i.ticker.C:
			loop.jobChan <- func() {
				loop.doInterval(i)
			}
		}
	}
}

func findAndDelete[T Timer | Interval | Immediate](s []*T, item *T) []*T {
	index := 0
	for _, i := range s {
		if i != item {
			s[index] = i
			index++
		}
	}
	return s[:index]
}
