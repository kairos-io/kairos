package provider

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/sdk/bus"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/go-pluggable"
)

// fakeProvider describes a provider that is installed, or not, without
// touching the filesystem the real lookups read.
func fakeProvider(name, path, version string) providerBinary {
	return providerBinary{
		name:    name,
		bin:     func() string { return path },
		version: func(loggerpkg.KairosLogger) string { return version },
	}
}

// infoEventFor builds the event kairos-init publishes when it asks about one
// provider by name.
func infoEventFor(provider string) *pluggable.Event {
	data, err := json.Marshal(bus.ProviderPayload{Provider: provider})
	Expect(err).ToNot(HaveOccurred())
	return &pluggable.Event{Name: bus.InitProviderInfo, Data: string(data)}
}

var _ = Describe("Provider info event", func() {
	var l loggerpkg.KairosLogger

	BeforeEach(func() {
		l = loggerpkg.NewKairosLogger("test", "fatal", false)
	})

	Describe("choosing which provider to report", func() {
		// An image can carry both, because kairos-init takes --provider more
		// than once. One of them used to be lost: the k0s block overwrote the
		// k3s one with no condition between them.
		both := func() []providerBinary {
			return []providerBinary{
				fakeProvider(K0s, "/usr/bin/k0s", "v1.33.1+k0s.0"),
				fakeProvider(K3s, "/usr/bin/k3s", "v1.33.1+k3s1"),
			}
		}

		It("reports the provider that was asked about, not the last one installed", func() {
			info, found := installedProvider(K3s, both(), l)
			Expect(found).To(BeTrue())
			Expect(info.Provider).To(Equal(K3s))
			Expect(info.Version).To(Equal("v1.33.1+k3s1"))
		})

		It("reports the other one when that is the one asked about", func() {
			info, found := installedProvider(K0s, both(), l)
			Expect(found).To(BeTrue())
			Expect(info.Provider).To(Equal(K0s))
			Expect(info.Version).To(Equal("v1.33.1+k0s.0"))
		})

		It("reports nothing when the provider asked about is not installed", func() {
			supported := []providerBinary{
				fakeProvider(K0s, "", ""),
				fakeProvider(K3s, "/usr/bin/k3s", "v1.33.1+k3s1"),
			}
			_, found := installedProvider(K0s, supported, l)
			Expect(found).To(BeFalse())
		})

		It("reports nothing when the provider asked about is not one it serves", func() {
			_, found := installedProvider("kubeadm", both(), l)
			Expect(found).To(BeFalse())
		})

		// An image built without --provider still has to report a provider
		// binary it inherited from the image it was built on.
		It("falls back to the first installed provider when none was asked about", func() {
			info, found := installedProvider("", both(), l)
			Expect(found).To(BeTrue())
			Expect(info.Provider).To(Equal(K0s))
		})

		It("skips a provider that is not installed when falling back", func() {
			supported := []providerBinary{
				fakeProvider(K0s, "", ""),
				fakeProvider(K3s, "/usr/bin/k3s", "v1.33.1+k3s1"),
			}
			info, found := installedProvider("", supported, l)
			Expect(found).To(BeTrue())
			Expect(info.Provider).To(Equal(K3s))
		})

		// kairos-init drops an answer that carries no version, so falling back
		// to a provider whose version could not be read, when another one on
		// the same image could, leaves the image with no software version.
		It("prefers a readable version when falling back", func() {
			supported := []providerBinary{
				fakeProvider(K0s, "/usr/bin/k0s", ""),
				fakeProvider(K3s, "/usr/bin/k3s", "v1.33.1+k3s1"),
			}
			info, found := installedProvider("", supported, l)
			Expect(found).To(BeTrue())
			Expect(info.Provider).To(Equal(K3s))
			Expect(info.Version).To(Equal("v1.33.1+k3s1"))
		})

		// The provider that was asked about is the answer even when its
		// version could not be read. No other provider can stand in for it.
		It("does not substitute another provider for the one asked about", func() {
			supported := []providerBinary{
				fakeProvider(K0s, "/usr/bin/k0s", ""),
				fakeProvider(K3s, "/usr/bin/k3s", "v1.33.1+k3s1"),
			}
			info, found := installedProvider(K0s, supported, l)
			Expect(found).To(BeTrue())
			Expect(info.Provider).To(Equal(K0s))
			Expect(info.Version).To(BeEmpty())
		})

		It("reports nothing when nothing is installed", func() {
			supported := []providerBinary{
				fakeProvider(K0s, "", ""),
				fakeProvider(K3s, "", ""),
			}
			_, found := installedProvider("", supported, l)
			Expect(found).To(BeFalse())
		})
	})

	Describe("reading the event payload", func() {
		It("asks about the provider the payload names", func() {
			e := infoEventFor(K3s)
			payload := bus.ProviderPayload{}
			Expect(json.Unmarshal([]byte(e.Data), &payload)).To(Succeed())
			Expect(payload.Provider).To(Equal(K3s))
		})

		// go-pluggable marshals a nil payload into the JSON null, which is
		// what a caller that configured no provider sends. That has to decode
		// into an empty request rather than fail the event.
		It("treats a null payload as asking about no provider", func() {
			resp := InfoEvent(&pluggable.Event{Name: bus.InitProviderInfo, Data: "null"})
			Expect(resp.State).ToNot(Equal(bus.EventResponseError))
			Expect(resp.Error).To(BeEmpty())
		})

		It("fails the event when the payload is not JSON", func() {
			resp := InfoEvent(&pluggable.Event{Name: bus.InitProviderInfo, Data: "{not json"})
			Expect(resp.State).To(Equal(bus.EventResponseError))
			Expect(resp.Error).ToNot(BeEmpty())
		})

		It("is not applicable when it does not serve the provider asked about", func() {
			resp := InfoEvent(infoEventFor("kubeadm"))
			Expect(resp.State).To(Equal(bus.EventResponseNotApplicable))
			Expect(resp.Data).To(BeEmpty())
		})
	})
})
