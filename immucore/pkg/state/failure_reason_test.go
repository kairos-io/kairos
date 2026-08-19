package state_test

import (
	"context"
	"errors"

	"github.com/kairos-io/immucore/pkg/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("State.FailureReason", func() {
	It("returns a generic message when nothing errored", func() {
		g := herd.DAG(herd.EnableInit)
		Expect(g.Add("ok", herd.WithCallback(func(_ context.Context) error {
			return nil
		}))).To(Succeed())
		Expect(g.Run(context.Background())).To(Succeed())

		s := &state.State{}
		Expect(s.FailureReason(g)).To(ContainSubstring("no specific operation"))
	})

	It("lists the failing operation and its error", func() {
		g := herd.DAG(herd.EnableInit)
		Expect(g.Add("boom", herd.WithCallback(func(_ context.Context) error {
			return errors.New("disk on fire")
		}))).To(Succeed())
		_ = g.Run(context.Background())

		s := &state.State{}
		reason := s.FailureReason(g)
		Expect(reason).To(ContainSubstring("boom"))
		Expect(reason).To(ContainSubstring("disk on fire"))
	})
})
