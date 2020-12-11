// Package cconn provides a wrapper for net.Conn which implements SetReadContext
// and SetWriteContext as an enhancement to SetReadDeadline and SetWriteDeadline.
//
// The basic idea of the implementation is to appoint a separate goroutine to watch
// the done events of both the Read context and the Write context. Once the Read/Write
// context is done, the watcher goroutine cache the error from the context and then
// interrupt any currently-blocked Read/Write call by SetReadContext(past)/SetWriteDeadline(past).
// For the Read/Write caller, once the call returns a timeout error, it retrieves the
// last-cached error by the watcher goroutine and returns that error instead.
//
// The watcher goroutine is created lazily and the WatcherIdleTimeout option determines
// it's lifetime:
//
// • If WatcherIdleTimeout has a negative value, the watcher goroutine never exits
// until the connection has been closed;
//
// • If WatcherIdleTimeout has a zero value, the watcher goroutine exits when both
// the Read context and Write context are set to context.Background(), or the connection
// has been closed;
//
// • If WatcherIdleTimeout has a positive value e.g. Ts, the watcher goroutine
// exits when both the Read context and Write context are set to context.Background()
// and none of them has been updated in the following Ts, or the connection has been
// closed.
//
// The default value for WatcherIdleTimeout is 5s.
package cconn

import (
	"context"
	"net"
	"sync"
	"time"
)

// DefaultWatcherIdleTimeout is the default value for the WatcherIdleTimeout option.
const DefaultWatcherIdleTimeout = 5 * time.Second

// Conn represents a wrapper for net.Conn.
type Conn struct {
	c net.Conn

	mu                 sync.Mutex
	updates            chan struct{}
	watcherIdleTimeout time.Duration
	readContext        ioContext
	writeContext       ioContext
	lastReadErr        error
	lastWriteErr       error
	watcherIsRunning   bool
}

// Init initializes the wrapper with the given net.Conn and returns it.
// Use new(Conn).Init() to create and initialize a Conn instance at once.
func (c *Conn) Init(cc net.Conn) *Conn {
	c.c = cc
	c.updates = make(chan struct{}, 1)
	c.watcherIdleTimeout = DefaultWatcherIdleTimeout
	return c
}

// SetWatcherIdleTimeout sets the WatcherIdleTimeout option which determines
// the lifetime of the watcher goroutine.
func (c *Conn) SetWatcherIdleTimeout(watcherIdleTimeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.watcherIdleTimeout = watcherIdleTimeout
}

// SetReadContext sets the context for currently-blocked and future Read calls.
// The watcher goroutine will be created when necessary.
func (c *Conn) SetReadContext(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.c.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	done := ctx.Done()
	if done == nil {
		if c.readContext.Done == nil {
			return nil
		}
		ctx = nil
	}
	c.readContext.Ctx = ctx
	c.readContext.Done = done
	c.readContext.Version++
	if c.watcherIsRunning {
		c.sendUpdate()
	} else {
		watcher := watcher{Conn: c}
		go watcher.Run()
		c.watcherIsRunning = true
	}
	return nil
}

// SetWriteContext sets the context for currently-blocked and future Write calls.
// The watcher goroutine will be created when necessary.
func (c *Conn) SetWriteContext(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.c.SetWriteDeadline(time.Time{}); err != nil {
		return err
	}
	done := ctx.Done()
	if done == nil {
		if c.writeContext.Done == nil {
			return nil
		}
		ctx = nil
	}
	c.writeContext.Ctx = ctx
	c.writeContext.Done = done
	c.writeContext.Version++
	if c.watcherIsRunning {
		c.sendUpdate()
	} else {
		watcher := watcher{Conn: c}
		go watcher.Run()
		c.watcherIsRunning = true
	}
	return nil
}

// Read reads the underlying net.Conn with respect to the Read context.
func (c *Conn) Read(b []byte) (int, error) {
	n, err := c.c.Read(b)
	if err2, ok := err.(net.Error); ok && err2.Timeout() {
		c.mu.Lock()
		defer c.mu.Unlock()
		err = c.lastReadErr
	}
	return n, err
}

// Write writes the underlying net.Conn with respect to the Write context.
func (c *Conn) Write(b []byte) (int, error) {
	n, err := c.c.Write(b)
	if err2, ok := err.(net.Error); ok && err2.Timeout() {
		c.mu.Lock()
		defer c.mu.Unlock()
		err = c.lastWriteErr
	}
	return n, err
}

// Write closes the underlying net.Conn and make the watcher goroutine exit.
func (c *Conn) Close() error {
	err := c.c.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.watcherIsRunning = false
	c.sendUpdate()
	return err
}

