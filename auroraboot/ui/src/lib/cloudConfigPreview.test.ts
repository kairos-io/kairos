import { describe, it, expect } from "vitest";
import { parse } from "yaml";

import { buildCloudConfigPreview, stripPhonehome } from "./cloudConfigPreview";
import { PHONEHOME_SAFE_DEFAULTS } from "./buildConfig";

const base: Parameters<typeof buildCloudConfigPreview>[0] = {
  autoInstall: true,
  registerAuroraBoot: true,
  groupName: "production",
  allowedCommands: [...PHONEHOME_SAFE_DEFAULTS],
  variant: "standard",
  kubernetesDistro: "k3s",
  kubernetesEnabled: true,
  userMode: "default",
  username: "",
  password: "",
  sshKeys: "",
  extraYAML: "",
};

function docBody(yaml: string): Record<string, unknown> {
  const body = yaml.replace(/^#cloud-config\n?/, "");
  const parsed = parse(body);
  expect(parsed).toBeTypeOf("object");
  return parsed as Record<string, unknown>;
}

describe("buildCloudConfigPreview", () => {
  it("emits k3s.enabled for standard variant", () => {
    const doc = docBody(buildCloudConfigPreview(base));
    expect(doc.k3s).toEqual({ enabled: true });
  });

  it("merges extra k3s config instead of duplicating the top-level key", () => {
    const yaml = buildCloudConfigPreview({
      ...base,
      extraYAML: "k3s:\n  enabled: true\n  cluster-cidr: 10.42.0.0/16",
    });
    expect(yaml.match(/^k3s:/gm)?.length ?? 0).toBeLessThanOrEqual(1);

    const doc = docBody(yaml);
    expect(doc.k3s).toEqual({
      enabled: true,
      "cluster-cidr": "10.42.0.0/16",
    });
  });

  it("lets extra YAML override kubernetes enabled when present", () => {
    const doc = docBody(
      buildCloudConfigPreview({
        ...base,
        kubernetesEnabled: true,
        extraYAML: "k3s:\n  enabled: false",
      }),
    );
    expect(doc.k3s).toEqual({ enabled: false });
  });

  it("omits k3s for core variant", () => {
    const doc = docBody(
      buildCloudConfigPreview({
        ...base,
        variant: "core",
        kubernetesDistro: "k3s",
      }),
    );
    expect(doc.k3s).toBeUndefined();
  });

  it("merges extra stages under the canonical stages key", () => {
    const doc = docBody(
      buildCloudConfigPreview({
        ...base,
        extraYAML: "stages:\n  boot:\n    - commands:\n        - echo hi",
      }),
    );
    const stages = doc.stages as Record<string, unknown>;
    expect(stages.initramfs).toBeDefined();
    expect(stages.boot).toBeDefined();
  });

  it("ignores prototype-pollution keys in extra YAML", () => {
    const protoKey = "__proto__";
    buildCloudConfigPreview({
      ...base,
      extraYAML: `${protoKey}:\n  polluted: true\nk3s:\n  enabled: false`,
    });
    expect(Object.prototype).not.toHaveProperty("polluted");
  });

  it("falls back to placeholders when no real registration values are given", () => {
    const doc = docBody(buildCloudConfigPreview(base));
    const phonehome = doc.phonehome as Record<string, unknown>;
    expect(phonehome.url).toBe("<server-url>");
    expect(phonehome.registration_token).toBe("<token>");
  });

  it("uses the real url and token when provided", () => {
    const doc = docBody(
      buildCloudConfigPreview({
        ...base,
        registrationUrl: "https://auroraboot.example.com",
        registrationToken: "real-token-value",
      }),
    );
    const phonehome = doc.phonehome as Record<string, unknown>;
    expect(phonehome.url).toBe("https://auroraboot.example.com");
    expect(phonehome.registration_token).toBe("real-token-value");
  });

  it("keeps showing real values after the extra YAML is edited", () => {
    const doc = docBody(
      buildCloudConfigPreview({
        ...base,
        extraYAML: "k3s:\n  enabled: false",
        registrationUrl: "https://auroraboot.example.com",
        registrationToken: "real-token-value",
      }),
    );
    const phonehome = doc.phonehome as Record<string, unknown>;
    expect(phonehome.url).toBe("https://auroraboot.example.com");
    expect(phonehome.registration_token).toBe("real-token-value");
  });
});

describe("stripPhonehome", () => {
  it("removes the phonehome block from a baked cloud config", () => {
    const baked =
      "#cloud-config\nphonehome:\n  url: https://real.example.com\n  registration_token: real-token\nk3s:\n  enabled: true\n";
    const stripped = stripPhonehome(baked);
    expect(stripped).not.toMatch(/phonehome/);
    expect(stripped).not.toMatch(/real-token/);
    expect(parse(stripped.replace(/^#cloud-config\n?/, ""))).toEqual({
      k3s: { enabled: true },
    });
  });

  it("leaves config without a phonehome block untouched", () => {
    const config = "#cloud-config\nk3s:\n  enabled: true\n";
    expect(stripPhonehome(config)).toBe(config);
  });

  it("returns the input unchanged when it is not valid YAML", () => {
    const invalid = "not: valid: yaml: [";
    expect(stripPhonehome(invalid)).toBe(invalid);
  });

  it("returns empty input unchanged", () => {
    expect(stripPhonehome("")).toBe("");
  });
});
