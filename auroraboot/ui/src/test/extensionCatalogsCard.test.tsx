import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";

vi.mock("@/api/settings", () => ({
  getExtensionCatalogSettings: vi.fn(),
  updateExtensionCatalogSettings: vi.fn(),
}));

import { ExtensionCatalogsCard } from "@/components/settings/ExtensionCatalogsCard";
import { getExtensionCatalogSettings, updateExtensionCatalogSettings } from "@/api/settings";
import {
  DEFAULT_EXTENSIONS_CATALOG,
  extensionCatalogChoices,
  isHadronBaseImage,
} from "@/lib/catalogExtensions";

const getMock = getExtensionCatalogSettings as unknown as ReturnType<typeof vi.fn>;
const putMock = updateExtensionCatalogSettings as unknown as ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  putMock.mockImplementation(async (saved: string[]) => ({ launch: [], saved }));
});

describe("extension catalog helpers", () => {
  it("recognizes Hadron base images only", () => {
    expect(isHadronBaseImage("ghcr.io/kairos-io/hadron:v0.5.3")).toBe(true);
    expect(isHadronBaseImage("ghcr.io/kairos-io/hadron@sha256:aa")).toBe(true);
    expect(isHadronBaseImage("ghcr.io/kairos-io/hadron-layers:v1")).toBe(false);
    expect(isHadronBaseImage("ubuntu:24.04")).toBe(false);
  });

  it("puts the default catalog first for Hadron only, without duplicates", () => {
    const mine = "https://example.test/releases.json";
    expect(extensionCatalogChoices(true, [mine, DEFAULT_EXTENSIONS_CATALOG])).toEqual([
      DEFAULT_EXTENSIONS_CATALOG,
      mine,
    ]);
    expect(extensionCatalogChoices(false, [mine, mine])).toEqual([mine]);
    expect(extensionCatalogChoices(false, [])).toEqual([]);
  });
});

describe("ExtensionCatalogsCard", () => {
  it("lists launch catalogs as read-only and saved ones as removable", async () => {
    getMock.mockResolvedValue({
      launch: ["https://launch.example.test/releases.json"],
      saved: ["https://saved.example.test/releases.json"],
    });
    render(<ExtensionCatalogsCard />);

    expect(await screen.findByText("https://launch.example.test/releases.json")).toBeInTheDocument();
    expect(screen.getByText("launch flag")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Remove https://launch.example.test/releases.json" }),
    ).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "Remove https://saved.example.test/releases.json" }),
    );
    await waitFor(() => expect(putMock).toHaveBeenCalledWith([]));
  });

  it("adds a typed catalog after the saved ones", async () => {
    getMock.mockResolvedValue({ launch: [], saved: ["https://a.example.test/c.json"] });
    render(<ExtensionCatalogsCard />);
    await screen.findByText("https://a.example.test/c.json");

    fireEvent.change(screen.getByLabelText("New extension catalog URL"), {
      target: { value: " https://b.example.test/c.json " },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Add$/ }));

    await waitFor(() =>
      expect(putMock).toHaveBeenCalledWith([
        "https://a.example.test/c.json",
        "https://b.example.test/c.json",
      ]),
    );
    expect(await screen.findByText("https://b.example.test/c.json")).toBeInTheDocument();
  });

  it("proposes the suggested catalogs that are not configured yet", async () => {
    getMock.mockResolvedValue({ launch: [], saved: [] });
    render(<ExtensionCatalogsCard />);

    fireEvent.click(await screen.findByRole("button", { name: "Add Kairos hadron-layers" }));
    await waitFor(() => expect(putMock).toHaveBeenCalledWith([DEFAULT_EXTENSIONS_CATALOG]));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Add Kairos hadron-layers" })).not.toBeInTheDocument(),
    );
  });

  it("shows the server's error when a catalog is rejected", async () => {
    getMock.mockResolvedValue({ launch: [], saved: [] });
    putMock.mockRejectedValue(new Error("invalid extension catalog \"/srv/c.json\""));
    render(<ExtensionCatalogsCard />);

    fireEvent.change(await screen.findByLabelText("New extension catalog URL"), {
      target: { value: "/srv/c.json" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Add$/ }));

    expect(await screen.findByText(/invalid extension catalog/)).toBeInTheDocument();
    expect(screen.getByLabelText("New extension catalog URL")).toHaveValue("/srv/c.json");
  });
});
