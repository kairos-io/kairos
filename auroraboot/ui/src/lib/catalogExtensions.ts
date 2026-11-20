// Reading and picking system extensions from a published catalog, for the
// Artifact Builder's build-time selection. The catalog document is the same
// index the agent reads on a node and the same one the Hadron composer reads
// for its software layers; this module additionally reads each version's
// `sysext` map, because only that says which architectures an extension is
// actually published for.

// DEFAULT_EXTENSIONS_CATALOG is the catalog a build resolves names against
// when the operator does not name one. It mirrors extensions.DefaultCatalog
// in pkg/extensions: the UI pre-fills it so the operator can see and override
// what is being read, but a build that did not override it sends no catalog
// at all and lets the server stay authoritative.
export const DEFAULT_EXTENSIONS_CATALOG =
  "https://kairos-io.github.io/hadron-layers/releases.json";

// The default catalog publishes extensions built for Hadron only. Other
// flavors get the catalogs the operator configured (`web
// --extensions-catalog` or Settings), and none by default.
const HADRON_IMAGE_REPO = "ghcr.io/kairos-io/hadron";

// isHadronBaseImage reports whether a base image ref is a Hadron image, so a
// Custom build from a Hadron image gets the Hadron catalog too.
export function isHadronBaseImage(ref: string): boolean {
  const trimmed = ref.trim();
  return (
    trimmed === HADRON_IMAGE_REPO ||
    trimmed.startsWith(`${HADRON_IMAGE_REPO}:`) ||
    trimmed.startsWith(`${HADRON_IMAGE_REPO}@`)
  );
}

export type SuggestedExtensionCatalog = {
  name: string;
  url: string;
  description: string;
};

// SUGGESTED_EXTENSION_CATALOGS are the catalogs the UI proposes when no
// catalog is configured for a non-Hadron flavor, and in Settings. Add a
// published catalog here to offer it with one click.
export const SUGGESTED_EXTENSION_CATALOGS: SuggestedExtensionCatalog[] = [
  {
    name: "Kairos hadron-layers",
    url: DEFAULT_EXTENSIONS_CATALOG,
    description:
      "Extensions built for Hadron. They can fail to load on other flavors.",
  },
];

// extensionCatalogChoices lists the catalogs the builder offers: the Hadron
// default first for a Hadron build, then the configured ones, without
// duplicates.
export function extensionCatalogChoices(hadron: boolean, configured: string[]): string[] {
  const all = hadron ? [DEFAULT_EXTENSIONS_CATALOG, ...configured] : configured;
  return all.filter((url, index) => url !== "" && all.indexOf(url) === index);
}

export type CatalogExtensionVersion = { version: string; archs: string[] };

export type CatalogExtensionItem = {
  name: string;
  title?: string;
  description?: string;
  latest?: string;
  versions: CatalogExtensionVersion[];
};

export async function fetchCatalogExtensions(url: string): Promise<CatalogExtensionItem[]> {
  const resp = await fetch(url);
  if (!resp.ok) throw new Error(`extension catalog: ${resp.status}`);
  const payload = (await resp.json()) as {
    layers?: Array<{
      name?: string;
      title?: string;
      description?: string;
      latest?: string;
      tags?: Array<{ tag?: string; sysext?: Record<string, unknown> }>;
    }>;
  };
  const items: CatalogExtensionItem[] = [];
  for (const layer of payload.layers ?? []) {
    if (typeof layer.name !== "string" || layer.name === "") continue;
    items.push({
      name: layer.name,
      title: layer.title,
      description: layer.description,
      latest: layer.latest,
      versions: (layer.tags ?? [])
        .filter((t): t is { tag: string; sysext?: Record<string, unknown> } =>
          typeof t.tag === "string" && t.tag !== "",
        )
        .map((t) => ({ version: t.tag, archs: Object.keys(t.sysext ?? {}) })),
    });
  }
  items.sort((a, b) => (a.name < b.name ? -1 : 1));
  return items;
}

// catalogExtensionsForArch drops what cannot be materialized for the build:
// an extension with no artifact for the target architecture would fail the
// build after the source image has already been pulled, so the picker never
// offers it.
export function catalogExtensionsForArch(
  items: CatalogExtensionItem[],
  arch: string,
): CatalogExtensionItem[] {
  return items
    .map((item) => ({
      ...item,
      versions: item.versions.filter((v) => v.archs.includes(arch)),
    }))
    .filter((item) => item.versions.length > 0);
}

// LATEST_VERSION is the "no version" selection: the request carries the bare
// name and the catalog resolves the newest published version at build time.
export const LATEST_VERSION = "__latest__";

// serializeExtensionSelection turns the picker state into the name@version
// list the API takes, in the given order so a rebuild sends the same request.
export function serializeExtensionSelection(
  selection: Record<string, string>,
  order: string[],
): string[] {
  return order
    .filter((name) => selection[name] !== undefined)
    .map((name) =>
      selection[name] === LATEST_VERSION ? name : `${name}@${selection[name]}`,
    );
}

// parseExtensionSelection is the inverse, for rehydrating a cloned build.
export function parseExtensionSelection(values: string[]): Record<string, string> {
  const selection: Record<string, string> = {};
  for (const value of values) {
    const at = value.indexOf("@");
    if (at > 0) {
      selection[value.slice(0, at)] = value.slice(at + 1);
    } else if (value !== "") {
      selection[value] = LATEST_VERSION;
    }
  }
  return selection;
}
