package sshimage_test

import (
	"os"
	"syscall"
)

func blocksOf(st os.FileInfo) (int64, bool) {
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return s.Blocks, true
}
