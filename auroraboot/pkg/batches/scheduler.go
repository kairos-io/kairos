// Package batches schedules rolling batches: it starts them, advances them
// when something they wait on happens and on a ticker, and pushes the commands
// the store created for them.
package batches

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// StopReasonUser is the reason recorded when a user stops a batch.
const StopReasonUser = "stopped by the user"

// Deliver pushes a command the store created to its node.
type Deliver func(ctx context.Context, cmd *store.NodeCommand)

// StartRequest describes a batch to start. The scheduler applies no defaults:
// callers fill Concurrency and RebootTimeoutSeconds.
type StartRequest struct {
	Command              string
	Args                 map[string]string
	Nodes                []*store.ManagedNode
	Selection            string
	GroupID              *string
	Concurrency          int // >= 0, 0 means every node at once
	StopOnFailure        bool
	RebootTimeoutSeconds int // > 0
}

// ErrNodesBusy lists nodes that are already in a batch that has not finished with them.
type ErrNodesBusy struct{ NodeIDs []string }

func (e *ErrNodesBusy) Error() string {
	return fmt.Sprintf("nodes already in a running batch: %s", strings.Join(e.NodeIDs, ", "))
}

// ErrNoNodes is returned when a batch would have no nodes.
var ErrNoNodes = errors.New("no nodes match the selection")

// Scheduler drives batches over a BatchStore.
type Scheduler struct {
	store   store.BatchStore
	deliver Deliver

	// startMu serializes the busy check and the creation in Start and WhenIdle
	// so two requests naming the same node cannot both pass the check. One
	// AuroraBoot process owns the database, so a process-level lock is enough.
	startMu sync.Mutex

	mu  sync.RWMutex
	now func() time.Time
}

// New creates a Scheduler that hands the commands batches create to deliver.
func New(bs store.BatchStore, deliver Deliver) *Scheduler {
	return &Scheduler{store: bs, deliver: deliver, now: time.Now}
}

// SetClock replaces time.Now, for tests.
func (s *Scheduler) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

func (s *Scheduler) clock() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.now()
}

// Start creates the batch and advances it once. Nodes listed twice get one row.
func (s *Scheduler) Start(ctx context.Context, req StartRequest) (*store.CommandBatch, error) {
	seen := map[string]bool{}
	var rows []*store.BatchNode
	var ids []string
	for _, n := range req.Nodes {
		if n == nil || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		ids = append(ids, n.ID)
		rows = append(rows, &store.BatchNode{
			NodeID:   n.ID,
			Position: len(rows),
			Hostname: n.Hostname,
			State:    store.BatchNodeWaiting,
		})
	}
	if len(rows) == 0 {
		return nil, ErrNoNodes
	}

	s.startMu.Lock()
	if err := s.checkIdle(ctx, ids); err != nil {
		s.startMu.Unlock()
		return nil, err
	}

	b := &store.CommandBatch{
		ID:                   uuid.NewString(),
		Command:              req.Command,
		Args:                 req.Args,
		Selection:            req.Selection,
		GroupID:              req.GroupID,
		Concurrency:          req.Concurrency,
		StopOnFailure:        req.StopOnFailure,
		RebootTimeoutSeconds: req.RebootTimeoutSeconds,
	}
	err := s.store.Create(ctx, b, rows)
	s.startMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("creating batch: %w", err)
	}

	s.Advance(ctx, b.ID)
	return s.store.Get(ctx, b.ID)
}

// WhenIdle calls create when none of nodeIDs has a row that is not final in
// any batch, stopped batches included: a stopped batch still settles the nodes
// it started. It returns *ErrNodesBusy without calling create otherwise.
//
// The check and create run under startMu, so no batch can start for one of
// nodeIDs between them.
func (s *Scheduler) WhenIdle(ctx context.Context, nodeIDs []string, create func() error) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if err := s.checkIdle(ctx, nodeIDs); err != nil {
		return err
	}
	return create()
}

// checkIdle returns *ErrNodesBusy when one of nodeIDs is in a batch that has
// not finished with it. Callers hold startMu.
func (s *Scheduler) checkIdle(ctx context.Context, nodeIDs []string) error {
	busy, err := s.store.BusyNodes(ctx, nodeIDs)
	if err != nil {
		return fmt.Errorf("checking busy nodes: %w", err)
	}
	if len(busy) > 0 {
		return &ErrNodesBusy{NodeIDs: busy}
	}
	return nil
}

// Advance advances one batch and delivers the commands it created. Errors are logged.
func (s *Scheduler) Advance(ctx context.Context, batchID string) {
	cmds, err := s.store.Advance(ctx, batchID, s.clock())
	if err != nil {
		if !errors.Is(err, store.ErrBatchNotFound) {
			log.Printf("batches: advancing batch %s: %v", batchID, err)
		}
		return
	}
	for _, cmd := range cmds {
		s.deliver(ctx, cmd)
	}
}

// NodeBooted settles the node's rebooting rows after it came back and advances
// the batches that touched. A nil bootedAt means the node registered.
func (s *Scheduler) NodeBooted(ctx context.Context, nodeID, bootState string, bootedAt *time.Time) {
	ids, err := s.store.NodeBooted(ctx, nodeID, bootState, bootedAt, s.clock())
	if err != nil {
		log.Printf("batches: settling node %s after boot: %v", nodeID, err)
		return
	}
	for _, id := range ids {
		s.Advance(ctx, id)
	}
}

// Stop stops a running batch.
func (s *Scheduler) Stop(ctx context.Context, batchID string) error {
	return s.store.Stop(ctx, batchID, StopReasonUser, s.clock())
}

// Tick advances every active batch.
func (s *Scheduler) Tick(ctx context.Context) {
	ids, err := s.store.ActiveIDs(ctx)
	if err != nil {
		log.Printf("batches: listing active batches: %v", err)
		return
	}
	for _, id := range ids {
		s.Advance(ctx, id)
	}
}

// Run postpones expired deadlines once, then calls Tick every interval until
// ctx is done. The postponement gives nodes that rebooted while AuroraBoot was
// down a full timeout to report in.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	if err := s.store.PostponeExpiredDeadlines(ctx, s.clock()); err != nil {
		log.Printf("batches: postponing expired deadlines: %v", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}
