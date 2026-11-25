package p2p

import "syscall"

// fitFDLimit makes sure the process can hold the connections the server
// accepts: it raises the soft RLIMIT_NOFILE towards the hard one when needed
// and, when that is not enough, lowers the connection cap so the listener never
// runs out of descriptors (which would starve every peer, not only the abusive one).
func fitFDLimit(maxConns int, logf func(string, ...any)) int {
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil {
		return maxConns
	}
	want, conns := planFDLimit(rl.Cur, rl.Max, maxConns)
	if want > rl.Cur {
		nl := syscall.Rlimit{Cur: want, Max: rl.Max}
		if syscall.Setrlimit(syscall.RLIMIT_NOFILE, &nl) == nil {
			rl = nl
		} else {
			want, conns = planFDLimit(rl.Cur, rl.Cur, maxConns)
		}
	}
	if conns < maxConns {
		logf("p2p: the open files limit is %d, accepting at most %d connections", rl.Cur, conns)
	}
	return conns
}

// planFDLimit says which soft limit to ask for (up to the hard one) and how many
// connections fit in it. Every connection is a socket, and the process needs
// some descriptors for itself.
func planFDLimit(cur, max uint64, maxConns int) (want uint64, conns int) {
	need := uint64(maxConns)*2 + 64
	want = cur
	if cur < need && max > cur {
		want = need
		if want > max {
			want = max
		}
	}
	conns = maxConns
	if want < need {
		conns = int((want - 64) / 2)
		if want < 64 || conns < 8 {
			conns = 8
		}
		if conns > maxConns {
			conns = maxConns
		}
	}
	return want, conns
}
