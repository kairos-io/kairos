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
GitHub Actions would, and executes the result under bash. The tag and
artifact formats come out of `_build-iso.yaml`'s input defaults and
`release.yaml`'s overrides rather than being retyped here. Nothing in this
file is copy-pasted shell logic or a copied format string: a future drift
shows up as a mismatch between what the script observed the files doing,
not as a passing test of text duplicated into this file.

Deriving the segment correctly is only half of it -- the two sides also
have to agree on which cell is which. So the second half walks pr.yaml and
master.yaml and, for every `reusable-qemu-test.yaml` / `_uki-test.yaml`
job, resolves the `kubernetes_distro` it passes against the `build-iso`
matrix cell sharing its base_image/arch/model. Exactly one cell has to
match, and its computed variant has to be the variant the test job claims;
otherwise the suite is booting one cell's image and reporting as another's
coverage. Build cells are also checked pairwise for a unique pushed tag, a
unique uploaded artifact name and a unique upload-sarif category, all three
of which collapse when two cells differ only by k8s distro.

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


def wf_call_inputs(doc):
    # YAML 1.1 resolves a bare `on` to the boolean True, which is what
    # PyYAML hands back for a workflow's trigger key.
    trigger = doc["on"] if "on" in doc else doc[True]
    return trigger["workflow_call"]["inputs"]


def input_default(doc, name):
    spec = wf_call_inputs(doc)[name]
    if "default" in spec:
        return spec["default"]
    if spec.get("required"):
        raise KeyError(f"{name} is required and has no default")
    # A non-required workflow_call input without a default arrives as the
    # zero value of its type.
    return {"string": "", "boolean": False, "number": 0}[spec["type"]]


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
                artifact_format=None, base_image="ghcr.io/kairos-io/hadron:v0.5.1",
                model="generic", arch="amd64"):
    """Run reusable-factory.yaml's real "Setup environment" step.

    Returns UPPERCASE keys for shell variables printed after the step, and
    lowercase keys for whatever the step wrote to $GITHUB_OUTPUT (the same
    values later steps read back as steps.setup.outputs.*).
    """
    inputs = {
        "kubernetes_distro": distro,
        "kubernetes_version": version,
        "version": "v4.5.0",
        "kairos_version": "v4.0.0",
        "base_image": base_image,
        "model": model,
        "arch": arch,
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
                        '"$TAG" "$ARTIFACT_NAME" "$K8S" "$DEFAULT_TAG"'
                        '\ncat "$GITHUB_OUTPUT"', prelude)
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


# ---- the tag and artifact formats, read out of the workflows that set them ---
# Retyping these as literals would let someone reorder _build-iso.yaml's
# default (say, move $K8S after $VERSION) and keep this file green while the
# real build and the two mirrors disagreed.
build_iso = load(f"{WF}/_build-iso.yaml")
release = load(f"{WF}/release.yaml")

PR_TAG_FMT = input_default(build_iso, "custom_tag_format")
PR_ARTIFACT_FMT = input_default(build_iso, "custom_artifact_format")


def release_format(key):
    """The single value release.yaml's jobs pass for `key`.

    release.yaml repeats the same format across its build jobs; one copy
    drifting is itself the bug, so read them all and refuse to pick one
    when they disagree.
    """
    fmts = {
        name: job["with"][key]
        for name, job in release["jobs"].items()
        if key in (job.get("with") or {})
    }
    if not fmts:
        raise SystemExit(f"release.yaml passes no {key}")
    if len(set(fmts.values())) != 1:
        raise SystemExit(f"release.yaml's {key} copies diverged: {fmts}")
    return next(iter(fmts.values())), len(fmts)


REL_TAG_FMT, rel_tag_jobs = release_format("custom_tag_format")
REL_ARTIFACT_FMT, rel_artifact_jobs = release_format("custom_artifact_format")

print(f"tag formats in use ({rel_tag_jobs} release jobs agree):")
print(f"  _build-iso.yaml default: {PR_TAG_FMT}")
print(f"  release.yaml override:   {REL_TAG_FMT}")
print(f"artifact formats in use ({rel_artifact_jobs} release jobs agree):")
print(f"  _build-iso.yaml default: {PR_ARTIFACT_FMT}")
print(f"  release.yaml override:   {REL_ARTIFACT_FMT}\n")

failures = []


def fail(label, got, want):
    failures.append((label, got, want))
    print(f"  [FAIL] {label}\n         got  {got}\n         want {want}")


def check(label, got, want, terse=False):
    if got != want:
        return fail(label, got, want)
    print(f"  [ok  ] {label}")
    if not terse:
        print(f"         got  {got}\n         want {want}")


print("== pr/master build cells (kubernetes_version: auto) ==")
core = factory_tag("", "auto", tag_format=PR_TAG_FMT)
k3s = factory_tag("k3s", "auto", tag_format=PR_TAG_FMT)
k0s = factory_tag("k0s", "auto", tag_format=PR_TAG_FMT)
check("core tag", core["TAG"], "hadron-v0.5.1-core-amd64-generic-v4.5.0")
check("k3s tag", k3s["TAG"], "hadron-v0.5.1-standard-amd64-generic-k3s-v4.5.0")
check("k0s tag", k0s["TAG"], "hadron-v0.5.1-standard-amd64-generic-k0s-v4.5.0")
print(f"  k3s vs k0s collide? {k3s['TAG'] == k0s['TAG']}")
if k3s["TAG"] == k0s["TAG"]:
    fail("k3s/k0s tag collision", k3s["TAG"], "distinct tags")

