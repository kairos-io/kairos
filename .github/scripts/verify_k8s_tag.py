#!/usr/bin/env python3
"""Regression guard for kairos-io/kairos#4579.

The `$K8S` image-tag segment is hand-derived in shell in three places --
reusable-factory.yaml's "Setup environment" step (the source of truth) and
two mirrors that reconstruct the same tag to find the image under test:
reusable-qemu-test.yaml's "Set Image tag" step and _uki-test.yaml's
"Compute IMAGE_NAME" step. reusable-factory.yaml is a cross-repo callable
(also used by kairos-io/hadron and friends), so it cannot `uses:` a local
composite action or `scripts/` reference to collapse the three copies into
one -- see the coder journal for kairos-io/kairos#4579 round 0. That means
nothing stops the three copies drifting again except a check that actually
executes all three and compares results.

This script extracts the real `run:` shell out of the three workflow files
via PyYAML, substitutes the `${{ inputs.* }}` expressions the same way
GitHub Actions would, and executes the result under bash. Nothing here is
copy-pasted shell logic: a future drift between the three copies shows up
as a mismatch between what this script observed the files doing, not as a
passing test of text duplicated into this file.

Usage:
    python3 .github/scripts/verify_k8s_tag.py

Requires: PyYAML (`pip install pyyaml`). No repo-wide Python dependency is
introduced -- this is a standalone script, not part of any package, and it
is not currently wired into a CI job (see `make verify-k8s-tag`, which
mirrors how `make lint-workflows-actions`/actionlint are local-only checks
in this repo too). Run it by hand after touching any of the three files
above, or any caller (pr.yaml/master.yaml) that feeds them
kubernetes_distro/kubernetes_version.
"""

import os
import re
import subprocess
import sys

import yaml

WF = os.path.normpath(
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "workflows")
)


def load(path):
    with open(path) as fh:
        return yaml.safe_load(fh)


def step(doc, job, name_fragment):
    for st in doc["jobs"][job]["steps"]:
        if name_fragment in st.get("name", ""):
            return st
    raise KeyError(name_fragment)


def subst_inputs(script, inputs):
    def repl(m):
        key = m.group(1)
        if key not in inputs:
            raise KeyError(f"unstubbed input: {key}")
        return str(inputs[key])

    script = re.sub(r"\$\{\{\s*inputs\.([A-Za-z0-9_]+)\s*\}\}", repl, script)
    script = re.sub(r"\$\{\{\s*github\.sha\s*\}\}", "deadbeef", script)
    return script


def run(script, env_prelude=""):
    full = "set -u\n" + env_prelude + "\n" + script + "\n"
    p = subprocess.run(["bash", "-c", full], capture_output=True, text=True)
    if p.returncode != 0:
        print(full)
        print(p.stderr)
        raise SystemExit(f"shell failed: {p.returncode}")
    return p.stdout


# ---- factory: run the real "Setup environment" step -------------------------
factory = load(f"{WF}/reusable-factory.yaml")
setup_script = step(factory, "build", "Setup environment")["run"]


def factory_tag(distro, version, trusted_boot=False, tag_format=None,
                artifact_format=None):
    inputs = {
        "kubernetes_distro": distro,
        "kubernetes_version": version,
        "version": "v4.5.0",
        "kairos_version": "v4.0.0",
        "base_image": "ghcr.io/kairos-io/hadron:v0.5.1",
        "model": "generic",
        "arch": "amd64",
        "trusted_boot": "true" if trusted_boot else "false",
        "custom_tag_format": tag_format or "",
        "custom_artifact_format": artifact_format or "",
        "registry_domain": "ghcr.io",
        "registry_namespace": "kairos-io/kairos",
        "registry_repository": "",
        "keys_dir": "",
        "sysext_dir": "",
        "single_efi_cmdline": "",
    }
    script = subst_inputs(setup_script, inputs)
    prelude = (
        'GITHUB_ENV=$(mktemp); GITHUB_OUTPUT=$(mktemp); export GITHUB_ENV GITHUB_OUTPUT\n'
        'git() { echo "v4.5.0"; }\n'
    )
    out = run(script + '\nprintf "TAG=%s\\nARTIFACT=%s\\nK8S=%s\\nDEFAULT_TAG=%s\\n" '
                        '"$TAG" "$ARTIFACT_NAME" "$K8S" "$DEFAULT_TAG"', prelude)
    return dict(
        line.split("=", 1) for line in out.strip().splitlines() if "=" in line
    )


