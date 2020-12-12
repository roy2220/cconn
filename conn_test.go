package cconn_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/roy2220/cconn"
	"github.com/stretchr/testify/assert"
)

func TestConn_SetWatcherIdleTimeout(t *testing.T) {
	type Input struct {
		WatcherIdleTimeout time.Duration
	}
	type State = cconn.ConnDetails
	type TestCase struct {
		Given, When, Then string
		Setup, Teardown   func(*TestCase)
		Input             Input
		State             State

		t *testing.T
		c *cconn.Conn
	}
	testCases := []TestCase{
		{
			When: "WatcherIdleTimeout is negative and watcher goroutine (WG) is idle",
			Then: "WG should not exit",
			Input: Input{
				WatcherIdleTimeout: -1,
			},
			State: State{
				WatcherIdleTimeout: -1,
				ReadContext: cconn.IOContextDetails{
					Version: 2,
				},
				LastReadErrStr:   context.Canceled.Error(),
				WatcherIsRunning: true,
			},
			Teardown: func(tc *TestCase) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				tc.c.SetReadContext(ctx)
				time.Sleep(10 * time.Millisecond)
			},
		},
		{
			When: "WatcherIdleTimeout is zero and watcher goroutine (WG) is idle",
			Then: "WG should exit",
			State: State{
				ReadContext: cconn.IOContextDetails{
					Version: 2,
				},
				LastReadErrStr: context.Canceled.Error(),
			},
			Teardown: func(tc *TestCase) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				tc.c.SetReadContext(ctx)
				time.Sleep(10 * time.Millisecond)
			},
		},
		{
			When: "WatcherIdleTimeout is positive and watcher goroutine (WG) is idle and idle deadline has been reached",
			Then: "WG should exit",
			Input: Input{
				WatcherIdleTimeout: 10 * time.Millisecond,
			},
			State: State{
				WatcherIdleTimeout: 10 * time.Millisecond,
				ReadContext: cconn.IOContextDetails{
					Version: 2,
				},
				LastReadErrStr: context.Canceled.Error(),
			},
			Teardown: func(tc *TestCase) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				tc.c.SetReadContext(ctx)
				time.Sleep(20 * time.Millisecond)
			},
		},
		{
			When: "WatcherIdleTimeout is positive and watcher goroutine (WG) is idle and idle deadline has not been reached",
			Then: "WG should not exit",
			Input: Input{
				WatcherIdleTimeout: 20 * time.Millisecond,
			},
			State: State{
				WatcherIdleTimeout: 20 * time.Millisecond,
				ReadContext: cconn.IOContextDetails{
					Version: 2,
				},
				LastReadErrStr:   context.Canceled.Error(),
				WatcherIsRunning: true,
			},
			Teardown: func(tc *TestCase) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				tc.c.SetReadContext(ctx)
				time.Sleep(10 * time.Millisecond)
			},
		},
	}
	for i := range testCases {
		tc := &testCases[i]
		t.Run(fmt.Sprintf("#%d", i+1), func(t *testing.T) {
			t.Logf("\nGIVEN %s\nWHEN %s\nTHEN %s", tc.Given, tc.When, tc.Then)
			t.Parallel()

			cc1, cc2 := net.Pipe()
			defer cc2.Close()
			c := new(cconn.Conn).Init(cc1)
			defer c.Close()

			tc.t = t
			tc.c = c
			if f := tc.Setup; f != nil {
				f(tc)
			}

			c.SetWatcherIdleTimeout(tc.Input.WatcherIdleTimeout)

			if f := tc.Teardown; f != nil {
				f(tc)
			}

			state := c.Inspect()
			assert.Equal(t, tc.State, state)
		})
	}
}

func TestConn_SetReadContext(t *testing.T) {
	testConn_SetIOContext(t, true)
}

func TestConn_SetWriteContext(t *testing.T) {
	testConn_SetIOContext(t, false)
}

