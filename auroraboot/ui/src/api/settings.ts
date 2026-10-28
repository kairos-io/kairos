import { apiFetch } from "./client";

export interface RegistrationToken {
  registrationToken: string;
}

export function getRegistrationToken(): Promise<RegistrationToken> {
  return apiFetch<RegistrationToken>("/api/v1/settings/registration-token");
}

export function rotateRegistrationToken(): Promise<RegistrationToken> {
  return apiFetch<RegistrationToken>(
    "/api/v1/settings/registration-token/rotate",
    { method: "POST" }
  );
}

// ImageSourceSettings mirrors the server's GET /api/v1/settings/image-source
// response: the runtime-configurable image source for Redfish deploys.
export interface ImageSourceSettings {
  // defaultImageURL is the global default URL the BMC pulls the ISO from (model
  // a) when neither a per-deploy nor a per-BMC override is set.
  defaultImageURL: string;
  localServe: {
    // configured reports whether a local ISO-serve listener was set at launch
    // (--redfish-serve-addr). Without it, local serving cannot be enabled.
    configured: boolean;
    // enabled is the runtime gate for serving the built artifact ISO (model b).
    enabled: boolean;
    // advertisedURL is the base URL the BMC reaches the served ISO at.
    advertisedURL: string;
    // usesTLS reports whether the listener serves over HTTPS.
    usesTLS: boolean;
  };
}

// UpdateImageSourcePayload is the PUT body. Omitted fields are left unchanged; an
// explicit empty string clears a URL value.
export interface UpdateImageSourcePayload {
  defaultImageURL?: string;
  localServeEnabled?: boolean;
  localServeAdvertisedURL?: string;
}

export function getImageSourceSettings(): Promise<ImageSourceSettings> {
  return apiFetch<ImageSourceSettings>("/api/v1/settings/image-source");
}

export function updateImageSourceSettings(
  payload: UpdateImageSourcePayload
): Promise<ImageSourceSettings> {
  return apiFetch<ImageSourceSettings>("/api/v1/settings/image-source", {
    method: "PUT",
    body: JSON.stringify(payload),
  });
}

// ExtensionCatalogSettings mirrors GET /api/v1/settings/extension-catalogs:
// the catalogs the Artifact Builder offers for system extensions.
export interface ExtensionCatalogSettings {
  // launch are the catalogs given with `web --extensions-catalog`. Read-only.
  launch: string[];
  // saved are the catalogs an operator added in Settings.
  saved: string[];
}

export async function getExtensionCatalogSettings(): Promise<ExtensionCatalogSettings> {
  const resp = await apiFetch<Partial<ExtensionCatalogSettings>>(
    "/api/v1/settings/extension-catalogs"
  );
  return {
    launch: Array.isArray(resp?.launch) ? resp.launch : [],
    saved: Array.isArray(resp?.saved) ? resp.saved : [],
  };
}

// updateExtensionCatalogSettings replaces the saved list. The launch
// catalogs are not affected.
export function updateExtensionCatalogSettings(
  saved: string[]
): Promise<ExtensionCatalogSettings> {
  return apiFetch<ExtensionCatalogSettings>("/api/v1/settings/extension-catalogs", {
    method: "PUT",
    body: JSON.stringify({ saved }),
  });
}
