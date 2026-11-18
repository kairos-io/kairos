package ws

import "time"

// SetWriteWait shortens the data-frame write deadline for a test and returns a
// func that restores the previous value.
func SetWriteWait(d time.Duration) func() {
	prev := writeWait.Swap(int64(d))
	return func() { writeWait.Store(prev) }
}
