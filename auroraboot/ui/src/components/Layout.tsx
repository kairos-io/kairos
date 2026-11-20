import { useEffect, useState } from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { NavLink, Outlet, useNavigate } from "react-router";
import {
  LayoutDashboard,
  FolderTree,
  ListChecks,
  Server,
  Package,
  Puzzle,
  Rocket,
  ServerCog,
  KeyRound,
  Download,
  Settings,
  LogOut,
  BookOpen,
  ExternalLink,
  Menu,
  PanelLeftClose,
  PanelLeftOpen,
  X,
  type LucideIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { logout } from "@/api/client";
import { getSystemBuilder, type SystemBuilder } from "@/api/system";
import { cn } from "@/lib/utils";
import { KairosLogo } from "@/components/KairosLogo";
import { Toaster } from "@/components/ui/toaster";
import { toast } from "@/hooks/useToast";
import { ThemeToggle } from "@/components/ThemeToggle";
import { PRODUCT_NAME } from "@/lib/branding";

// NavItem is either a client-side route (the common case) or an
// `external: true` link that renders as a plain <a target="_blank">
// — used for server-rendered pages like /api/docs that aren't part of
// the React router.
type NavItem = {
  to: string;
  icon: LucideIcon;
  label: string;
  external?: boolean;
};
type NavSection = { label?: string; items: NavItem[] };

const navSections: NavSection[] = [
  {
    items: [
      { to: "/", icon: LayoutDashboard, label: "Dashboard" },
    ],
  },
  {
    label: "Fleet",
    items: [
      { to: "/nodes", icon: Server, label: "Nodes" },
      { to: "/groups", icon: FolderTree, label: "Groups" },
      { to: "/batches", icon: ListChecks, label: "Batches" },
    ],
  },
  {
    label: "Build",
    items: [
      { to: "/artifacts", icon: Package, label: "Artifacts" },
      { to: "/extensions", icon: Puzzle, label: "Extensions" },
    ],
  },
  {
    label: "Deploy",
    items: [
      { to: "/bmc", icon: ServerCog, label: "BMC Registration" },
      { to: "/deployments", icon: Rocket, label: "Deployments" },
      { to: "/import", icon: Download, label: "Import" },
    ],
  },
  {
    label: "Admin",
    items: [
      { to: "/certificates", icon: KeyRound, label: "Certificates" },
      { to: "/settings", icon: Settings, label: "Settings" },
    ],
  },
  {
    label: "Developer",
    items: [
      { to: "/api/docs", icon: BookOpen, label: "API docs", external: true },
    ],
  },
];

// BuilderChip is a small legend at the sidebar footer telling the operator
// which build backend is active. Deliberately unobtrusive - most users do
// not care where their builds run, but when something goes wrong (or a
// cluster is misconfigured) it is useful to confirm at a glance. On the
// operator backend the cluster URL is available on hover via the
// browser's native tooltip so we do not stretch the sidebar width.
function BuilderChip({ info, className }: { info: SystemBuilder; className?: string }) {
  const rows =
    info.backend === "operator"
      ? [
          ["Backend:", "Operator"],
          ["Namespace:", info.namespace || "default"],
        ]
      : [["Backend:", "Local"]];
  return (
    <div
      className={cn(
        "grid-cols-2 gap-x-1 px-4 pb-2 text-[10px] leading-tight text-sidebar-fg/40",
        className ?? "grid",
      )}
      title={info.backend === "operator" && info.cluster ? info.cluster : undefined}
    >
      {rows.map(([label, value]) => (
        <span key={label} className="contents">
          <span className="text-right">{label}</span>
          <span className="text-left">{value}</span>
        </span>
      ))}
    </div>
  );
}

// SIDEBAR_KEY stores the sidebar state the user chose on a desktop-sized
// screen. When it is absent the sidebar follows the screen width: a full
// sidebar at lg and wider, an icon rail between md and lg.
export const SIDEBAR_KEY = "auroraboot_sidebar";

type SidebarPref = "full" | "rail";
// "auto" is the state when nothing is stored: responsive classes decide.
type SidebarMode = SidebarPref | "auto";

function readSidebarPref(): SidebarMode {
  try {
    const v = window.localStorage.getItem(SIDEBAR_KEY);
    return v === "full" || v === "rail" ? v : "auto";
  } catch {
    return "auto";
  }
}

function writeSidebarPref(v: SidebarPref) {
  try {
    window.localStorage.setItem(SIDEBAR_KEY, v);
  } catch {
    // Storage can be blocked; the choice then lasts for this page only.
  }
}

// Class helpers per mode. "full only" content shows on the full sidebar,
// "rail only" content shows on the icon rail.
const labelCls: Record<SidebarMode, string> = {
  full: "",
  rail: "sr-only",
  auto: "sr-only lg:not-sr-only",
};
const fullOnly: Record<SidebarMode, string> = {
  full: "",
  rail: "hidden",
  auto: "hidden lg:block",
};
const railOnly: Record<SidebarMode, string> = {
  full: "hidden",
  rail: "",
  auto: "lg:hidden",
};
const linkLayout: Record<SidebarMode, string> = {
  full: "justify-start px-3",
  rail: "justify-center px-0",
  auto: "justify-center px-0 lg:justify-start lg:px-3",
};

function SidebarNav({
  mode,
  onNavigate,
}: {
  mode: SidebarMode;
  onNavigate?: () => void;
}) {
  const railTitle = (label: string) => (mode === "full" ? undefined : label);
  return (
    <nav aria-label="Main" className="flex-1 overflow-y-auto overflow-x-hidden p-3">
      {navSections.map((section, si) => (
        <div key={si}>
          {section.label && (
            <>
              <span
                className={cn(
                  "text-[10px] uppercase tracking-wider text-sidebar-fg/40 px-3 pt-4 pb-1 block",
                  labelCls[mode],
                )}
              >
                {section.label}
              </span>
              <div className={cn("mx-2 my-2 border-t border-white/10", railOnly[mode])} />
            </>
          )}
          <div className="space-y-1">
            {section.items.map((item) =>
              item.external ? (
                <a
                  key={item.to}
                  href={item.to}
                  target="_blank"
                  rel="noopener noreferrer"
                  aria-label={item.label}
                  title={railTitle(item.label)}
                  onClick={onNavigate}
                  className={cn(
                    "flex items-center gap-3 rounded-md py-2 text-sm font-medium transition-colors text-sidebar-fg opacity-80 hover:opacity-100 hover:bg-sidebar-muted",
                    linkLayout[mode],
                  )}
                >
                  <item.icon className="h-4 w-4 shrink-0" />
                  <span className={cn("flex-1", labelCls[mode])}>{item.label}</span>
                  <ExternalLink className={cn("h-3 w-3 opacity-60", fullOnly[mode])} />
                </a>
              ) : (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === "/"}
                  aria-label={item.label}
                  title={railTitle(item.label)}
                  onClick={onNavigate}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-3 rounded-md py-2 text-sm font-medium transition-colors",
                      linkLayout[mode],
                      isActive
                        ? "bg-sidebar-accent text-white"
                        : "text-sidebar-fg opacity-80 hover:opacity-100 hover:bg-sidebar-muted",
                    )
                  }
                >
                  <item.icon className="h-4 w-4 shrink-0" />
                  <span className={labelCls[mode]}>{item.label}</span>
                </NavLink>
              ),
            )}
          </div>
        </div>
      ))}
    </nav>
  );
}

