package collector_test

import (
	"context"
	"net"
	"net/http"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Collector Suite")
}

type ServerCloseFunc func()

func startAssetServer(path string) (ServerCloseFunc, int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port

	ctx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		err := http.Serve(listener, http.FileServer(http.Dir(path)))
		select {
		case <-ctx.Done(): // We closed it with the CancelFunc, ignore the error
			return
		default: // We didnt' close it, return the error
			Expect(err).ToNot(HaveOccurred())
		}
	}()

	stopFunc := func() {
		cancelFunc()
		listener.Close()
	}

	return stopFunc, port, nil
}
