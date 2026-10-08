package phonehome

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("metrics parsers", func() {
	It("reads total and idle jiffies from the aggregate cpu line", func() {
		stat := "cpu  100 5 50 800 40 0 5 0 0 0\ncpu0 50 2 25 400 20 0 3 0 0 0\nintr 1 2 3\n"
		s, err := parseProcStat(strings.NewReader(stat))
		Expect(err).ToNot(HaveOccurred())
		// idle = idle + iowait; total = user..steal (guest is already in user).
		Expect(s.idle).To(Equal(uint64(840)))
		Expect(s.total).To(Equal(uint64(1000)))
	})

	It("rejects a stat file without an aggregate cpu line", func() {
		_, err := parseProcStat(strings.NewReader("intr 1 2 3\n"))
		Expect(err).To(HaveOccurred())
	})

	It("computes the used percentage between two samples", func() {
		prev := cpuSample{total: 1000, idle: 800}
		cur := cpuSample{total: 2000, idle: 1100}
		Expect(cpuUsedPercent(prev, cur)).To(BeNumerically("~", 70.0, 0.001))
	})

	It("reports no cpu usage when the counters did not move or went backwards", func() {
		Expect(cpuUsedPercent(cpuSample{total: 10, idle: 5}, cpuSample{total: 10, idle: 5})).To(BeNumerically("<", 0))
		Expect(cpuUsedPercent(cpuSample{total: 10, idle: 5}, cpuSample{total: 4, idle: 1})).To(BeNumerically("<", 0))
	})

	It("reads MemTotal and MemAvailable in bytes", func() {
		mi := "MemTotal:       32718516 kB\nMemFree:         1234 kB\nMemAvailable:    6920600 kB\n"
		m, err := parseMeminfo(strings.NewReader(mi))
		Expect(err).ToNot(HaveOccurred())
		Expect(m.TotalBytes).To(Equal(uint64(32718516 * 1024)))
		Expect(m.AvailableBytes).To(Equal(uint64(6920600 * 1024)))
	})

	It("rejects meminfo without MemTotal", func() {
		_, err := parseMeminfo(strings.NewReader("MemFree: 1 kB\n"))
		Expect(err).To(HaveOccurred())
	})

	It("reads the three load averages", func() {
		l, err := parseLoadavg("13.90 12.10 10.40 3/1024 4567\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(l).To(Equal([]float64{13.9, 12.1, 10.4}))
	})

	It("reads the uptime in whole seconds", func() {
		u, err := parseUptime("3895200.57 123456.00\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(u).To(Equal(uint64(3895200)))
	})
})

var _ = Describe("gatherMetrics", func() {
	var root string
	var restore func()

	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		Expect(os.WriteFile(p, []byte(content), 0o644)).To(Succeed())
	}

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		write("proc/stat", "cpu  100 0 100 800 0 0 0 0 0 0\n")
		write("proc/meminfo", "MemTotal: 1000 kB\nMemAvailable: 250 kB\n")
		write("proc/loadavg", "0.50 0.40 0.30 1/100 1\n")
		write("proc/uptime", "120.9 10.0\n")
		write("sys/class/thermal/thermal_zone0/temp", "45000\n")
		write("sys/class/thermal/thermal_zone1/temp", "71500\n")
		restore = setMetricsSources(
			filepath.Join(root, "proc"),
			filepath.Join(root, "sys"),
			func(path string) (uint64, uint64, error) {
				switch path {
				case "/usr/local":
					return 240 << 30, 158 << 30, nil
				case "/run/initramfs/cos-state":
					return 24 << 30, 10 << 30, nil
				}
				return 0, 0, errors.New("not mounted")
			},
		)
	})
	AfterEach(func() { restore() })

	It("reports memory, load, uptime, temperature and the mounted Kairos partitions", func() {
		t := &cpuTracker{}
		m := t.gather()
		Expect(m).ToNot(BeNil())
		Expect(m.SampledAt.IsZero()).To(BeFalse())
		Expect(m.UptimeSeconds).To(Equal(uint64(120)))
		Expect(m.Load).To(Equal([]float64{0.5, 0.4, 0.3}))
		Expect(m.Memory).To(Equal(&MemoryMetrics{TotalBytes: 1000 * 1024, AvailableBytes: 250 * 1024}))
		Expect(m.TemperatureC).ToNot(BeNil())
		Expect(*m.TemperatureC).To(BeNumerically("~", 71.5, 0.001))
		Expect(m.Disks).To(Equal([]DiskMetrics{
			{Label: "COS_PERSISTENT", Mount: "/usr/local", TotalBytes: 240 << 30, UsedBytes: 158 << 30},
			{Label: "COS_STATE", Mount: "/run/initramfs/cos-state", TotalBytes: 24 << 30, UsedBytes: 10 << 30},
		}))
	})

	It("leaves cpu out of the first sample and reports it from the second", func() {
		t := &cpuTracker{}
		Expect(t.gather().CPU).To(BeNil())
		write("proc/stat", "cpu  400 0 200 1200 0 0 0 0 0 0\n")
		m := t.gather()
		Expect(m.CPU).ToNot(BeNil())
		// total 1000 -> 1800 and idle 800 -> 1200: 400 of 800 jiffies busy.
		Expect(m.CPU.UsedPercent).To(BeNumerically("~", 50.0, 0.001))
	})

	It("omits every source that cannot be read instead of failing", func() {
		Expect(os.RemoveAll(filepath.Join(root, "proc"))).To(Succeed())
		Expect(os.RemoveAll(filepath.Join(root, "sys"))).To(Succeed())
		m := (&cpuTracker{}).gather()
		Expect(m).ToNot(BeNil())
		Expect(m.Memory).To(BeNil())
		Expect(m.Load).To(BeEmpty())
		Expect(m.TemperatureC).To(BeNil())
		Expect(m.CPU).To(BeNil())
		Expect(m.Disks).To(HaveLen(2))
	})
})