print("\n== release cells (kubernetes_version pinned) -- must be byte-identical to before ==")
relcore = factory_tag("", "auto", tag_format=REL_TAG_FMT,
                      artifact_format=REL_ARTIFACT_FMT)
check("release core", relcore["TAG"], "v0.5.1-core-amd64-generic-v4.5.0")
relk3s = factory_tag("k3s", "v1.33.9+k3s1", tag_format=REL_TAG_FMT,
                     artifact_format=REL_ARTIFACT_FMT)
check("release k3s", relk3s["TAG"],
      "v0.5.1-standard-amd64-generic-v4.5.0-k3s-v1.33.9-k3s1")
relk0s = factory_tag("k0s", "v1.33.4+k0s.0", tag_format=REL_TAG_FMT,
                     artifact_format=REL_ARTIFACT_FMT)
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


# ---- callers: each test job must resolve to one build cell ------------------
# The mirror checks above take the distro from this file on both sides, so they
# cannot see a test job aimed at a cell that was built with a different
# kubernetes_distro. That is the coupling that breaks quietly: `standard` alone
# does not identify a k3s image once a k0s cell computes the same variant, so a
# test job can end up booting one cell's image while reporting as the other's
# coverage. Walk the callers instead and require each test job to land on
# exactly one build-iso matrix cell.

CALLEES = {
    "_build-iso.yaml": build_iso,
    "reusable-qemu-test.yaml": qemu,
    "_uki-test.yaml": uki,
}

MATRIX_REF = re.compile(
    r"^\$\{\{\s*matrix\.([A-Za-z0-9_-]+)\s*(?:\|\|\s*'([^']*)'\s*)?\}\}$"
)


def callee(job):
    return job.get("uses", "").rsplit("/", 1)[-1]


def matrix_rows(job):
    include = ((job.get("strategy") or {}).get("matrix") or {}).get("include")
    return include or [{}]


_RAISE = object()


def resolve(job, row, key, dynamic=_RAISE):
    """What `job` passes for `key`, for one matrix row.

    Falls back to the callee's declared input default when the job does not
    pass the key at all, so an omitted input is read from the workflow that
    defines it rather than assumed here. An expression this script cannot
    evaluate is a hard error unless the caller supplies `dynamic`, which is
    then returned in its place.
    """
    passed = job.get("with") or {}
    if key not in passed:
        return input_default(CALLEES[callee(job)], key)
    val = passed[key]
    if not isinstance(val, str):
        return val
    m = MATRIX_REF.match(val.strip())
    if m:
        name, fallback = m.group(1), m.group(2)
        if name not in row:
            if fallback is None:
                raise SystemExit(f"matrix.{name} missing from row {row}")
            return fallback
        got = row[name]
        if got == "" and fallback is not None:
            return fallback
        return got
    if "${{" in val:
        if dynamic is not _RAISE:
            return dynamic
        raise SystemExit(f"cannot resolve {key}={val!r} without a GitHub runner")
    return val


def as_bool(val):
    return val if isinstance(val, bool) else str(val).lower() == "true"


GH_EXPR = re.compile(r"\$\{\{(.+?)\}\}")
GH_TERNARY = re.compile(r"([\w.]+)\s*&&\s*'([^']*)'\s*\|\|\s*'([^']*)'")


def eval_ghexpr(expr, inputs, outputs):
    """Evaluate the expression subset the factory's `category:` values use:
    context lookups plus the `<cond> && '<a>' || '<b>'` idiom."""

    def lookup(ref):
        ref = ref.strip()
        for prefix, source in (("inputs.", inputs),
                               ("steps.setup.outputs.", outputs)):
            if ref.startswith(prefix):
                return source[ref[len(prefix):]]
        raise SystemExit(f"unsupported expression context: {ref}")

    def repl(m):
        body = m.group(1).strip()
        tern = GH_TERNARY.fullmatch(body)
        if tern:
            cond = lookup(tern.group(1))
            falsy = cond in ("", "false", False, None)
            return tern.group(3) if falsy else tern.group(2)
        return str(lookup(body))

    return GH_EXPR.sub(repl, expr)


sarif_steps = [
    (st["name"], st["with"]["category"])
    for st in factory["jobs"]["build"]["steps"]
    if "upload-sarif" in str(st.get("uses", ""))
]
if not sarif_steps:
    raise SystemExit("reusable-factory.yaml has no upload-sarif step")

