import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import {
  listArtifacts,
  listSecureBootKeySets,
  type Artifact,
  type SecureBootKeySet,
} from "@/api/artifacts";
import {
  createExtension,
  type CreateExtensionInput,
  type ExtensionType,
} from "@/api/extensions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { PageHeader } from "@/components/PageHeader";
import { HierarchyChipInput } from "@/components/HierarchyChipInput";
import { WizardShell, type WizardStep } from "@/components/wizard/WizardShell";

type SourceMode = "artifact" | "image" | "dockerfile";
type Arch = "amd64" | "arm64" | "riscv64";

const STEPS = ["source", "configure", "review"] as const;
type StepKey = (typeof STEPS)[number];
const STEP_LABELS: Record<StepKey, string> = {
  source: "Source",
  configure: "Configure",
  review: "Review",
};

type FieldError = { field: string; step: StepKey; message: string };

export function ExtensionBuilder() {
  const navigate = useNavigate();
  const [step, setStep] = useState<StepKey>("source");
  const [maxReached, setMaxReached] = useState(0);
  // Errors of a step are shown only once the user tried to leave it.
  const [shownErrorSteps, setShownErrorSteps] = useState<Set<StepKey>>(new Set());
  const [name, setName] = useState("");
  const [sourceMode, setSourceMode] = useState<SourceMode>("image");
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [selectedArtifactId, setSelectedArtifactId] = useState("");
  const [extraSteps, setExtraSteps] = useState("");
  const [baseImage, setBaseImage] = useState("");
  const [dockerfile, setDockerfile] = useState("");

  const [type, setType] = useState<ExtensionType>("sysext");
  const [arch, setArch] = useState<Arch>("amd64");
  const [version, setVersion] = useState("v1.0");
  const [keySets, setKeySets] = useState<SecureBootKeySet[]>([]);
  const [signingKeySetId, setSigningKeySetId] = useState("");
  const [hierarchies, setHierarchies] = useState<string[]>([]);
  const [serviceReload, setServiceReload] = useState(false);

  const [submitting, setSubmitting] = useState(false);
  const [submitErr, setSubmitErr] = useState<string | null>(null);

  useEffect(() => {
    listArtifacts()
      .then((rows) => {
        const ready = rows.filter(
          (a) => a.phase === "Ready" && a.containerImage,
        );
        setArtifacts(ready);
        if (ready.length > 0) {
          setSourceMode("artifact");
          setSelectedArtifactId(ready[0].id);
        }
      })
      .catch(() => {});
    listSecureBootKeySets()
      .then((rows) => setKeySets(rows ?? []))
      .catch(() => {});
  }, []);

  // computeErrors lists every invalid field with the step it belongs to.
  // It runs on every render, so an error goes away as soon as its field
  // becomes valid.
  function computeErrors(): FieldError[] {
    const errs: FieldError[] = [];
    if (!name.trim()) {
      errs.push({ field: "name", step: "source", message: "Name is required" });
    }
    if (sourceMode === "image" && !baseImage.trim()) {
      errs.push({ field: "baseImage", step: "source", message: "Base image is required" });
    }
    if (sourceMode === "dockerfile" && !dockerfile.trim()) {
      errs.push({ field: "dockerfile", step: "source", message: "Dockerfile is required" });
    }
    if (sourceMode === "artifact" && !selectedArtifactId) {
      errs.push({ field: "artifact", step: "source", message: "Artifact is required" });
    }
    if (!version.trim()) {
      errs.push({ field: "version", step: "configure", message: "Version is required" });
    }
    return errs;
  }

  const liveErrors = computeErrors();
  const fieldError = (field: string) =>
    liveErrors.find((e) => e.field === field && shownErrorSteps.has(e.step))?.message;
  const stepIndex = STEPS.indexOf(step);
  const currentStepErrors = liveErrors.filter((e) => e.step === step);
  const stepHasErrors = (key: StepKey) => liveErrors.some((e) => e.step === key);

  const wizardSteps: WizardStep[] = STEPS.map((key, i) => {
    let state: WizardStep["state"];
    if (i === stepIndex) {
      state = "current";
    } else if (i < stepIndex) {
      state = stepHasErrors(key) ? "error" : "done";
    } else if (i <= maxReached && !STEPS.slice(0, i).some(stepHasErrors)) {
      state = stepHasErrors(key) ? "error" : "done";
    } else {
      state = "todo";
    }
    return { key, label: STEP_LABELS[key], state };
  });

  function goToStep(next: StepKey) {
    setStep(next);
    setMaxReached((prev) => Math.max(prev, STEPS.indexOf(next)));
  }

  // Next validates only the current step and stays on it when it is invalid.
  function handleNext() {
    setShownErrorSteps((prev) => (prev.has(step) ? prev : new Set(prev).add(step)));
    if (currentStepErrors.length > 0) return;
    goToStep(STEPS[stepIndex + 1]);
  }

  async function submit() {
    if (liveErrors.length > 0) {
      setShownErrorSteps(new Set(STEPS));
      goToStep(liveErrors[0].step);
      return;
    }
    setSubmitting(true);
    setSubmitErr(null);
    const input: CreateExtensionInput = {
      name,
      type,
      arch,
      version,
      source: {
        mode: sourceMode,
        artifactId:
          sourceMode === "artifact" ? selectedArtifactId : undefined,
        baseImage: sourceMode === "image" ? baseImage : undefined,
        dockerfile: sourceMode === "dockerfile" ? dockerfile : undefined,
        extraSteps:
          sourceMode === "artifact" && extraSteps ? extraSteps : undefined,
      },
      signingKeySetId: signingKeySetId || undefined,
      hierarchies: type === "sysext" && hierarchies.length > 0 ? hierarchies : undefined,
      serviceReload: type === "sysext" ? serviceReload : false,
    };
    try {
      const status = await createExtension(input);
      navigate(`/extensions/${status.id}`);
    } catch (e) {
      setSubmitErr(String(e));
      setSubmitting(false);
    }
  }

  const selectedArtifact = artifacts.find((a) => a.id === selectedArtifactId);
  const signingLabel = signingKeySetId
    ? keySets.find((k) => k.id === signingKeySetId)?.name || signingKeySetId
    : "Unsigned";

  return (
    <div>
      <PageHeader
        title="Build extension"
        description="A sysext extends /usr; a confext extends /etc. Both ship as a single signed .raw."
      />

      <WizardShell
        steps={wizardSteps}
        current={step}
        onStepChange={(key) => goToStep(key as StepKey)}
        footer={{
          onBack: stepIndex > 0 ? () => goToStep(STEPS[stepIndex - 1]) : () => navigate("/extensions"),
          backLabel: stepIndex > 0 ? "Back" : "Cancel",
          status:
            currentStepErrors.length === 0
              ? { tone: "success", text: "All required fields set" }
              : {
                  tone: "warning",
                  text: `${currentStepErrors.length} issue${currentStepErrors.length === 1 ? "" : "s"} on this step`,
                },
          primary:
            step === "review"
              ? {
                  label: "Build extension",
                  onClick: () => void submit(),
                  disabled: submitting,
                  loading: submitting,
                }
              : { label: `Next: ${STEP_LABELS[STEPS[stepIndex + 1]]}`, onClick: handleNext },
        }}
      >
      {step === "source" && (
        <div className="grid gap-6">
          <div className="max-w-md grid gap-1.5">
            <Label htmlFor="ext-name">Name</Label>
            <Input
              id="ext-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. tailscale-agent"
              aria-invalid={!!fieldError("name")}
            />
            <FieldErrorText message={fieldError("name")} />
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-sm">Image source</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-4">
              <div className="flex gap-2">
                <ModeButton
                  active={sourceMode === "artifact"}
                  onClick={() => setSourceMode("artifact")}
                >
                  From artifact
                </ModeButton>
                <ModeButton
                  active={sourceMode === "image"}
                  onClick={() => setSourceMode("image")}
                >
                  Base image
                </ModeButton>
                <ModeButton
                  active={sourceMode === "dockerfile"}
                  onClick={() => setSourceMode("dockerfile")}
                >
                  Dockerfile
                </ModeButton>
              </div>

              {sourceMode === "artifact" && (
                <>
                  <ArtifactPicker
                    artifacts={artifacts}
                    selectedId={selectedArtifactId}
                    onSelect={setSelectedArtifactId}
                    extraSteps={extraSteps}
                    onExtraStepsChange={setExtraSteps}
                  />
                  <FieldErrorText message={fieldError("artifact")} />
                </>
              )}
              {sourceMode === "image" && (
                <div className="grid gap-1.5">
                  <Label htmlFor="ext-base">Base image</Label>
                  <Input
                    id="ext-base"
                    value={baseImage}
                    onChange={(e) => setBaseImage(e.target.value)}
                    placeholder="e.g. ubuntu:24.04"
                    aria-invalid={!!fieldError("baseImage")}
                  />
                  <FieldErrorText message={fieldError("baseImage")} />
                </div>
              )}
              {sourceMode === "dockerfile" && (
                <div className="grid gap-1.5">
                  <Label htmlFor="ext-df">Dockerfile</Label>
                  <Textarea
                    id="ext-df"
                    rows={8}
                    value={dockerfile}
                    onChange={(e) => setDockerfile(e.target.value)}
                    placeholder="FROM ubuntu:24.04\nRUN apt-get install -y curl"
                    className="font-mono text-sm"
                    aria-invalid={!!fieldError("dockerfile")}
                  />
                  <FieldErrorText message={fieldError("dockerfile")} />
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      {step === "configure" && (
        <ConfigureStep
          type={type}
          onType={setType}
          arch={arch}
          onArch={setArch}
          version={version}
          onVersion={setVersion}
          versionError={fieldError("version")}
          keySets={keySets}
          signingKeySetId={signingKeySetId}
          onSigningKeySetId={setSigningKeySetId}
          hierarchies={hierarchies}
          onHierarchies={setHierarchies}
          serviceReload={serviceReload}
          onServiceReload={setServiceReload}
        />
      )}

      {step === "review" && (
        <ReviewStep
          name={name}
          type={type}
          arch={arch}
          version={version}
          sourceMode={sourceMode}
          baseImage={baseImage}
          artifactLabel={selectedArtifact ? selectedArtifact.name || selectedArtifact.id : selectedArtifactId}
          dockerfile={dockerfile}
          extraSteps={extraSteps}
          hierarchies={hierarchies}
          serviceReload={serviceReload}
          signingLabel={signingLabel}
          submitErr={submitErr}
        />
      )}
      </WizardShell>
    </div>
  );
}

function FieldErrorText({ message }: { message?: string }) {
  if (!message) return null;
  return <p className="text-xs text-danger-foreground">{message}</p>;
}

function ModeButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? "default" : "outline"}
      onClick={onClick}
      data-active={active}
    >
      {children}
    </Button>
  );
}

function ArtifactPicker({
  artifacts,
  selectedId,
  onSelect,
  extraSteps,
  onExtraStepsChange,
}: {
  artifacts: Artifact[];
  selectedId: string;
  onSelect: (id: string) => void;
  extraSteps: string;
  onExtraStepsChange: (s: string) => void;
}) {
  if (artifacts.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No Ready artifacts yet — build one first.
      </p>
    );
  }
  const selected = artifacts.find((a) => a.id === selectedId);
  return (
    <div className="grid gap-3">
      <Label>Pick an existing artifact</Label>
      <select
        className="border rounded-md px-3 py-2 text-sm bg-background"
        value={selectedId}
        onChange={(e) => onSelect(e.target.value)}
      >
        {artifacts.map((a) => (
          <option key={a.id} value={a.id}>
            {a.name || a.id} — {a.arch} — {a.kairosVersion}
          </option>
        ))}
      </select>
      {selected && (
        <p className="text-[11px] text-muted-foreground font-mono">
          {selected.containerImage}
        </p>
      )}
      <div className="grid gap-1.5">
        <Label className="text-xs">Steps on top (optional)</Label>
        <Textarea
          rows={4}
          value={extraSteps}
          onChange={(e) => onExtraStepsChange(e.target.value)}
          placeholder="RUN curl -fsSL https://tailscale.com/install.sh | sh"
          className="font-mono text-xs"
        />
        <p className="text-[11px] text-muted-foreground">
          Wrapped in <code>FROM &lt;artifact-image&gt;</code> before the
          extractor runs. Lines starting with <code>FROM</code> are rejected.
        </p>
      </div>
    </div>
  );
}