func testConn_SetIOContext(t *testing.T, read bool) {
	type Input struct {
		Ctx context.Context
	}
	type Output struct {
		ErrStr string
	}
	type State = cconn.ConnDetails
	type TestCase struct {
		Given, When, Then string
		Setup, Teardown   func(*TestCase)
		Input             Input
		Output            Output
		State             State

		t    *testing.T
		c    *cconn.Conn
		peer net.Conn
	}
	testCases := []TestCase{
		{
			When: "io context has been cancelled",
			Then: "io should fail",
			Setup: func(tc *TestCase) {
				if read {
					tc.State.ReadContext.Version = 2
					tc.State.LastReadErrStr = context.Canceled.Error()
				} else {
					tc.State.WriteContext.Version = 2
					tc.State.LastWriteErrStr = context.Canceled.Error()
				}

				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				tc.Input.Ctx = ctx
			},
			Teardown: func(tc *TestCase) {
				for i := 0; i < 2; i++ {
					var b [4096]byte
					var err error
					if read {
						_, err = tc.c.Read(b[:])
					} else {
						_, err = tc.c.Write(b[:])
					}
					assert.EqualError(t, err, context.Canceled.Error())
					if i == 0 {
						time.Sleep(10 * time.Millisecond)
					}
				}
			},
			State: State{
				WatcherIdleTimeout: cconn.DefaultWatcherIdleTimeout,
				WatcherIsRunning:   true,
			},
		},
		{
			When: "io context has timed out",
			Then: "io should fail",
			Setup: func(tc *TestCase) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				_ = cancel
				tc.Input.Ctx = ctx
				if read {
					tc.State.ReadContext.Version = 2
					tc.State.LastReadErrStr = context.DeadlineExceeded.Error()
				} else {
					tc.State.WriteContext.Version = 2
					tc.State.LastWriteErrStr = context.DeadlineExceeded.Error()
				}
			},
			Teardown: func(tc *TestCase) {
				for i := 0; i < 2; i++ {
					var b [4096]byte
					var err error
					if read {
						_, err = tc.c.Read(b[:])
					} else {
						_, err = tc.c.Write(b[:])
					}
					assert.EqualError(t, err, context.DeadlineExceeded.Error())
					if i == 0 {
						time.Sleep(10 * time.Millisecond)
					}
				}
			},
			State: State{
				WatcherIdleTimeout: cconn.DefaultWatcherIdleTimeout,
				WatcherIsRunning:   true,
			},
		},
		{
			When: "io context has been updated",
			Then: "io should respect latest io context",
			Setup: func(tc *TestCase) {
				time.AfterFunc(5*time.Millisecond, func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
					_ = cancel
					var err error
					if read {
						err = tc.c.SetReadContext(ctx)
						tc.State.ReadContext.Version = 2
						tc.State.LastReadErrStr = context.DeadlineExceeded.Error()
					} else {
						err = tc.c.SetWriteContext(ctx)
						tc.State.WriteContext.Version = 2
						tc.State.LastWriteErrStr = context.DeadlineExceeded.Error()
					}
					assert.NoError(t, err)
				})

			},
			Teardown: func(tc *TestCase) {
				for i := 0; i < 2; i++ {
					var b [4096]byte
					var err error
					if read {
						_, err = tc.c.Read(b[:])
					} else {
						_, err = tc.c.Write(b[:])
					}
					assert.EqualError(t, err, context.DeadlineExceeded.Error())
					if i == 0 {
						time.Sleep(10 * time.Millisecond)
					}
				}
			},
			Input: Input{
				Ctx: context.Background(),
			},
			State: State{
				WatcherIdleTimeout: cconn.DefaultWatcherIdleTimeout,
				WatcherIsRunning:   true,
			},
		},
		{
			Given: "cancelled io context",
			When:  "io context has been updated",
			Then:  "io should respect latest io context",
			Setup: func(tc *TestCase) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				var err error
				if read {
					err = tc.c.SetReadContext(ctx)
					tc.State.LastReadErrStr = context.Canceled.Error()
				} else {
					err = tc.c.SetWriteContext(ctx)
					tc.State.LastWriteErrStr = context.Canceled.Error()
				}
				if !assert.NoError(t, err) {
					t.FailNow()
				}
				time.Sleep(10 * time.Millisecond)

				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
				_ = cancel
				if read {
					err = tc.c.SetReadContext(ctx)
					tc.State.ReadContext.Version = 4
				} else {
					err = tc.c.SetWriteContext(ctx)
					tc.State.WriteContext.Version = 4
				}
				if !assert.NoError(t, err) {
					t.FailNow()
				}

				go func() {
					if read {
						n, err := tc.peer.Write([]byte("12345"))
						if assert.NoError(t, err) {
							assert.Equal(t, 5, n)
						}
					} else {
						b := make([]byte, 5)
						n, err := tc.peer.Read(b)
						if assert.NoError(t, err) {
							assert.Equal(t, []byte("12345"), b[:n])
						}
					}
				}()
			},
			Teardown: func(tc *TestCase) {
				if read {
					b := make([]byte, 5)
					n, err := tc.c.Read(b)
					if assert.NoError(t, err) {
						assert.Equal(t, []byte("12345"), b[:n])
					}
				} else {
					n, err := tc.c.Write([]byte("12345"))
					if assert.NoError(t, err) {
						assert.Equal(t, 5, n)
					}
				}
				time.Sleep(10 * time.Millisecond)
			},
			Input: Input{
				Ctx: context.Background(),
			},
			State: State{
				WatcherIdleTimeout: cconn.DefaultWatcherIdleTimeout,
				WatcherIsRunning:   true,
			},
		},
		{
			When: "io context has not been updated",
			Then: "should not create watcher goroutine",
			Input: Input{
				Ctx: context.Background(),
			},
			State: State{
				WatcherIdleTimeout: cconn.DefaultWatcherIdleTimeout,
			},
		},
		{
			Given: "closed Conn",
			Then:  "should fail",
			Setup: func(tc *TestCase) {
				tc.c.Close()
			},
			Input: Input{
				Ctx: context.Background(),
			},
			Output: Output{
				ErrStr: "io: read/write on closed pipe",
			},
			State: State{
				WatcherIdleTimeout: cconn.DefaultWatcherIdleTimeout,
			},
		},
	}
	for i := range testCases {
		tc := &testCases[i]
		t.Run(fmt.Sprintf("#%d", i+1), func(t *testing.T) {
			t.Logf("\nGIVEN %s\nWHEN %s\nTHEN %s", tc.Given, tc.When, tc.Then)
			t.Parallel()

			cc1, cc2 := net.Pipe()
			defer cc2.Close()
			c := new(cconn.Conn).Init(cc1)
			defer c.Close()

			tc.t = t
			tc.c = c
			tc.peer = cc2
			if f := tc.Setup; f != nil {
				f(tc)
			}

			var err error
			if read {
				err = c.SetReadContext(tc.Input.Ctx)
			} else {
				err = c.SetWriteContext(tc.Input.Ctx)
			}

			output := Output{}
			if err != nil {
				output.ErrStr = err.Error()
			}
			assert.Equal(t, tc.Output, output)

			if f := tc.Teardown; f != nil {
				f(tc)
			}

			state := c.Inspect()
			assert.Equal(t, tc.State, state)
		})
	}
}

func TestConn_LocalAddr(t *testing.T) {
	cc1, cc2 := net.Pipe()
	defer cc2.Close()
	c := new(cconn.Conn).Init(cc1)
	defer c.Close()
	addr := c.LocalAddr()
	assert.Equal(t, "pipe", addr.String())
}

func TestConn_RemoteAddr(t *testing.T) {
	cc1, cc2 := net.Pipe()
	defer cc2.Close()
	c := new(cconn.Conn).Init(cc1)
	defer c.Close()
	addr := c.RemoteAddr()
	assert.Equal(t, "pipe", addr.String())
}
