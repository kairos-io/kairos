package ws

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// wsConn wraps a *websocket.Conn with a per-connection write mutex.
//
// gorilla/websocket permits at most one concurrent writer per connection: it is
// the caller's responsibility to serialize writes. In this server several
// goroutines write to the same connection — the hub's SendCommand/Broadcast, the
// agent read-loop's response writes, and the UI ping ticker — so every write
// must go through writeMessage/writeControl to take this lock. The hub's map
// mutex (Hub.mu / UIHub.mu) guards map access only and is intentionally separate
// from this per-connection write lock.
type wsConn struct {
	*websocket.Conn
	writeMu sync.Mutex
}

// writeWait bounds one data-frame write. A peer that stops reading fills the
// socket buffers, and without a deadline the write blocks forever while holding
// writeMu. Stored in nanoseconds and atomic rather than a const so tests can
// shorten it while other connections are writing.
var writeWait atomic.Int64

func init() { writeWait.Store(int64(10 * time.Second)) }

// newConn wraps a raw gorilla connection so all writes are serialized.
func newConn(c *websocket.Conn) *wsConn {
	return &wsConn{Conn: c}
}

// writeMessage serializes a data-frame write to the connection. A write that
// fails or misses writeWait closes the connection: gorilla cannot recover from
// a partly written frame, and closing it makes the handler's read loop exit and
// unregister the peer, while later writes fail at once.
func (c *wsConn) writeMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.Conn.SetWriteDeadline(time.Now().Add(time.Duration(writeWait.Load()))); err != nil {
		_ = c.Conn.Close()
		return err
	}
	if err := c.Conn.WriteMessage(messageType, data); err != nil {
		_ = c.Conn.Close()
		return err
	}
	return nil
}

// writeControl serializes a control-frame write (e.g. ping) to the connection
// and, like writeMessage, closes the connection when the write fails.
func (c *wsConn) writeControl(messageType int, data []byte, deadline time.Time) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.Conn.WriteControl(messageType, data, deadline); err != nil {
		_ = c.Conn.Close()
		return err
	}
	return nil
}