// SidebarFooter holds the theme switch and Logout. The rail uses the compact
// theme toggle. In "auto" mode both toggles render and CSS shows one; each
// remounts the other after a change so the hidden one never goes stale.
function SidebarFooter({ mode, onLogout }: { mode: SidebarMode; onLogout: () => void }) {
  const [compactRev, setCompactRev] = useState(0);
  const [fullRev, setFullRev] = useState(0);
  return (
    <div className="space-y-2 p-3 pb-5">
      {mode !== "rail" && (
        <div className={fullOnly[mode]} onClick={() => setCompactRev((r) => r + 1)}>
          <ThemeToggle key={fullRev} />
        </div>
      )}
      {mode !== "full" && (
        <div
          className={cn("flex justify-center", railOnly[mode])}
          onClick={() => setFullRev((r) => r + 1)}
        >
          <ThemeToggle key={compactRev} compact />
        </div>
      )}
      <Button
        variant="ghost"
        aria-label="Logout"
        title={mode === "full" ? undefined : "Logout"}
        className={cn(
          "w-full gap-3 text-sidebar-fg opacity-70 hover:opacity-100 hover:bg-sidebar-muted hover:text-sidebar-fg",
          linkLayout[mode],
        )}
        onClick={onLogout}
      >
        <LogOut className="h-4 w-4 shrink-0" />
        <span className={labelCls[mode]}>Logout</span>
      </Button>
    </div>
  );
}

