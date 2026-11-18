package auth_test

import (
	"math"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// spentBurstProbe is how many requests past the expected burst an allowance spec
// fires. It must be large enough that the refill cannot cover all of it on a
// healthy run, otherwise the upper bound expectAllowance checks would be vacuous.
const spentBurstProbe = 20

// batch is the outcome of firing a group of requests through a limiter: the
// status code of each request, and how long the whole group took.
type batch struct {
	codes   []int
	elapsed time.Duration
}

// expectAllowance asserts that a batch saw the limiter's instantaneous allowance
// served and the rest refused.
//
// The bucket refills while the batch runs, so the only sound upper bound on how
// many requests may legitimately have been served is the burst plus whatever
// refilled during the elapsed time. Asserting instead that one fixed index was
// refused is not sound, and it is what made these specs flaky: at 50 rps a token
// comes back every 20ms, and a loaded runner has taken longer than that to fire
// the batch, so the request just past the burst was legitimately served
// (AuroraBoot#776, #893). The refill is rounded down, so the bound stays tight:
// a healthy batch finishes inside one token period, the slack is zero, and a
// limiter whose burst is one too large still fails here.
func expectAllowance(b batch, rps float64, burst int) {
	ExpectWithOffset(1, b.codes).To(HaveLen(burst + spentBurstProbe))
	for i, code := range b.codes[:burst] {
		ExpectWithOffset(1, code).To(Equal(http.StatusOK),
			"request %d of the burst of %d must be served", i+1, burst)
	}

	served := 0
	for _, code := range b.codes {
		if code == http.StatusOK {
			served++
		}
	}
	refilled := int(math.Floor(b.elapsed.Seconds() * rps))
	ExpectWithOffset(1, served).To(BeNumerically("<=", burst+refilled),
		"a burst of %d plus the %d token(s) refilled in %s is the most that may be served", burst, refilled, b.elapsed)
}

var _ = Describe("Rate limiting", func() {
	var e *echo.Echo

	BeforeEach(func() { e = echo.New() })

	okHandler := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }

	Describe("NodeRateLimiter", func() {
		// fireAsNode sends n requests through the limiter with the given node ID set
		// in context (as the node-auth middleware would have), returning the status
		// codes and the elapsed time. The remote address is left at httptest's
		// default so the IP plays no part — the key under test is the node ID. An
		// empty nodeID models an admin request, which sets no node ID.
		fireAsNode := func(mw echo.MiddlewareFunc, nodeID string, n int) batch {
			codes := make([]int, 0, n)
			handler := mw(okHandler)
			start := time.Now()
			for i := 0; i < n; i++ {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := httptest.NewRecorder()
				c := e.NewContext(req, rec)
				if nodeID != "" {
					c.Set(auth.ContextKeyNodeID, nodeID)
				}
				_ = handler(c)
				codes = append(codes, rec.Code)
			}
			return batch{codes: codes, elapsed: time.Since(start)}
		}

		It("limits a node once its burst is spent", func() {
			mw := auth.NodeRateLimiter(1, 2) // burst 2, ~1 rps refill
			expectAllowance(fireAsNode(mw, "node-a", 2+spentBurstProbe), 1, 2)
		})

		It("keys per node — one node's burst does not affect another", func() {
			mw := auth.NodeRateLimiter(1, 2)
			_ = fireAsNode(mw, "node-a", 5) // exhaust node-a
			codes := fireAsNode(mw, "node-b", 2).codes
			Expect(codes).To(Equal([]int{http.StatusOK, http.StatusOK}))
		})

		It("never limits admin requests (no node ID in context)", func() {
			mw := auth.NodeRateLimiter(1, 2)
			codes := fireAsNode(mw, "", 10).codes // admin: AuthNodeID == ""
			for _, code := range codes {
				Expect(code).To(Equal(http.StatusOK))
			}
		})

		It("passes through when disabled (non-positive rps)", func() {
			mw := auth.NodeRateLimiter(0, 0)
			codes := fireAsNode(mw, "node-a", 50).codes
			for _, code := range codes {
				Expect(code).To(Equal(http.StatusOK))
			}
		})

		It("keeps the generous floor as the default burst at the shipped rate", func() {
			mw := auth.NodeRateLimiter(auth.DefaultNodeRateLimitRPS, 0)
			b := fireAsNode(mw, "node-a", auth.DefaultNodeRateLimitBurst+spentBurstProbe)
			expectAllowance(b, auth.DefaultNodeRateLimitRPS, auth.DefaultNodeRateLimitBurst)
		})

		It("defaults the burst to one second of the rate once that exceeds the floor", func() {
			// The whole point of --node-rate-limit: at 50 rps the old flat burst of
			// 20 was the binding constraint, so the 21st request in an instant was
			// refused even though the node's sustained budget was 50/s.
			mw := auth.NodeRateLimiter(50, 0)
			expectAllowance(fireAsNode(mw, "node-a", 50+spentBurstProbe), 50, 50)
		})

		It("lets an explicit burst override the default in both directions", func() {
			mw := auth.NodeRateLimiter(50, 3)
			expectAllowance(fireAsNode(mw, "node-a", 3+spentBurstProbe), 50, 3)
		})
	})

	Describe("RegistrationRateLimiter", func() {
		// fireFromIP sends n requests through the limiter from the given client IP,
		// returning the status codes and the elapsed time. No node ID is set —
		// registration has no node identity yet, so the key is the client IP.
		fireFromIP := func(mw echo.MiddlewareFunc, ip string, n int) batch {
			codes := make([]int, 0, n)
			handler := mw(okHandler)
			start := time.Now()
			for i := 0; i < n; i++ {
				req := httptest.NewRequest(http.MethodPost, "/", nil)
				req.RemoteAddr = ip + ":12345"
				rec := httptest.NewRecorder()
				c := e.NewContext(req, rec)
				_ = handler(c)
				codes = append(codes, rec.Code)
			}
			return batch{codes: codes, elapsed: time.Since(start)}
		}

		It("limits an IP once its burst is spent", func() {
			mw := auth.RegistrationRateLimiter(0.5, 2)
			expectAllowance(fireFromIP(mw, "203.0.113.5", 2+spentBurstProbe), 0.5, 2)
		})

		It("keys per IP — one IP's flood does not affect another", func() {
			mw := auth.RegistrationRateLimiter(0.5, 2)
			_ = fireFromIP(mw, "203.0.113.5", 5) // exhaust one IP
			codes := fireFromIP(mw, "198.51.100.9", 2).codes
			Expect(codes).To(Equal([]int{http.StatusOK, http.StatusOK}))
		})

		It("passes through when disabled (non-positive rps)", func() {
			mw := auth.RegistrationRateLimiter(0, 0)
			codes := fireFromIP(mw, "203.0.113.5", 50).codes
			for _, code := range codes {
				Expect(code).To(Equal(http.StatusOK))
			}
		})

		It("keeps the generous floor as the default burst at the shipped rate", func() {
			mw := auth.RegistrationRateLimiter(auth.DefaultRegisterRateLimitRPS, 0)
			b := fireFromIP(mw, "203.0.113.5", auth.DefaultRegisterRateLimitBurst+spentBurstProbe)
			expectAllowance(b, auth.DefaultRegisterRateLimitRPS, auth.DefaultRegisterRateLimitBurst)
		})

		It("defaults the burst to one second of the rate once that exceeds the floor", func() {
			mw := auth.RegistrationRateLimiter(40, 0)
			expectAllowance(fireFromIP(mw, "203.0.113.5", 40+spentBurstProbe), 40, 40)
		})

		It("lets an explicit burst override the default in both directions", func() {
			mw := auth.RegistrationRateLimiter(40, 2)
			expectAllowance(fireFromIP(mw, "203.0.113.5", 2+spentBurstProbe), 40, 2)
		})
	})
})
