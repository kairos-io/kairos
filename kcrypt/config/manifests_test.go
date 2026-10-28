package config_test

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kcrypt config suite")
}

var manifestFiles = []string{
	filepath.Join("manager", "manager.yaml"),
	filepath.Join("rbac", "auth_proxy_service.yaml"),
}

func loadManifests() ([]appsv1.Deployment, []corev1.Service) {
	var deployments []appsv1.Deployment
	var services []corev1.Service
	for _, f := range manifestFiles {
		data, err := os.ReadFile(f)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		r := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
		for {
			doc, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			ExpectWithOffset(1, err).ToNot(HaveOccurred(), "splitting %s", f)
			if len(bytes.TrimSpace(doc)) == 0 {
				continue
			}
			var meta struct {
				Kind string `json:"kind"`
			}
			ExpectWithOffset(1, yaml.Unmarshal(doc, &meta)).To(Succeed(), "decoding %s", f)
			switch meta.Kind {
			case "Deployment":
				var d appsv1.Deployment
				ExpectWithOffset(1, yaml.Unmarshal(doc, &d)).To(Succeed(), "decoding Deployment in %s", f)
				deployments = append(deployments, d)
			case "Service":
				var s corev1.Service
				ExpectWithOffset(1, yaml.Unmarshal(doc, &s)).To(Succeed(), "decoding Service in %s", f)
				services = append(services, s)
			}
		}
	}
	return deployments, services
}

func selects(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func declaresPort(pod corev1.PodTemplateSpec, target intstr.IntOrString) bool {
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			if target.Type == intstr.String && p.Name == target.StrVal {
				return true
			}
			if target.Type == intstr.Int && p.ContainerPort == target.IntVal {
				return true
			}
		}
	}
	return false
}

var _ = Describe("Manifests", func() {
	It("give every Service a selector and target port that reach a manager pod", func() {
		deployments, services := loadManifests()
		Expect(deployments).ToNot(BeEmpty())
		Expect(services).ToNot(BeEmpty())

		for _, svc := range services {
			Expect(svc.Spec.Selector).ToNot(BeEmpty(), "Service %s has no selector", svc.Name)

			var pods []corev1.PodTemplateSpec
			for _, d := range deployments {
				if selects(svc.Spec.Selector, d.Spec.Template.Labels) {
					pods = append(pods, d.Spec.Template)
				}
			}
			Expect(pods).ToNot(BeEmpty(), "Service %s selector %v matches no Deployment pod template", svc.Name, svc.Spec.Selector)

			for _, p := range svc.Spec.Ports {
				target := p.TargetPort
				if target.Type == intstr.Int && target.IntVal == 0 {
					target = intstr.FromInt32(p.Port)
				}
				found := false
				for _, pod := range pods {
					if declaresPort(pod, target) {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "Service %s port %s targets %s, which no selected container declares", svc.Name, p.Name, target.String())
			}
		}
	})
})
