package clusterplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/mudler/go-pluggable"
	yip "github.com/mudler/yip/pkg/schema"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The cluster config the provider hands back. Its content only has to be
// recognisable in the file afterwards.
func testProviderConfig() yip.YipConfig {
	return yip.YipConfig{Name: "cluster-token-in-here"}
}

// bootEvent builds the payload agent.boot sends, with cluster_config_path
// pointing at path.
func bootEvent(path string) *pluggable.Event {
	cfg := fmt.Sprintf("#cloud-config\ncluster:\n  cluster_token: sekret\n  role: worker\n  cluster_config_path: %s\n", path)
	data, err := json.Marshal(bus.EventPayload{Config: cfg})
	Expect(err).ToNot(HaveOccurred())

	return &pluggable.Event{Name: bus.EventBoot, Data: string(data)}
}

// callWithin runs f and fails the spec if it has not returned by bound, so
// that a blocking open fails the run instead of hanging it forever.
func callWithin(bound time.Duration, f func() error) error {
	done := make(chan error, 1)
	go func() {
		defer GinkgoRecover()
		done <- f()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(bound):
		Fail(fmt.Sprintf("writeClusterConfig did not return within %s", bound))
		return nil
	}
}

var _ = Describe("writeClusterConfig", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
	})

	It("creates the directory the cluster config goes in", func() {
		// A node whose /usr/local/cloud-config does not exist yet used to
		// fail the whole boot event here, on ENOENT.
		path := filepath.Join(root, "cloud-config", "cluster.kairos.yaml")

		Expect(writeClusterConfig(path, testProviderConfig())).To(Succeed())

		content, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(HavePrefix("#cloud-config\n"))
		Expect(string(content)).To(ContainSubstring("cluster-token-in-here"))

		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
	})

	It("writes the config once the directory is already there", func() {
		path := filepath.Join(root, "cluster.kairos.yaml")

		Expect(writeClusterConfig(path, testProviderConfig())).To(Succeed())
		// A second boot rewrites it in place.
		Expect(writeClusterConfig(path, testProviderConfig())).To(Succeed())

		content, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(HavePrefix("#cloud-config\n"))
	})

	It("refuses a symlink at the config path, and leaves its target alone", func() {
		victim := filepath.Join(root, "victim")
		Expect(os.WriteFile(victim, []byte("do not truncate me"), 0644)).To(Succeed())

		path := filepath.Join(root, "cluster.kairos.yaml")
		Expect(os.Symlink(victim, path)).To(Succeed())

		err := writeClusterConfig(path, testProviderConfig())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(path))
		Expect(err.Error()).To(ContainSubstring("it is a symlink"))

		// O_TRUNC never reached the target.
		content, err := os.ReadFile(victim)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("do not truncate me"))
	})

	It("does not block on a named pipe nobody is reading", func() {
		path := filepath.Join(root, "cluster.kairos.yaml")
		Expect(syscall.Mkfifo(path, 0600)).To(Succeed())

		err := callWithin(10*time.Second, func() error {
			return writeClusterConfig(path, testProviderConfig())
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("it is a named pipe"))
	})

	It("refuses a named pipe somebody is reading, instead of writing the token into it", func() {
		path := filepath.Join(root, "cluster.kairos.yaml")
		Expect(syscall.Mkfifo(path, 0600)).To(Succeed())

		// A reader makes the write-side open succeed, so only the fstat
		// stands between the cluster token and the pipe.
		r, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		Expect(err).ToNot(HaveOccurred())
		defer r.Close()

		err = callWithin(10*time.Second, func() error {
			return writeClusterConfig(path, testProviderConfig())
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("a named pipe"))
		Expect(err.Error()).To(ContainSubstring("not a regular file"))

		// Nothing was handed to the pipe. The write side is closed again by
		// now, so the read reports EOF rather than EAGAIN; either way it
		// carries no bytes.
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		Expect(n).To(Equal(0))
	})

	It("refuses a directory at the config path", func() {
		path := filepath.Join(root, "cluster.kairos.yaml")
		Expect(os.Mkdir(path, 0700)).To(Succeed())

		err := writeClusterConfig(path, testProviderConfig())
		Expect(err).To(HaveOccurred())
	})

	It("takes the mode down to 0600 on a file somebody else created", func() {
		// O_CREATE applies its perm argument only when it creates the file,
		// so a pre-created 0666 file used to receive the cluster token and
		// stay world readable.
		path := filepath.Join(root, "cluster.kairos.yaml")
		Expect(os.WriteFile(path, []byte("planted"), 0666)).To(Succeed())
		Expect(os.Chmod(path, 0666)).To(Succeed())

		Expect(writeClusterConfig(path, testProviderConfig())).To(Succeed())

		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))

		content, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).ToNot(ContainSubstring("planted"))
	})
})

var _ = Describe("onBoot", func() {
	var plugin ClusterPlugin

	BeforeEach(func() {
		plugin = ClusterPlugin{Provider: func(cluster Cluster) yip.YipConfig {
			return testProviderConfig()
		}}
	})

	It("says what went wrong with the file, not that the event failed to parse", func() {
		// Every error past the JSON unmarshal used to read
		// "failed to parse boot event", which sends the reader to the
		// payload rather than to the filesystem.
		root := GinkgoT().TempDir()
		path := filepath.Join(root, "cluster.kairos.yaml")
		Expect(os.Mkdir(path, 0700)).To(Succeed())

		response := plugin.onBoot(bootEvent(path))

		Expect(response.Error).ToNot(BeEmpty())
		Expect(response.Error).ToNot(ContainSubstring("failed to parse boot event"))
		Expect(response.Error).To(ContainSubstring(path))
	})

	It("still reports a payload that is not JSON as a parse failure", func() {
		response := plugin.onBoot(&pluggable.Event{Name: bus.EventBoot, Data: "not json"})

		Expect(response.Error).To(ContainSubstring("failed to parse boot event"))
	})

	It("writes the provider config through to the path the cluster asked for", func() {
		root := GinkgoT().TempDir()
		path := filepath.Join(root, "cloud-config", "cluster.kairos.yaml")

		response := plugin.onBoot(bootEvent(path))
		Expect(response.Error).To(BeEmpty())

		content, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("cluster-token-in-here"))
	})
})
