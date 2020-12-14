package cconn_test

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	. "github.com/roy2220/cconn"
	"github.com/stretchr/testify/assert"
)

func TestConn_SetWatcherIdleTimeout(t *testing.T) {
	type Input struct {
		WatcherIdleTimeout time.Duration
	}
	type State = ConnDetails
	type TestCase struct {
		Given, When, Then string
		Setup, Teardown   func(*TestCase)
		Input             Input
		State             State

		t *testing.T
		c *Conn
	}
	testCases := []TestCase{
		{
			Given: "negative WatcherIdleTimeout",
			When:  "watcher goroutine (WG) is idle",
			Then:  "WG should not exit",
			Input: Input{
				WatcherIdleTimeout: -1,
			},
			State: State{
				WatcherIdleTimeout: -1,
				ReadContext: IOContextDetails{
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
			Given: "zero WatcherIdleTimeout",
			When:  "watcher goroutine (WG) is idle",
			Then:  "WG should exit",
			State: State{
				ReadContext: IOContextDetails{
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
			Given: "positive WatcherIdleTimeout",
			When:  "watcher goroutine (WG) is idle and idle deadline has been reached",
			Then:  "WG should exit",
			Input: Input{
				WatcherIdleTimeout: 10 * time.Millisecond,
			},
			State: State{
				WatcherIdleTimeout: 10 * time.Millisecond,
				ReadContext: IOContextDetails{
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
			Given: "positive WatcherIdleTimeout",
			When:  "watcher goroutine (WG) is idle and idle deadline has not been reached",
			Then:  "WG should not exit",
			Input: Input{
				WatcherIdleTimeout: 20 * time.Millisecond,
			},
			State: State{
				WatcherIdleTimeout: 20 * time.Millisecond,
				ReadContext: IOContextDetails{
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
			c := new(Conn).Init(cc1)
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
	type State = ConnDetails
	type TestCase struct {
		Given, When, Then string
		Setup, Teardown   func(*TestCase)
		Input             Input
		Output            Output
		State             State

		t    *testing.T
		c    *Conn
		peer net.Conn
	}
	testCases := []TestCase{
		{
			When: "io context has been cancelled",
			Then: "io should fail",
			Setup: func(tc *TestCase) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				tc.Input.Ctx = ctx
				if read {
					tc.State.LastReadErrStr = context.Canceled.Error()
				} else {
					tc.State.LastWriteErrStr = context.Canceled.Error()
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
					assert.EqualError(t, err, context.Canceled.Error())
					if i == 0 {
						time.Sleep(10 * time.Millisecond)
						if read {
							tc.State.ReadContext.Version = 2
						} else {
							tc.State.WriteContext.Version = 2
						}
					}
				}
			},
			State: State{
				WatcherIdleTimeout: DefaultWatcherIdleTimeout,
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
					tc.State.LastReadErrStr = context.DeadlineExceeded.Error()
				} else {
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
						if read {
							tc.State.ReadContext.Version = 2
						} else {
							tc.State.WriteContext.Version = 2
						}
					}
				}
			},
			State: State{
				WatcherIdleTimeout: DefaultWatcherIdleTimeout,
				WatcherIsRunning:   true,
			},
		},
		{
			Then: "io should respect latest io context (1)",
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
				} else {
					err = tc.c.SetWriteContext(ctx)
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
				if read {
					tc.State.ReadContext.Version = 4
				} else {
					tc.State.WriteContext.Version = 4
				}
			},
			Input: Input{
				Ctx: context.Background(),
			},
			State: State{
				WatcherIdleTimeout: DefaultWatcherIdleTimeout,
				WatcherIsRunning:   true,
			},
		},
		{
			Then: "io should respect latest io context (2)",
			Setup: func(tc *TestCase) {
				if read {
					tc.State.LastReadErrStr = context.DeadlineExceeded.Error()
				} else {
					tc.State.LastWriteErrStr = context.DeadlineExceeded.Error()
				}
				time.AfterFunc(5*time.Millisecond, func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
					_ = cancel
					var err error
					if read {
						err = tc.c.SetReadContext(ctx)
					} else {
						err = tc.c.SetWriteContext(ctx)
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
						if read {
							tc.State.ReadContext.Version = 2
						} else {
							tc.State.WriteContext.Version = 2
						}
					}
				}
			},
			Input: Input{
				Ctx: context.Background(),
			},
			State: State{
				WatcherIdleTimeout: DefaultWatcherIdleTimeout,
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
				WatcherIdleTimeout: DefaultWatcherIdleTimeout,
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
				WatcherIdleTimeout: DefaultWatcherIdleTimeout,
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
			c := new(Conn).Init(cc1)
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
	c := new(Conn).Init(cc1)
	defer c.Close()
	addr := c.LocalAddr()
	assert.Equal(t, "pipe", addr.String())
}

func TestConn_RemoteAddr(t *testing.T) {
	cc1, cc2 := net.Pipe()
	defer cc2.Close()
	c := new(Conn).Init(cc1)
	defer c.Close()
	addr := c.RemoteAddr()
	assert.Equal(t, "pipe", addr.String())
}

func TestConnRace(t *testing.T) {
	cs := make([]*Conn, 10)
	for i := range cs {
		cc1, cc2 := net.Pipe()
		defer cc2.Close()
		c := new(Conn).Init(cc1)
		defer c.Close()
		cs[i] = c
	}
	const N = 100
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < N; i++ {
				c := cs[rand.Intn(len(cs))]
				switch rand.Intn(5) {
				case 0:
					to := time.Duration((rand.Intn(4)-1)*10) * time.Millisecond
					c.SetWatcherIdleTimeout(to)
				case 1, 2:
					to := time.Duration(rand.Intn(3)*10) * time.Millisecond
					ctx, cancel := context.WithTimeout(context.Background(), to)
					_ = cancel
					err := c.SetReadContext(ctx)
					assert.NoError(t, err)

					b := make([]byte, N)
					n, err := c.Read(b)
					if err == nil {
						assert.Equal(t, N, n)
					} else {
						assert.EqualError(t, err, context.DeadlineExceeded.Error())
					}
				case 3, 4:
					to := time.Duration(rand.Intn(3)*10) * time.Millisecond
					var cancel context.CancelFunc
					ctx, cancel := context.WithTimeout(context.Background(), to)
					_ = cancel
					err := c.SetWriteContext(ctx)
					assert.NoError(t, err)

					b := make([]byte, N)
					n, err := c.Write(b)
					if err == nil {
						assert.Equal(t, N, n)
					} else {
						assert.EqualError(t, err, context.DeadlineExceeded.Error())
					}
				}
			}
		}()
	}
	wg.Wait()
	for _, c := range cs {
		err := c.Close()
		if !assert.NoError(t, err) {
			t.FailNow()
		}
		time.Sleep(10 * time.Millisecond)
		d := c.Inspect()
		assert.False(t, d.WatcherIsRunning)
	}
}
