package collector_test

import (
	"fmt"
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

func startAssetServer(path string) (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: http.FileServer(http.Dir(path))}

	go func() {
		defer GinkgoRecover()
		if err := server.ListenAndServe(); err != nil {
			Expect(err).ToNot(HaveOccurred())
		}
	}()

	return port, nil
}