# ---- qemu test mirror -------------------------------------------------------
qemu = load(f"{WF}/reusable-qemu-test.yaml")
qemu_step = step(qemu, "test", "Set Image tag")


def qemu_tag(distro, version, variant):
    script = qemu_step["run"]
    prelude = (
        'GITHUB_ENV=$(mktemp); export GITHUB_ENV\n'
        'git() { echo "v4.5.0"; }\n'
        'PREFIX_TEMPLATE=\'ghcr.io/kairos-io/kairos/${flavor}\'\n'
        f'FLAVOR=hadron\nFLAVOR_RELEASE=v0.5.1\nVARIANT={variant}\n'
        f'ARCH=amd64\nMODEL=generic\n'
        f'KUBERNETES_DISTRO="{distro}"\nKUBERNETES_VERSION_INPUT="{version}"\n'
    )
    out = run(script + '\ncat "$GITHUB_ENV"', prelude)
    return out.strip().split("IMAGE_NAME=")[1]


# ---- uki test mirror --------------------------------------------------------
uki = load(f"{WF}/_uki-test.yaml")
uki_step = step(uki, "test", "Compute IMAGE_NAME")


def uki_tag(distro, version, variant):
    script = uki_step["run"]
    prelude = (
        'GITHUB_ENV=$(mktemp); export GITHUB_ENV\n'
        'PREFIX_TEMPLATE=\'ghcr.io/kairos-io/kairos/${flavor}\'\n'
        f'FLAVOR=hadron-trusted\nFLAVOR_RELEASE=v0.5.1\nVARIANT={variant}\n'
        f'ARCH=amd64\nMODEL=generic\nVERSION=v4.5.0\n'
        f'KUBERNETES_DISTRO="{distro}"\nKUBERNETES_VERSION_INPUT="{version}"\n'
    )
    out = run(script + '\ncat "$GITHUB_ENV"', prelude)
    return out.strip().split("IMAGE_NAME=")[1]


PR_TAG_FMT = "$FLAVOR-$FLAVOR_RELEASE-$VARIANT-$ARCH-$MODEL$K8S-$VERSION$UKI"
REL_TAG_FMT = "$FLAVOR_RELEASE-$VARIANT-$ARCH-$MODEL-$VERSION$K8S$UKI"

failures = []


def check(label, got, want):
    status = "ok  " if got == want else "FAIL"
    if got != want:
        failures.append((label, got, want))
    print(f"  [{status}] {label}\n         got  {got}\n         want {want}")


print("== pr/master build cells (kubernetes_version: auto) ==")
core = factory_tag("", "auto", tag_format=PR_TAG_FMT)
k3s = factory_tag("k3s", "auto", tag_format=PR_TAG_FMT)
k0s = factory_tag("k0s", "auto", tag_format=PR_TAG_FMT)
check("core tag", core["TAG"], "hadron-v0.5.1-core-amd64-generic-v4.5.0")
check("k3s tag", k3s["TAG"], "hadron-v0.5.1-standard-amd64-generic-k3s-v4.5.0")
check("k0s tag", k0s["TAG"], "hadron-v0.5.1-standard-amd64-generic-k0s-v4.5.0")
print(f"  k3s vs k0s collide? {k3s['TAG'] == k0s['TAG']}")
if k3s["TAG"] == k0s["TAG"]:
    failures.append(("k3s/k0s tag collision", k3s["TAG"], "distinct tags"))

