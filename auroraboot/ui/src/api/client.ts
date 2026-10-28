/**
 * ApiError carries the HTTP status plus the parsed response body so callers
 * can render structured errors (e.g. a 409 with the list of artifacts that
 * still reference an extension) instead of dumping raw JSON at the user.
 */
export class ApiError extends Error {
  status: number;
  body: unknown;
  raw: string;
  constructor(status: number, body: unknown, raw: string) {
    const detail =
      (body &&
        typeof body === "object" &&
        "error" in body &&
        (body as { error?: unknown }).error) ||
      raw ||
      `HTTP ${status}`;
    super(String(detail));
    this.name = "ApiError";
    this.status = status;
    this.body = body;
    this.raw = raw;
  }
}

// apiErrorFrom builds the ApiError for a failed response, reading the
// server's {"error": "..."} body when there is one.
export async function apiErrorFrom(res: Response): Promise<ApiError> {
  const text = await res.text();
  let body: unknown = text;
  try {
    body = JSON.parse(text);
  } catch {
    /* keep body as raw text */
  }
  return new ApiError(res.status, body, text);
}

const LEGACY_TOKEN_KEY = "auroraboot_token";

// clearLegacyToken removes any admin password stored under the legacy
// localStorage key.
export function clearLegacyToken() {
  try {
    localStorage.removeItem(LEGACY_TOKEN_KEY);
  } catch {
    /* storage unavailable */
  }
}

// The browser holds the session in an HttpOnly cookie the server sets at
// login; same-origin requests carry it, and no script can read it.
export type LoginResult = { ok: true } | { ok: false; status: number };

export async function login(password: string): Promise<LoginResult> {
  const res = await fetch("/api/v1/session", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
  });
  return res.ok ? { ok: true } : { ok: false, status: res.status };
}

// logout reports whether the server ended the session. Only the server can
// clear the HttpOnly cookie, so a failed request leaves the browser signed in.
export async function logout(): Promise<boolean> {
  try {
    const res = await fetch("/api/v1/session", { method: "DELETE" });
    return res.ok;
  } catch {
    return false;
  }
}

// hasSession reports whether the browser holds a live session. It does not
// redirect on 401, unlike apiFetch.
export async function hasSession(): Promise<boolean> {
  try {
    const res = await fetch("/api/v1/session");
    return res.ok;
  } catch {
    return false;
  }
}

export async function apiFetch<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(options.headers as Record<string, string>),
  };

  const res = await fetch(path, {
    ...options,
    headers,
  });

  if (res.status === 401) {
    window.location.href = "/login";
    throw new Error("Unauthorized");
  }

  if (!res.ok) {
    throw await apiErrorFrom(res);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  return res.json();
}

export async function apiFetchText(
  path: string,
  options: RequestInit = {}
): Promise<string> {
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
  };

  const res = await fetch(path, { ...options, headers });

  if (res.status === 401) {
    window.location.href = "/login";
    throw new Error("Unauthorized");
  }

  if (!res.ok) {
    throw new Error(`API error ${res.status}`);
  }

  return res.text();
}
