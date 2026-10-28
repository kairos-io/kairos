import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

import { CommandDialog, COMMANDS } from "@/components/CommandDialog";
import type { CommandDef } from "@/components/CommandDialog";
import { PHONEHOME_ALL_COMMANDS } from "@/lib/buildConfig";

// The dialog's action cards are the only place a command name is minted for a
// node, and the node refuses any name that is not in its baked
// phonehome.allowed_commands -- which the Artifact Builder fills from
// PHONEHOME_ALL_COMMANDS. The two lists have to be the same vocabulary, so
// every card is driven to submit here and its name checked against that list.
// A name only this file knows about reaches the node as
// "command %q is not permitted by the phonehome policy" and can never be
// opted into, because the picker cannot render it.
//
// The cases come from COMMANDS itself rather than a list kept here, so a card
// added to the dialog is covered without touching this file. A new card that
// needs an input is the one thing still to add below: without an entry in
// PREPARE its Send button stays disabled and its case fails.

vi.mock("@/api/artifacts", () => ({
  listArtifacts: vi.fn().mockResolvedValue([]),
  resolveBundle: vi.fn().mockResolvedValue([]),
}));

// Fill whatever the card's Send button needs before it becomes enabled.
// Keyed by command key; a card that submits with an empty form needs no entry.
const PREPARE: Partial<Record<CommandDef["key"], () => void>> = {
  upgrade: () => fillImage(),
  "upgrade-recovery": () => fillImage(),
  "apply-cloud-config": () => {
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "#cloud-config\nstages: {}\n" },
    });
  },
  exec: () => {
    fireEvent.change(screen.getByPlaceholderText("e.g. uname -a"), {
      target: { value: "uname -a" },
    });
  },
};

function fillImage() {
  fireEvent.change(screen.getByPlaceholderText(/quay.io|ghcr.io|image/i), {
    target: { value: "quay.io/kairos/ubuntu:latest" },
  });
}

describe("CommandDialog command names", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  for (const c of COMMANDS) {
    it(`submits a name the agent accepts for "${c.label}"`, async () => {
      const onSubmit = vi.fn();
      render(
        <CommandDialog open onOpenChange={() => {}} onSubmit={onSubmit} />
      );

      fireEvent.click(await screen.findByText(c.label));
      const prepare = PREPARE[c.key];
      if (prepare) {
        await waitFor(() => prepare());
      }

      // The card's own verb, so a renamed button cannot quietly match a
      // sibling card's Send button and submit the wrong command.
      const send = screen
        .getAllByRole("button")
        .find((b) => (b.textContent ?? "").trim() === c.verb);
      expect(send, `send button for "${c.verb}"`).toBeTruthy();
      expect(send, `"${c.verb}" is enabled after PREPARE`).not.toBeDisabled();
      fireEvent.click(send!);

      await waitFor(() => expect(onSubmit).toHaveBeenCalled());
      const sent = onSubmit.mock.calls[0][0] as string;
      expect(PHONEHOME_ALL_COMMANDS).toContain(sent);
    });
  }
});
