package cmd

import (
	"errors"
	"net/url"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// loadKubeConfig resolves a Kubernetes REST config. When path is set it is
// loaded verbatim from that single file. Otherwise in-cluster config is
// attempted first; a genuine in-cluster failure (e.g. a broken service-account
// token) is surfaced rather than silently masked. When we are simply not
// running in a cluster (rest.ErrNotInCluster), we fall through to the default
// client-go loading rules, which honour a multi-file KUBECONFIG env value and
// ~/.kube/config.
func loadKubeConfig(path string) (*rest.Config, error) {
	if path != "" {
		return clientcmd.BuildConfigFromFlags("", path)
	}
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, rest.ErrNotInCluster) {
		return nil, err
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// sanitizeClusterURL strips credential-bearing components from a kubeconfig
// server URL before it is echoed back over the admin API. A kubeconfig can
// legally embed credentials in the userinfo, or (through unusual setups) in
// query or fragment components (e.g. bearer tokens smuggled as ?token=...).
// A naive echo would leak any of those to admin callers of GET
// /api/v1/system/builder. We keep scheme+host+path and drop everything else.
// A URL we cannot fully parse or that has no scheme+host is passed through
// unchanged; the alternative is to over-mutilate a bare host:port value.
func sanitizeClusterURL(host string) string {
	u, err := url.Parse(host)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return host
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}