print("\n== release cells (kubernetes_version pinned) -- must be byte-identical to before ==")
relcore = factory_tag("", "auto", tag_format=REL_TAG_FMT,
                      artifact_format="kairos-$FLAVOR-$FLAVOR_RELEASE-$VARIANT-$ARCH-$MODEL-$VERSION$K8S$UKI")
check("release core", relcore["TAG"], "v0.5.1-core-amd64-generic-v4.5.0")
relk3s = factory_tag("k3s", "v1.33.9+k3s1", tag_format=REL_TAG_FMT,
                     artifact_format="kairos-$FLAVOR-$FLAVOR_RELEASE-$VARIANT-$ARCH-$MODEL-$VERSION$K8S$UKI")
check("release k3s", relk3s["TAG"],
      "v0.5.1-standard-amd64-generic-v4.5.0-k3s-v1.33.9-k3s1")
relk0s = factory_tag("k0s", "v1.33.4+k0s.0", tag_format=REL_TAG_FMT,
                     artifact_format="kairos-$FLAVOR-$FLAVOR_RELEASE-$VARIANT-$ARCH-$MODEL-$VERSION$K8S$UKI")
check("release k0s", relk0s["TAG"],
      "v0.5.1-standard-amd64-generic-v4.5.0-k0s-v1.33.4-k0s.0")
check("release k3s artifact", relk3s["ARTIFACT"],
      "kairos-hadron-v0.5.1-standard-amd64-generic-v4.5.0-k3s-v1.33.9-k3s1")

print("\n== DEFAULT_TAG (no custom format) -- must match the old two-branch shape ==")
check("default core", factory_tag("", "auto")["DEFAULT_TAG"],
      "v0.5.1-core-amd64-generic-v4.5.0")
check("default k3s pinned", factory_tag("k3s", "v1.33.9+k3s1")["DEFAULT_TAG"],
      "v0.5.1-standard-amd64-generic-v4.5.0-k3s-v1.33.9-k3s1")
check("default k0s auto", factory_tag("k0s", "auto")["DEFAULT_TAG"],
      "v0.5.1-standard-amd64-generic-v4.5.0-k0s")

print("\n== DEFAULT artifact name: no trailing bare dash on auto ==")
check("default artifact k0s auto", factory_tag("k0s", "auto")["ARTIFACT"],
      "kairos-v0.5.1-standard-amd64-generic-v4.5.0-k0s")

print("\n== test mirrors must reconstruct exactly what the build cell pushed ==")
check("qemu test-standard", qemu_tag("k3s", "auto", "standard"),
      "ghcr.io/kairos-io/kairos/hadron:" + k3s["TAG"])
check("qemu test-core", qemu_tag("", "auto", "core"),
      "ghcr.io/kairos-io/kairos/hadron:" + core["TAG"])

uki_k3s = factory_tag("k3s", "auto", trusted_boot=True, tag_format=PR_TAG_FMT)
uki_core = factory_tag("", "auto", trusted_boot=True, tag_format=PR_TAG_FMT)
# the UKI build cells use the hadron-trusted base, so re-derive with it
print("  (uki build tags use base hadron-trusted; compare suffix shape)")
check("uki standard mirror", uki_tag("k3s", "auto", "standard"),
      "ghcr.io/kairos-io/kairos/hadron-trusted:"
      + uki_k3s["TAG"].replace("hadron-", "hadron-trusted-", 1))
check("uki core mirror", uki_tag("", "auto", "core"),
      "ghcr.io/kairos-io/kairos/hadron-trusted:"
      + uki_core["TAG"].replace("hadron-", "hadron-trusted-", 1))

print("\n== pinned version flows through the mirrors too ==")
check("qemu pinned", qemu_tag("k3s", "v1.33.9+k3s1", "standard"),
      "ghcr.io/kairos-io/kairos/hadron:"
      + factory_tag("k3s", "v1.33.9+k3s1", tag_format=PR_TAG_FMT)["TAG"])

print()
if failures:
    print(f"{len(failures)} FAILURE(S)")
    sys.exit(1)
print("all checks pass")
