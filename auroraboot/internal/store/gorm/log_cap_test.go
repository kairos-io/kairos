package gorm_test

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// logTarget is one kind of record whose logs the store appends to.
type logTarget struct {
	create func(ctx context.Context, s *gormstore.Store, id string)
	append func(ctx context.Context, s *gormstore.Store, id, chunk string) error
	logs   func(ctx context.Context, s *gormstore.Store, id string) string
}

var artifactLogs = logTarget{
	create: func(ctx context.Context, s *gormstore.Store, id string) {
		Expect(s.ArtifactCreate(ctx, &store.ArtifactRecord{ID: id})).To(Succeed())
	},
	append: func(ctx context.Context, s *gormstore.Store, id, chunk string) error {
		return s.ArtifactAppendLog(ctx, id, chunk)
	},
	logs: func(ctx context.Context, s *gormstore.Store, id string) string {
		logs, err := s.ArtifactGetLogs(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		return logs
	},
}

var extensionLogs = logTarget{
	create: func(ctx context.Context, s *gormstore.Store, id string) {
		Expect(s.ExtensionCreate(ctx, &store.ExtensionRecord{ID: id, Name: "ext"})).To(Succeed())
	},
	append: func(ctx context.Context, s *gormstore.Store, id, chunk string) error {
		return s.ExtensionAppendLog(ctx, id, chunk)
	},
	logs: func(ctx context.Context, s *gormstore.Store, id string) string {
		rec, err := s.ExtensionGetByID(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		return rec.Logs
	},
}

var _ = Describe("Stored log cap", func() {
	const smallCap = 100

	var (
		s   *gormstore.Store
		ctx context.Context
	)

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(":memory:")
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	// tail returns the last n characters of text.
	tail := func(text string, n int) string {
		return text[len(text)-n:]
	}

	for name, target := range map[string]logTarget{"artifact": artifactLogs, "extension": extensionLogs} {
		Context("on an "+name+" record", func() {
			BeforeEach(func() {
				DeferCleanup(gormstore.SetLogCap(smallCap))
				target.create(ctx, s, "rec-1")
			})

			It("appends unchanged up to the cap", func() {
				Expect(target.append(ctx, s, "rec-1", "first\n")).To(Succeed())
				Expect(target.append(ctx, s, "rec-1", "second\n")).To(Succeed())
				Expect(target.logs(ctx, s, "rec-1")).To(Equal("first\nsecond\n"))

				filler := strings.Repeat("f", smallCap-len("first\nsecond\n"))
				Expect(target.append(ctx, s, "rec-1", filler)).To(Succeed())
				Expect(target.logs(ctx, s, "rec-1")).To(Equal("first\nsecond\n" + filler))
			})

			It("keeps the newest text behind one marker line once past the cap", func() {
				all := strings.Repeat("a", 60) + strings.Repeat("b", 60)
				Expect(target.append(ctx, s, "rec-1", all[:60])).To(Succeed())
				Expect(target.append(ctx, s, "rec-1", all[60:])).To(Succeed())
				Expect(target.logs(ctx, s, "rec-1")).To(Equal(gormstore.LogTruncatedMarker + tail(all, smallCap)))
			})

			It("keeps the tail of a single chunk larger than the cap", func() {
				chunk := strings.Repeat("0123456789", 25)
				Expect(target.append(ctx, s, "rec-1", chunk)).To(Succeed())
				Expect(target.logs(ctx, s, "rec-1")).To(Equal(gormstore.LogTruncatedMarker + tail(chunk, smallCap)))
			})

			It("never stacks markers or grows past the cap over many appends", func() {
				all := ""
				for i := range 200 {
					line := fmt.Sprintf("line %03d\n", i)
					all += line
					Expect(target.append(ctx, s, "rec-1", line)).To(Succeed())

					got := target.logs(ctx, s, "rec-1")
					if len(all) <= smallCap {
						Expect(got).To(Equal(all))
						continue
					}
					Expect(got).To(Equal(gormstore.LogTruncatedMarker + tail(all, smallCap)))
					Expect(strings.Count(got, gormstore.LogTruncatedMarker)).To(Equal(1))
				}
			})

			It("drops NUL bytes so they cannot hide text from the cap", func() {
				Expect(target.append(ctx, s, "rec-1", "start\x00of build\n")).To(Succeed())
				Expect(target.logs(ctx, s, "rec-1")).To(Equal("start" + "of build\n"))

				for range 20 {
					Expect(target.append(ctx, s, "rec-1", "more\x00output\n")).To(Succeed())
				}
				got := target.logs(ctx, s, "rec-1")
				Expect(got).NotTo(ContainSubstring("\x00"))
				Expect(got).To(HavePrefix(gormstore.LogTruncatedMarker))
				Expect(got).To(HaveLen(len(gormstore.LogTruncatedMarker) + smallCap))
			})

			It("leaves a truncated log as it is when an empty chunk is appended", func() {
				chunk := strings.Repeat("x", smallCap+10)
				Expect(target.append(ctx, s, "rec-1", chunk)).To(Succeed())
				before := target.logs(ctx, s, "rec-1")
				Expect(target.append(ctx, s, "rec-1", "")).To(Succeed())
				Expect(target.logs(ctx, s, "rec-1")).To(Equal(before))
			})
		})
	}

	It("caps stored logs at 4 MiB of text", func() {
		Expect(gormstore.MaxLogChars).To(Equal(4 * 1024 * 1024))
		artifactLogs.create(ctx, s, "big")
		chunk := strings.Repeat("y", 3*1024*1024)
		Expect(artifactLogs.append(ctx, s, "big", chunk)).To(Succeed())
		Expect(artifactLogs.append(ctx, s, "big", strings.Repeat("z", 2*1024*1024))).To(Succeed())

		got := artifactLogs.logs(ctx, s, "big")
		Expect(got).To(HavePrefix(gormstore.LogTruncatedMarker))
		Expect(got).To(HaveLen(len(gormstore.LogTruncatedMarker) + gormstore.MaxLogChars))
		Expect(got).To(HaveSuffix(strings.Repeat("z", 2*1024*1024)))
	})
})
