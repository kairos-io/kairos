package webui

import "sync"

// Message types on the progress websocket, as the "type" field.
const (
	// MessageStep is one agentrun step the agent reached. Step carries the
	// agentrun step id, not a rendered label.
	MessageStep = "step"
	// MessageLog is one line of agent output that was not a progress event.
	MessageLog = "log"
	// MessageError is a failure the agent reported, or the exit error.
	MessageError = "error"
	// MessageDone ends the stream. OK says whether the install succeeded.
	MessageDone = "done"
)

// Message is one frame on the progress websocket.
//
// The web UI used to stream the agent's terminal transcript, ANSI colours
// converted to HTML spans, and the browser decided an install had finished by
// looking for a "[COMPLETE]" substring in it. This carries the agent's own
// vocabulary instead: Step holds an sdk/agentrun step id, the same one the TUI
// turns into a checklist and the MCP server reports as a progress
// notification, so all three frontends describe an install the same way.
type Message struct {
	Type    string `json:"type"`
	Step    string `json:"step,omitempty"`
	Message string `json:"message,omitempty"`
	OK      bool   `json:"ok,omitempty"`
}

// maxHistory bounds the replay buffer. An install is a few thousand lines, so
// this is not reached in practice; it is here so a pathologically noisy agent
// cannot grow the installer's heap without bound. Once it is hit the oldest
// messages are dropped, and a client that connects late simply starts further
// in.
const maxHistory = 20000

// progressLog is the transcript of one install run, and the fan-out to every
// browser watching it.
//
// It keeps the whole run rather than only what is arriving now, because the
// browser reaches /ws by following a redirect to progress.html: the install is
// already running by the time the socket opens, and a reload must not show an
// empty screen. Readers take messages by index, so every client sees the same
// sequence and a slow one cannot lose messages or stall the install.
type progressLog struct {
	mu       sync.Mutex
	messages []Message
	dropped  int
	finished bool
	// done is closed once, when the run publishes its done message. The
	// installer's TUI waits on it so pressing `q` at the console cannot cut
	// a browser-driven install short.
	done chan struct{}
	// changed is closed and replaced on every publish, so readers can wait
	// for the next message without polling.
	changed chan struct{}
}

func newProgressLog() *progressLog {
	return &progressLog{changed: make(chan struct{}), done: make(chan struct{})}
}

// publish appends a message and wakes every reader.
func (p *progressLog) publish(m Message) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.messages = append(p.messages, m)
	if len(p.messages) > maxHistory {
		drop := len(p.messages) - maxHistory
		p.messages = append(p.messages[:0], p.messages[drop:]...)
		p.dropped += drop
	}
	// Guarded: a second done message would panic on the close, and
	// nothing here can promise a caller only publishes one.
	if m.Type == MessageDone && !p.finished {
		p.finished = true
		close(p.done)
	}

	close(p.changed)
	p.changed = make(chan struct{})
}

// since returns the messages from index i onwards and the index to ask for
// next. done is true when those messages are the last of a finished run, so a
// reader closes its socket having sent everything, rather than one wake-up
// later. changed is closed on the next publish, so a reader with nothing left
// to send can block on it.
//
// An index below the replay window is moved up to the oldest message still
// held, so a reader that started before maxHistory was reached resumes rather
// than reading off the front of the buffer.
func (p *progressLog) since(i int) (msgs []Message, next int, done bool, changed <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if i < p.dropped {
		i = p.dropped
	}
	total := p.dropped + len(p.messages)
	if i < total {
		msgs = append(msgs, p.messages[i-p.dropped:]...)
	}
	return msgs, total, p.finished && total == i+len(msgs), p.changed
}

// running reports whether the run is still in progress.
func (p *progressLog) running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.finished
}

// succeeded reports whether the run finished without an error.
func (p *progressLog) succeeded() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.finished {
		return false
	}
	for i := len(p.messages) - 1; i >= 0; i-- {
		if p.messages[i].Type == MessageDone {
			return p.messages[i].OK
		}
	}
	return false
}

// doneChan is closed when the run publishes its done message. It is created
// with the log, so a caller can take it before the run has ended and a run
// that already ended hands back an already-closed channel.
func (p *progressLog) doneChan() <-chan struct{} { return p.done }
