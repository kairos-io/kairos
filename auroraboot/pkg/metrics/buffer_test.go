package metrics_test

import (
	"sync"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/metrics"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func sample(n uint64) store.NodeMetrics {
	return store.NodeMetrics{
		SampledAt:     time.Date(2026, 9, 29, 0, 0, int(n), 0, time.UTC),
		UptimeSeconds: n,
	}
}

var _ = Describe("Buffer", func() {
	It("uses DefaultCapacity when capacity is below 1", func() {
		b := metrics.NewBuffer(0)
		for i := uint64(1); i <= metrics.DefaultCapacity+5; i++ {
			b.Record("n1", sample(i))
		}
		Expect(b.Samples("n1")).To(HaveLen(metrics.DefaultCapacity))
	})

	It("keeps samples oldest first and evicts beyond capacity", func() {
		b := metrics.NewBuffer(120)
		for i := uint64(1); i <= 125; i++ {
			b.Record("n1", sample(i))
		}
		s := b.Samples("n1")
		Expect(s).To(HaveLen(120))
		Expect(s[0].UptimeSeconds).To(Equal(uint64(6)))
		Expect(s[119].UptimeSeconds).To(Equal(uint64(125)))

		latest, ok := b.Latest("n1")
		Expect(ok).To(BeTrue())
		Expect(latest.UptimeSeconds).To(Equal(uint64(125)))
	})

	It("sets a zero SampledAt to the receive time", func() {
		b := metrics.NewBuffer(10)
		before := time.Now().UTC()
		b.Record("n1", store.NodeMetrics{UptimeSeconds: 1})
		latest, ok := b.Latest("n1")
		Expect(ok).To(BeTrue())
		Expect(latest.SampledAt.IsZero()).To(BeFalse())
		Expect(latest.SampledAt).To(BeTemporally(">=", before))
		Expect(latest.SampledAt.Location()).To(Equal(time.UTC))
	})

	It("reports no data for unknown nodes", func() {
		b := metrics.NewBuffer(10)
		_, ok := b.Latest("nope")
		Expect(ok).To(BeFalse())
		Expect(b.Samples("nope")).To(BeEmpty())
		Expect(b.AllLatest()).To(BeEmpty())
	})

	It("returns copies that are not aliased", func() {
		b := metrics.NewBuffer(10)
		m := sample(1)
		m.Load = []float64{1, 2, 3}
		m.Disks = []store.DiskMetrics{{Mount: "/", TotalBytes: 10, UsedBytes: 5}}
		m.CPU = &store.CPUMetrics{UsedPercent: 50}
		b.Record("n1", m)

		// Mutating the caller's value after Record does not change the buffer.
		m.Load[0] = 99
		m.CPU.UsedPercent = 99

		s := b.Samples("n1")
		s[0].UptimeSeconds = 42
		s[0].Load[1] = 42
		s[0].Disks[0].Mount = "/changed"
		s = append(s, sample(2))
		_ = s

		all := b.AllLatest()
		all["n1"].Load[2] = 42
		delete(all, "n1")

		latest, _ := b.Latest("n1")
		latest.CPU.UsedPercent = 7

		again := b.Samples("n1")
		Expect(again).To(HaveLen(1))
		Expect(again[0].UptimeSeconds).To(Equal(uint64(1)))
		Expect(again[0].Load).To(Equal([]float64{1, 2, 3}))
		Expect(again[0].Disks[0].Mount).To(Equal("/"))
		Expect(again[0].CPU.UsedPercent).To(Equal(50.0))
		Expect(b.AllLatest()).To(HaveKey("n1"))
		l, _ := b.Latest("n1")
		Expect(l.Load).To(Equal([]float64{1, 2, 3}))
		Expect(l.CPU.UsedPercent).To(Equal(50.0))
	})

	It("returns the latest per node from AllLatest", func() {
		b := metrics.NewBuffer(10)
		b.Record("a", sample(1))
		b.Record("a", sample(2))
		b.Record("b", sample(3))
		all := b.AllLatest()
		Expect(all).To(HaveLen(2))
		Expect(all["a"].UptimeSeconds).To(Equal(uint64(2)))
		Expect(all["b"].UptimeSeconds).To(Equal(uint64(3)))
	})

	It("forgets a node", func() {
		b := metrics.NewBuffer(10)
		b.Record("a", sample(1))
		b.Record("b", sample(2))
		b.Forget("a")
		_, ok := b.Latest("a")
		Expect(ok).To(BeFalse())
		Expect(b.Samples("a")).To(BeEmpty())
		Expect(b.AllLatest()).To(HaveLen(1))
		Expect(b.AllLatest()).To(HaveKey("b"))
		b.Forget("unknown")
	})

	It("satisfies Recorder", func() {
		var r metrics.Recorder = metrics.NewBuffer(1)
		r.Record("a", sample(1))
	})

	It("is safe for concurrent use", func() {
		b := metrics.NewBuffer(50)
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func(g int) {
				defer GinkgoRecover()
				defer wg.Done()
				id := []string{"a", "b"}[g%2]
				for i := uint64(1); i <= 200; i++ {
					b.Record(id, sample(i))
					_ = b.Samples(id)
					_, _ = b.Latest(id)
					_ = b.AllLatest()
				}
			}(g)
		}
		wg.Wait()
		Expect(b.Samples("a")).To(HaveLen(50))
		Expect(b.Samples("b")).To(HaveLen(50))
	})

	It("caps the disks and load of a sample", func() {
		b := metrics.NewBuffer(10)
		m := sample(1)
		m.Disks = make([]store.DiskMetrics, 100)
		m.Load = []float64{1, 2, 3, 4, 5}
		b.Record("a", m)
		l, ok := b.Latest("a")
		Expect(ok).To(BeTrue())
		Expect(l.Disks).To(HaveLen(metrics.MaxDisks))
		Expect(metrics.MaxDisks).To(Equal(16))
		Expect(l.Load).To(Equal([]float64{1, 2, 3}))
		Expect(metrics.MaxLoad).To(Equal(3))
	})
})