for caller_name in ("pr.yaml", "master.yaml"):
    caller = load(f"{WF}/{caller_name}")
    print(f"\n== {caller_name}: build cells ==")
    cells = []
    for job_name, job in caller["jobs"].items():
        if callee(job) != "_build-iso.yaml":
            continue
        for row in matrix_rows(job):
            cell = {
                "name": resolve(job, row, "name"),
                "base_image": resolve(job, row, "base_image"),
                "arch": resolve(job, row, "arch"),
                "model": resolve(job, row, "model"),
                "distro": resolve(job, row, "kubernetes_distro"),
                "version": resolve(job, row, "kubernetes_version"),
                "trusted_boot": as_bool(resolve(job, row, "trusted_boot")),
                "iso": as_bool(resolve(job, row, "iso")),
                # The factory gates both upload-artifact steps on iso/raw,
                # so an image-only cell publishes no artifact name at all.
                # A `raw:` the caller computes on the runner counts as an
                # upload here, which keeps the check on the strict side.
                "uploads": as_bool(resolve(job, row, "iso", dynamic=True))
                or as_bool(resolve(job, row, "raw", dynamic=True)),
            }
            cell["out"] = factory_tag(
                cell["distro"], cell["version"],
                trusted_boot=cell["trusted_boot"],
                base_image=cell["base_image"], model=cell["model"],
                arch=cell["arch"],
                tag_format=resolve(job, row, "custom_tag_format"),
                artifact_format=resolve(job, row, "custom_artifact_format"),
            )
            cells.append(cell)
            line = (f"  {cell['name']:<26} distro={cell['distro'] or '-':<4}"
                    f" variant={cell['out']['variant']:<8}"
                    f" iso={str(cell['iso']):<5} tag={cell['out']['TAG']}")
            if cell["uploads"]:
                line += f"\n  {'':<26} artifact={cell['out']['ARTIFACT']}"
            print(line)

    # Two cells whose pushed tag is identical race each other into the same
    # registry reference; two cells uploading the same artifact name fail the
    # run on upload-artifact (v4 and later refuse a duplicate name, and
    # v4.3.0-rc3 shipped exactly that collision -- every k3s/k0s cell writing
    # kairos-hadron-v0.5.1-standard-amd64-generic.iso.zip); and two cells whose
    # SARIF category is identical are rejected by upload-sarif as a
    # misconfigured run.
    seen = {}
    for cell in cells:
        got = cell["out"]["TAG"]
        if got in seen:
            fail(f"{caller_name} pushed tag", got,
                 f"unique per cell ({seen[got]} vs {cell['name']})")
        seen[got] = cell["name"]
    seen = {}
    for cell in cells:
        if not cell["uploads"]:
            continue
        got = cell["out"]["ARTIFACT"]
        if got in seen:
            fail(f"{caller_name} uploaded artifact", got,
                 f"unique per cell ({seen[got]} vs {cell['name']})")
        seen[got] = cell["name"]
    for step_name, expr in sarif_steps:
        seen = {}
        for cell in cells:
            cat = eval_ghexpr(expr, {"arch": cell["arch"], "model": cell["model"],
                                     "trusted_boot": cell["trusted_boot"]},
                              cell["out"])
            if cat in seen:
                fail(f"{caller_name} {step_name!r} category", cat,
                     f"unique per cell ({seen[cat]} vs {cell['name']})")
            seen[cat] = cell["name"]

    print(f"\n== {caller_name}: test jobs must name the cell they consume ==")
    for job_name, job in caller["jobs"].items():
        target = callee(job)
        if target not in ("reusable-qemu-test.yaml", "_uki-test.yaml"):
            continue
        # reusable-qemu-test.yaml boots plain ISOs; _uki-test.yaml is the
        # trusted-boot path, so it consumes the trusted_boot cells.
        wants_uki = target == "_uki-test.yaml"
        for row in matrix_rows(job):
            label = job_name
            if "test" in row:
                label += f" [{row['test']}]"
            distro = resolve(job, row, "kubernetes_distro")
            version = resolve(job, row, "kubernetes_version")
            variant = resolve(job, row, "variant")
            key = (resolve(job, row, "base_image"), resolve(job, row, "arch"),
                   resolve(job, row, "model"), wants_uki)
            siblings = [c for c in cells
                        if (c["base_image"], c["arch"], c["model"],
                            c["trusted_boot"]) == key]
            hits = [c for c in siblings
                    if (c["distro"], c["version"]) == (distro, version)]
            if len(hits) != 1:
                offered = ", ".join(
                    f"{c['name']}={c['distro'] or '-'}/{c['version']}"
                    for c in siblings
                ) or "no cell with that base_image/arch/model"
                check(f"{label} distro={distro or '-'}/{version}",
                      f"{len(hits)} build cells ({offered})",
                      "exactly 1 build cell")
                continue
            cell = hits[0]
            check(f"{label} -> {cell['name']} variant",
                  variant, cell["out"]["variant"], terse=True)
            if not cell["iso"]:
                check(f"{label} -> {cell['name']} artifact",
                      "iso: false, no ISO artifact to download",
                      "a cell built with iso: true")

print()
if failures:
    print(f"{len(failures)} FAILURE(S)")
    for label, got, want in failures:
        print(f"  {label}\n    got  {got}\n    want {want}")
    sys.exit(1)
print("all checks pass")