function ConfigureStep({
  type,
  onType,
  arch,
  onArch,
  version,
  onVersion,
  versionError,
  keySets,
  signingKeySetId,
  onSigningKeySetId,
  hierarchies,
  onHierarchies,
  serviceReload,
  onServiceReload,
}: {
  type: ExtensionType;
  onType: (t: ExtensionType) => void;
  arch: Arch;
  onArch: (a: Arch) => void;
  version: string;
  onVersion: (v: string) => void;
  versionError?: string;
  keySets: SecureBootKeySet[];
  signingKeySetId: string;
  onSigningKeySetId: (s: string) => void;
  hierarchies: string[];
  onHierarchies: (h: string[]) => void;
  serviceReload: boolean;
  onServiceReload: (s: boolean) => void;
}) {
  return (
    <div className="grid gap-6">
      <div className="grid md:grid-cols-2 gap-4">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Extension type</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-2 gap-2">
            <TypeCard
              label="sysext"
              desc="Overlay on /usr (and additional paths)"
              active={type === "sysext"}
              onClick={() => onType("sysext")}
            />
            <TypeCard
              label="confext"
              desc="Overlay on /etc"
              active={type === "confext"}
              onClick={() => onType("confext")}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Architecture</CardTitle>
          </CardHeader>
          <CardContent className="flex gap-2">
            {(["amd64", "arm64", "riscv64"] as const).map((a) => (
              <Button
                key={a}
                type="button"
                size="sm"
                variant={arch === a ? "default" : "outline"}
                onClick={() => onArch(a)}
              >
                {a}
              </Button>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Version</CardTitle>
          </CardHeader>
          <CardContent>
            <Label htmlFor="ext-version" className="sr-only">
              Version
            </Label>
            <Input
              id="ext-version"
              value={version}
              onChange={(e) => onVersion(e.target.value)}
              placeholder="v1.0"
              aria-invalid={!!versionError}
            />
            <FieldErrorText message={versionError} />
            <p className="text-[11px] text-muted-foreground mt-1.5">
              Tracked server-side for staleness detection.
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Signing (optional)</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-1.5">
            <Label htmlFor="ext-signing" className="sr-only">
              Signing key set
            </Label>
            <select
              id="ext-signing"
              className="border rounded-md px-3 py-2 text-sm bg-background"
              value={signingKeySetId}
              onChange={(e) => onSigningKeySetId(e.target.value)}
            >
              <option value="">Unsigned</option>
              {keySets.map((k) => (
                <option key={k.id} value={k.id}>
                  {k.name || k.id}
                </option>
              ))}
            </select>
            <p className="text-[11px] text-muted-foreground">
              Key sets come from Secure Boot keys.
            </p>
          </CardContent>
        </Card>
      </div>

      {type === "sysext" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Hierarchies</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4">
            <HierarchyChipInput
              value={hierarchies}
              onChange={onHierarchies}
              implicitRoot="/usr"
              quickAdds={["/opt", "/srv", "/var/lib"]}
            />
            <label className="flex gap-2 items-start text-sm">
              <input
                type="checkbox"
                checked={serviceReload}
                onChange={(e) => onServiceReload(e.target.checked)}
                className="mt-0.5"
              />
              <span>
                <span className="font-medium">
                  Reload services after install
                </span>
                <span className="block text-xs text-muted-foreground">
                  Sets <code className="font-mono">EXTENSION_RELOAD_MANAGER=1</code>.
                  Only needed when the extension ships systemd units.
                </span>
              </span>
            </label>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function TypeCard({
  label,
  desc,
  active,
  onClick,
}: {
  label: string;
  desc: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`text-left rounded-md border p-3 transition-colors ${
        active
          ? "border-primary bg-primary-soft ring-1 ring-primary"
          : "hover:bg-muted/30"
      }`}
    >
      <div className="font-medium text-sm">{label}</div>
      <div className="text-xs text-muted-foreground mt-0.5">{desc}</div>
    </button>
  );
}

function ReviewStep({
  name,
  type,
  arch,
  version,
  sourceMode,
  baseImage,
  artifactLabel,
  dockerfile,
  extraSteps,
  hierarchies,
  serviceReload,
  signingLabel,
  submitErr,
}: {
  name: string;
  type: ExtensionType;
  arch: Arch;
  version: string;
  sourceMode: SourceMode;
  baseImage: string;
  artifactLabel: string;
  dockerfile: string;
  extraSteps: string;
  hierarchies: string[];
  serviceReload: boolean;
  signingLabel: string;
  submitErr: string | null;
}) {
  const sourceLabel: Record<SourceMode, string> = {
    artifact: "From artifact",
    image: "Base image",
    dockerfile: "Dockerfile",
  };
  const isSysext = type === "sysext";
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Review</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-2 text-sm">
          <KV k="Name" v={name} />
          <KV k="Type" v={type} />
          <KV k="Arch" v={arch} />
          <KV k="Version" v={version} />
          <KV k="Source" v={sourceLabel[sourceMode]} />
          {sourceMode === "artifact" && <KV k="Artifact" v={artifactLabel} />}
          {sourceMode === "image" && <KV k="Base image" v={baseImage} />}
          {sourceMode === "dockerfile" && (
            <KV k="Dockerfile" v={`${dockerfile.length} bytes`} />
          )}
          {sourceMode === "artifact" && (
            <KV k="Extra steps" v={extraSteps ? `${extraSteps.length} bytes` : "None"} />
          )}
          <KV
            k="Hierarchies"
            v={
              isSysext
                ? ["/usr", ...hierarchies].join(", ")
                : "/etc (confext)"
            }
          />
          <KV k="Signing" v={signingLabel} />
          <KV
            k="Service reload"
            v={isSysext ? (serviceReload ? "Yes" : "No") : "Not used for confext"}
          />
        </CardContent>
      </Card>

      {submitErr && (
        <p role="alert" className="text-sm text-danger-foreground">
          {submitErr}
        </p>
      )}
    </div>
  );
}

function KV({ k, v }: { k: string; v: string }) {
  return (
    <div className="grid grid-cols-[160px_1fr] gap-2">
      <span className="text-muted-foreground">{k}</span>
      <span className="font-mono">{v}</span>
    </div>
  );
}
