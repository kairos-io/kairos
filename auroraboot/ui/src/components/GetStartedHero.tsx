import { useNavigate } from "react-router";
import { Button } from "@/components/ui/button";
import {
  Package,
  Rocket,
  Server,
  ArrowRight,
  Download,
  Check,
} from "lucide-react";
import { PRODUCT_NAME } from "@/lib/branding";

// GetStartedHero is the first-run experience on the Dashboard. It only
// appears when the user has no artifacts AND no nodes, i.e. a brand new
// auroraboot instance. The goal is to answer "what do I do first" in one
// glance: a vertical three-step flow (Build → Deploy → Manage) with a
// single active CTA on step one, and a discreet side-door for users who
// already have Kairos nodes running somewhere else and just want to
// import them.
//
// Design intent: resist AI-slop (no stat cards, no gradient title, no
// glowing glass). The hero is warm but quiet: real brand mark, a
// product-voice welcome, and a numbered vertical flow whose typography
// and ordering do the talking.

interface StepDef {
  n: number;
  title: string;
  description: string;
  icon: typeof Package;
  cta?: { label: string; to: string };
  active?: boolean;
}

export function GetStartedHero() {
  const navigate = useNavigate();

  const steps: StepDef[] = [
    {
      n: 1,
      title: "Build your first artifact",
      description:
        `Pick a base OS, choose a target architecture and outputs (ISO, UKI, raw disk...), and let ${PRODUCT_NAME} produce a bootable image. Templates for Ubuntu, Fedora and more are one click away.`,
      icon: Package,
      cta: { label: "Start a build", to: "/artifacts/new" },
      active: true,
    },
    {
      n: 2,
      title: "Deploy it to a node",
      description:
        `Flash the ISO to a USB stick, point a Redfish BMC at the image, or serve it over the built-in netboot server. Nodes auto-register with ${PRODUCT_NAME} on first boot.`,
      icon: Rocket,
    },
    {
      n: 3,
      title: "Manage your fleet",
      description:
        "Registered nodes show up here. Organize them into groups, roll out upgrades, push cloud-config, and watch their health in real time.",
      icon: Server,
    },
  ];

  const stepDelayClasses = [
    "animate-fade-up-delay-1",
    "animate-fade-up-delay-2",
    "animate-fade-up-delay-3",
  ];

  return (
    <div className="mx-auto max-w-3xl py-8">
      {/* Brand mark + welcome */}
      <div className="text-center mb-12 animate-fade-up">
        <div className="inline-block rounded-2xl bg-navy p-6 mb-6">
          <img
            src="/kairos-wordmark.png"
            alt="Kairos by SpectroCloud"
            className="h-20 w-auto"
            draggable={false}
          />
        </div>
        <h1 className="text-3xl font-bold tracking-tight mb-2">
          Welcome to {PRODUCT_NAME}
        </h1>
        <p className="text-muted-foreground max-w-xl mx-auto">
          Build Kairos OS images, deploy them to bare metal or virtual machines, and
          manage the whole fleet from one place. Let's get your first node running.
        </p>
      </div>

      {/* Vertical numbered flow */}
      <ol className="space-y-4">
        {steps.map((step, idx) => {
          const Icon = step.icon;
          const delayClass = stepDelayClasses[idx] ?? "animate-fade-up";
          return (
            <li
              key={step.n}
              className={
                "relative flex gap-5 rounded-xl border p-5 transition-colors " +
                delayClass +
                " " +
                (step.active
                  ? "border-primary/40 bg-primary-soft"
                  : "border-border bg-card/60 opacity-75")
              }
            >
              {/* Step number badge */}
              <div
                className={
                  "flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-sm font-semibold " +
                  (step.active
                    ? "bg-primary text-primary-foreground"
                    : "bg-muted text-muted-foreground")
                }
              >
                {step.n}
              </div>

              {/* Step body */}
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-1.5">
                  <Icon
                    className={
                      "h-4 w-4 " +
                      (step.active ? "text-primary" : "text-muted-foreground")
                    }
                  />
                  <h3 className="font-semibold text-sm">{step.title}</h3>
                  {!step.active && (
                    <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-medium">
                      after step {step.n - 1}
                    </span>
                  )}
                </div>
                <p className="text-sm text-muted-foreground leading-relaxed">
                  {step.description}
                </p>

                {step.cta && (
                  <Button
                    size="sm"
                    className="mt-4 focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:ring-offset-2"
                    onClick={() => navigate(step.cta!.to)}
                  >
                    {step.cta.label}
                    <ArrowRight className="h-4 w-4 ml-2" />
                  </Button>
                )}
              </div>
            </li>
          );
        })}
      </ol>

      {/* Side door for users who already have a fleet */}
      <div className="mt-10 pt-6 border-t text-center animate-fade-up-delay-3">
        <p className="text-sm text-muted-foreground mb-3">
          Already have Kairos nodes running elsewhere?
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => navigate("/import")}
          className="focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <Download className="h-4 w-4 mr-2" />
          Import existing nodes
        </Button>
      </div>

      {/* Reassuring footer — this wizard only appears on empty instances. */}
      <p className="mt-10 text-center text-xs text-muted-foreground/70">
        <Check className="inline h-3 w-3 mr-1" />
        This flow disappears once you have your first build or node.
      </p>
    </div>
  );
}
