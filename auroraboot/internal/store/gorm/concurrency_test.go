package gorm_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm Store concurrency", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	BeforeEach(func() {
		// Use a file-backed SQLite DB (NOT :memory:) so WAL mode and
		// busy_timeout are actually exercised — in-memory SQLite has
		// different locking semantics.
		dbPath := filepath.Join(GinkgoT().TempDir(), "concurrency.db")
		var err error
		s, err = gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() {
		Expect(s.Close()).To(Succeed())
	})

	It("does not return 'database is locked' under concurrent command-status writers", func() {
		const workers = 8

		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			failures []error
		)

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer GinkgoRecover()
				defer wg.Done()

				node := &store.ManagedNode{MachineID: fmt.Sprintf("concurrent-%d", idx)}
				if err := s.Register(ctx, node); err != nil {
					mu.Lock()
					failures = append(failures, err)
					mu.Unlock()
					return
				}

				cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
				if err := s.CommandCreate(ctx, cmd); err != nil {
					mu.Lock()
					failures = append(failures, err)
					mu.Unlock()
					return
				}

				if err := s.UpdateStatusForNode(ctx, cmd.ID, node.ID, store.CommandCompleted, "ok"); err != nil {
					mu.Lock()
					failures = append(failures, err)
					mu.Unlock()
				}
			}(i)
		}

		wg.Wait()

		for _, err := range failures {
			Expect(strings.ToLower(err.Error())).NotTo(
				ContainSubstring("database is locked"),
				"busy_timeout should make writers wait-and-retry instead of failing",
			)
		}
		Expect(failures).To(BeEmpty())

		// All updates must have landed.
		nodes, err := s.NodeList(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).To(HaveLen(workers))
		for _, n := range nodes {
			cmds, err := s.ListByNode(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds).To(HaveLen(1))
			Expect(cmds[0].Phase).To(Equal(store.CommandCompleted))
		}
	})
})