// ConnDetails represents the detailed information of Conn.
type ConnDetails struct {
	WatcherIdleTimeout time.Duration
	ReadContext        IOContextDetails
	WriteContext       IOContextDetails
	LastReadErrStr     string
	LastWriteErrStr    string
	WatcherIsRunning   bool
}

// IOContextDetails represents the detailed information of the Read/Write context.
type IOContextDetails struct {
	CtxIsNotNil  bool
	DoneIsNotNil bool
	Version      int64
}

// Inspect returns the detailed information of Conn for testing and debugging
// purposes.
func (c *Conn) Inspect() ConnDetails {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lastReadErrStr string
	if c.lastReadErr != nil {
		lastReadErrStr = c.lastReadErr.Error()
	}
	var lastWriteErrStr string
	if c.lastWriteErr != nil {
		lastWriteErrStr = c.lastWriteErr.Error()
	}
	return ConnDetails{
		WatcherIdleTimeout: c.watcherIdleTimeout,
		ReadContext: IOContextDetails{
			CtxIsNotNil:  c.readContext.Ctx != nil,
			DoneIsNotNil: c.readContext.Done != nil,
			Version:      c.readContext.Version,
		},
		WriteContext: IOContextDetails{
			CtxIsNotNil:  c.writeContext.Ctx != nil,
			DoneIsNotNil: c.writeContext.Done != nil,
			Version:      c.writeContext.Version,
		},
		LastReadErrStr:   lastReadErrStr,
		LastWriteErrStr:  lastWriteErrStr,
		WatcherIsRunning: c.watcherIsRunning,
	}
}

// LocalAddr returns the local address of underlying net.Conn.
func (c *Conn) LocalAddr() net.Addr { return c.c.LocalAddr() }

// RemoteAddr returns the remote address of underlying net.Conn.
func (c *Conn) RemoteAddr() net.Addr { return c.c.RemoteAddr() }

func (c *Conn) sendUpdate() {
	select {
	case c.updates <- struct{}{}:
	default:
	}
}

type ioContext struct {
	Ctx     context.Context
	Done    <-chan struct{}
	Version int64
}

type watcher struct {
	Conn *Conn

	readContext  ioContext
	writeContext ioContext
	idleTimeout  time.Duration
	idleTimer    *time.Timer
}

func (w *watcher) Run() {
	conn := w.Conn
	mu := &conn.mu
	mu.Lock()
	defer func() {
		if mu != nil {
			mu.Unlock()
		}
	}()
	if !conn.watcherIsRunning {
		return
	}
	for {
		w.readContext = conn.readContext
		w.writeContext = conn.writeContext
		w.idleTimeout = conn.watcherIdleTimeout
		select {
		case <-conn.updates:
		default:
		}
		mu.Unlock()
		mu = nil
		action, ok := w.getAction()
		mu = &conn.mu
		mu.Lock()
		if !conn.watcherIsRunning {
			return
		}
		if ok {
			action()
		} else {
			if conn.readContext.Version == w.readContext.Version &&
				conn.writeContext.Version == w.writeContext.Version {
				conn.watcherIsRunning = false
				return
			}
		}
	}
}

func (w *watcher) getAction() (func(), bool) {
	conn := w.Conn
	var idleDeadline <-chan time.Time
	if w.readContext.Done == nil && w.writeContext.Done == nil {
		if w.idleTimeout == 0 {
			return nil, false
		}
		if w.idleTimeout >= 1 {
			if w.idleTimer == nil {
				w.idleTimer = time.NewTimer(w.idleTimeout)
			} else {
				w.idleTimer.Reset(w.idleTimeout)
			}
			idleDeadline = w.idleTimer.C
			defer func() {
				if idleDeadline != nil {
					if !w.idleTimer.Stop() {
						<-idleDeadline
					}
				}
			}()
		}
	}
	select {
	case <-w.readContext.Done:
		return func() {
			if conn.readContext.Version != w.readContext.Version {
				return
			}
			conn.readContext = ioContext{Version: w.readContext.Version + 1}
			conn.lastReadErr = w.readContext.Ctx.Err()
			_ = conn.c.SetReadDeadline(past)
		}, true
	case <-w.writeContext.Done:
		return func() {
			if conn.writeContext.Version != w.writeContext.Version {
				return
			}
			conn.writeContext = ioContext{Version: w.writeContext.Version + 1}
			conn.lastWriteErr = w.writeContext.Ctx.Err()
			_ = conn.c.SetWriteDeadline(past)
		}, true
	case <-conn.updates:
		return func() {}, true
	case <-idleDeadline:
		idleDeadline = nil
		return nil, false
	}
}

var past = time.Now()
