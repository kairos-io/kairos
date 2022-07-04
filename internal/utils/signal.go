package utils

import (
	"os"
	"os/signal"
)

func OnSignal(fn func(), sig ...os.Signal) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, sig...)
	go func() {
		<-sigs
		fn()
	}()
}