export function Layout() {
  const navigate = useNavigate();
  const [builder, setBuilder] = useState<SystemBuilder | null>(null);
  const [mode, setMode] = useState<SidebarMode>(() => readSidebarPref());
  const [drawerOpen, setDrawerOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;
    getSystemBuilder()
      .then((info) => {
        if (!cancelled) setBuilder(info);
      })
      .catch(() => {
        // Non-fatal - the badge is decorative; the app functions without it.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function handleLogout() {
    if (await logout()) {
      navigate("/login");
    } else {
      toast("Sign-out failed, you are still signed in", "error");
    }
  }

  function choose(v: SidebarPref) {
    writeSidebarPref(v);
    setMode(v);
  }

  const collapseButton = (className?: string) => (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="Collapse sidebar"
      title="Collapse sidebar"
      className={cn("text-sidebar-fg opacity-70 hover:opacity-100 hover:bg-sidebar-muted hover:text-sidebar-fg", className)}
      onClick={() => choose("rail")}
    >
      <PanelLeftClose className="h-4 w-4" />
    </Button>
  );
  const expandButton = (className?: string) => (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="Expand sidebar"
      title="Expand sidebar"
      className={cn("text-sidebar-fg opacity-70 hover:opacity-100 hover:bg-sidebar-muted hover:text-sidebar-fg", className)}
      onClick={() => choose("full")}
    >
      <PanelLeftOpen className="h-4 w-4" />
    </Button>
  );

  // Width of the desktop sidebar slot. A stored "full" sidebar between md
  // and lg overlays the content: the slot stays 64px and the sidebar is
  // positioned over the page.
  const slotWidth: Record<SidebarMode, string> = {
    full: "md:w-16 lg:w-60",
    rail: "w-16",
    auto: "w-16 lg:w-60",
  };

  return (
    <div className="flex h-screen flex-col md:flex-row">
      {/* Top bar below md */}
      <header className="flex h-14 shrink-0 items-center justify-between bg-sidebar-bg px-4 text-sidebar-fg md:hidden">
        <div className="flex items-center gap-2">
          <KairosLogo className="h-8 w-auto" />
          <span className="font-bold text-base text-white">{PRODUCT_NAME}</span>
        </div>
        <DialogPrimitive.Root open={drawerOpen} onOpenChange={setDrawerOpen}>
          <DialogPrimitive.Trigger asChild>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Open navigation"
              className="text-sidebar-fg hover:bg-sidebar-muted hover:text-sidebar-fg"
            >
              <Menu className="h-5 w-5" />
            </Button>
          </DialogPrimitive.Trigger>
          <DialogPrimitive.Portal>
            <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
            <DialogPrimitive.Content
              aria-describedby={undefined}
              className="fixed inset-y-0 left-0 z-50 flex w-72 max-w-[85vw] flex-col bg-sidebar-bg text-sidebar-fg shadow-lg data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-left data-[state=open]:slide-in-from-left"
            >
              <div className="flex items-center justify-between px-4 pt-4 pb-3">
                <div className="flex items-center gap-2">
                  <KairosLogo className="h-8 w-auto" />
                  <DialogPrimitive.Title className="font-bold text-base text-white">
                    {PRODUCT_NAME}
                  </DialogPrimitive.Title>
                </div>
                <DialogPrimitive.Close asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Close navigation"
                    className="text-sidebar-fg hover:bg-sidebar-muted hover:text-sidebar-fg"
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </DialogPrimitive.Close>
              </div>
              <div className="mx-4 border-t border-white/10" />
              <SidebarNav mode="full" onNavigate={() => setDrawerOpen(false)} />
              {builder && <BuilderChip info={builder} />}
              <div className="mx-4 border-t border-white/10" />
              <SidebarFooter mode="full" onLogout={handleLogout} />
            </DialogPrimitive.Content>
          </DialogPrimitive.Portal>
        </DialogPrimitive.Root>
      </header>

      {/* Desktop sidebar: md and wider */}
      <div className={cn("relative hidden shrink-0 md:flex", slotWidth[mode])}>
        <aside
          className={cn(
            "flex h-full flex-col bg-sidebar-bg text-sidebar-fg",
            mode === "full"
              ? "w-60 md:absolute md:inset-y-0 md:left-0 md:z-40 md:shadow-lg lg:static lg:shadow-none"
              : "w-full",
          )}
        >
          <div className="relative flex flex-col items-center gap-3 px-3 pt-6 pb-4">
            <KairosLogo
              className={cn(
                "w-auto",
                mode === "full" ? "h-14" : mode === "rail" ? "h-8" : "h-8 lg:h-14",
              )}
            />
            <div className={cn("text-center", fullOnly[mode])}>
              <span className="font-bold text-base text-white block leading-tight">{PRODUCT_NAME}</span>
            </div>
            {mode === "full" && collapseButton("absolute right-2 top-2")}
            {mode === "rail" && expandButton()}
            {mode === "auto" && (
              <>
                {collapseButton("absolute right-2 top-2 hidden lg:inline-flex")}
                {expandButton("lg:hidden")}
              </>
            )}
          </div>
          <div className="mx-4 border-t border-white/10" />
          <SidebarNav mode={mode} />
          {builder && mode !== "rail" && (
            <BuilderChip info={builder} className={mode === "auto" ? "hidden lg:grid" : "grid"} />
          )}
          <div className="mx-4 border-t border-white/10" />
          <SidebarFooter mode={mode} onLogout={handleLogout} />
        </aside>
      </div>

      {/* Main content */}
      <main className="min-w-0 flex-1 overflow-auto">
        <div className="p-4 md:p-6 lg:p-8">
          <Outlet />
        </div>
        <Toaster />
      </main>
    </div>
  );
}
