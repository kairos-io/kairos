# UI revamp, piece 1: foundations

Date: 2026-09-29
Status: draft for review
Base: `main` at `ecf65f0`

## Context

A UI audit of the AuroraBoot web UI found broken data on several pages, a button
system that makes every page look loud, no working dark mode, and a layout that
cannot be used on a phone. We compared three redesign directions and chose
direction A ("Clarity") with the group board from direction C. The mockups are
kept outside the repo; the relevant screens are summarized in this document.

The revamp ships in four pieces:

1. **Foundations** (this document): bug fixes, design tokens and button
   variants, theme switching, responsive sidebar.
2. Fleet screens: dashboard, node list, node detail, group board.
3. Build screens: artifact builder, extension builder, artifact detail, deploy
   dialog.
4. Agent metrics: heartbeat metrics block (kairos-io/kairos), storage and API,
   gauges.

Pieces 2 and 3 depend on piece 1: they use its tokens, button variants and
layout shell. Piece 1 changes no page layout. Its goal is that every existing
page shows correct data, has one clear primary action style, works in light and
dark themes, and works at phone width.

## Goals

- The four data bugs listed in section 1 no longer reproduce.
- One set of color tokens drives every brand, neutral and status color. No
  component uses a hard-coded brand hex value.
- The shared `Button` component provides all the variants that pages need, and
  pages use those variants instead of their own color classes.
- A user can select System, Light or Dark theme. The choice persists and there is
  no flash of the wrong theme on load.
- The sidebar collapses to an icon rail on desktop and becomes a drawer below
  768px. No page scrolls horizontally at 390px.

## Non-goals

- New page layouts, summary bands, row selection, the group board, the builder
  steps. These are pieces 2 and 3.
- Moving page actions (for example Delete into a ⋯ menu). Piece 1 only changes
  how buttons look, not where they are. The placement rules in section 3.4 apply
  when pieces 2 and 3 rebuild each page.
- Any agent or heartbeat change.

## 1. Bug fixes

Each fix is independent and gets a regression test that fails before the fix.

### B1. Group node counts are blank

**Symptom.** The Nodes column on the Groups page and "Node Count" on the group
detail page are empty.

**Cause.** The UI reads `group.node_count` (`ui/src/api/groups.ts`), and the Go
client decodes the same field (`pkg/client/types.go`, `NodeCount
json:"node_count"`). The server never sends it: `store.NodeGroup`
(`pkg/store/store.go`) has no count, and `GroupHandler.List` and `Get` return the
row as stored.

**Fix.**

- Add `NodeCount int` to `store.NodeGroup` with the tag
  `json:"node_count" gorm:"-"`. Keep the snake_case name, because the Go client
  and the UI already decode it. It is not stored in the database, and it has no
  `omitempty`, because a count of 0 must be sent.
- Add `NodeCounts(ctx) (map[string]int, error)` to `store.GroupStore`. The gorm
  implementation runs one grouped query:
  `SELECT group_id, COUNT(*) FROM managed_nodes WHERE group_id <> '' GROUP BY group_id`.
- `GroupHandler.List` calls `NodeCounts` once and fills `NodeCount` on every
  group. `GroupHandler.Get` fills it for the one group. If the count query fails,
  the handler returns 500, the same as a failed `List`.
- UI: change `created_at` and `updated_at` in the `Group` interface to
  `createdAt` and `updatedAt`, which is what the API sends.
- Regenerate the OpenAPI spec (`make openapi`).

**Tests.** A handler test: a group with 2 nodes and a group with 0 nodes, and
`List` returns `node_count` 2 and 0. The same for `Get`. A gorm store test for
`NodeCounts` that includes ungrouped nodes. A UI test: the Groups page renders
the count from a mocked response.

### B2. Registration token is empty in Settings and Import

**Symptom.** The token field in Settings is empty. On the Import page, the
install command shows the literal text `<token>`.

**Cause.** `GET /api/v1/settings/registration-token` and `.../rotate` return
`{"registrationToken": ...}` (`pkg/handlers/settings.go`; asserted in
`settings_test.go`). The UI type `RegistrationToken` declares `token`, so
`Settings.tsx` and `Import.tsx` read `t.token`, which is undefined.

**Fix.** Rename the field to `registrationToken` in `ui/src/api/settings.ts` and
update both pages. While `Settings.tsx` is open, replace its
`window.confirm()` for rotation with the existing `ConfirmDialog`. Name the
consequence in the dialog: "Rotate the registration token? Nodes that have not
registered yet must use the new token."

**Tests.** A UI test for each page with a mocked `{registrationToken: "abc…"}`
response: Settings shows the masked token, and Import's command contains
`REGISTRATION_TOKEN=abc…`.

### B3. Dashboard misreports builds and offline nodes

**Symptom.** The dashboard shows "No active builds" while a build runs. Pending
and Registered nodes appear in red as "Offline" and are counted as offline.

**Cause.** `Dashboard.tsx` compares `a.phase === "building"`, but the store
constant is `ArtifactBuilding = "Building"`. The offline list is
`nodes.filter(n => n.phase !== "Online")`.

**Fix.**

- Add `ui/src/lib/phase.ts` with helpers that compare case-insensitively:
  `isBuilding`, `isReady`, `isFailed`, `isOnline`, `isOffline`. Use them in
  `Dashboard.tsx` and in the other places that compare phase strings.
- On the dashboard, "offline" means `phase === "Offline"` only. Add a count to
  the status strip for nodes that are not yet online (Pending and Registered),
  labelled "waiting". The activity badge uses `StatusBadge` instead of the local
  `nodeStatusBadge`, which knows only Online and Offline.

**Tests.** With mocked data (1 `Building` artifact; 1 Online, 1 Offline, 1
Pending and 1 Registered node), the dashboard shows 1 active build, and the
Offline card lists only the Offline node.

### B4. Bulk command reaches nodes that are not shown

**Symptom.** On the Nodes page, type `pos` in the hostname search, or choose a
phase. The button still says "Send Command to 13 nodes", and the command goes to
nodes that are filtered out.

**Cause.** `Nodes.tsx` computes `filteredNodes` (hostname search, done in the
browser) but the button label, dialog title, confirmation and payload use
`nodes`. When a group or label filter is set, `handleBulkSubmit` sends a
`selector` instead. The server resolves that selector on its own, without the
hostname or phase filter, so the command also reaches offline nodes in the
group.

**Fix.** The bulk command always targets exactly the nodes on screen:

- The button label, dialog title and confirmation use `filteredNodes.length`.
- The payload is always `{ nodeIDs: filteredNodes.map(n => n.id) }`. The
  `selector` path is no longer used from this page. `sendBulkCommand` keeps
  accepting a selector for API users.
- When no filter is set, the confirmation dialog appears, as it does today, and
  says "Send upgrade to all 13 nodes". When a filter is set, the dialog says
  which nodes: "Send upgrade to 4 nodes matching hostname "pos"".
- The button is disabled when `filteredNodes` is empty.

**Trade-off.** A selector is resolved on the server at the moment of the send.
Node IDs come from a list that polls every 5 s, so a node that registers in that
window is not included. This is the behavior an operator expects ("what I see
is what I target"), so we accept it.

**Tests.** With 3 mocked nodes and the search set to match 1, submitting sends
exactly that node's ID, and the button label says 1 node. With a group filter
and a phase filter, the payload contains only the IDs shown.

## 2. Design tokens

All colors come from CSS custom properties in `ui/src/index.css`, and are
exposed to Tailwind through the existing `@theme inline` block.

### 2.1 Brand and neutral

| Token | Light | Dark | Use |
|---|---|---|---|
| `--primary` | `#EE5007` | `#FF7442` | Primary button, active nav item, current wizard step, links |
| `--primary-hover` | `#FF7442` | `#FF8A5C` | Hover of the primary button |
| `--primary-foreground` | `#FFFFFF` | `#0F172A` | Text on primary |
| `--primary-soft` | `#FFF0E8` | `#2A1A14` | Selected card or chip background |
| `--accent` | `#F1F5F9` | `#2D3341` | Hover and focus surface for outline and ghost buttons, menu items, select options |
| `--accent-foreground` | `#0F172A` | `#F1F5F9` | Text on accent |
| `--navy` | `#03153A` | `#111419` | Sidebar and terminal backgrounds |

Two of these are changes to existing tokens:

- **`--primary` becomes the brand orange in light mode** (today it is navy). In
  dark mode it is already `#FF7442`. The navy value moves to `--navy`, which the
  sidebar tokens reference. Outside `components/ui`, one element uses
  `text-primary` (a link in `ArtifactBuilder.tsx`), and orange is correct for a
  link.
- **`--accent` becomes a neutral surface** (today it is the brand orange). This
  is the fix for outline and ghost buttons, dropdown items and select options
  that turn solid orange on hover or focus.

### 2.2 Status

| Token | Light | Dark | Meaning |
|---|---|---|---|
| `--success` / `--success-foreground` | `#1A9A52` / `#12733C` | `#3CC97A` / `#5FDC95` | Online, Ready, Completed |
| `--warning` / `--warning-foreground` | `#C98A04` / `#8F6200` | `#EAB308` / `#F4C93F` | Pending, Queued, degraded |
| `--danger` / `--danger-foreground` | `#D6372C` / `#A8261D` | `#FF5F55` / `#FF8A82` | Offline, Failed, Error |
| `--info` / `--info-foreground` | `#2F6FE0` / `#1F53B5` | `#5D93FF` / `#8AB0FF` | Running, Building, Upgrading |
| `--neutral` / `--neutral-foreground` | `#8A94A8` / `#56607A` | `#6F7A91` / `#A3ADC2` | Registered, Expired, unknown |

`-foreground` here means the text color on a soft background of the same hue.
`--destructive` stays as an alias of `--danger` so that existing shadcn
components keep working.

**StatusBadge** maps each phase to one of these five tones through the helpers in
`lib/phase.ts`. The change you can see: Running, Building and Upgrading become
blue. Today they are red-orange, and users read them as errors. The existing
`statusBadge.test.tsx` is updated, and it keeps the rule that a missing status
renders as "unknown".

### 2.3 Guard against regressions

Add `ui/src/test/noHardcodedBrandColors.test.ts`. It reads every `.tsx` file
under `src/` and fails when it finds `#EE5007`, `#FF7442`, `#C73F00`, `#03153A`
or `#FFB380`. It lists the files and line numbers. The allowlist is empty, so the
test passes only after the migration in 3.3 is done.

## 3. Buttons

### 3.1 Variants

`ui/src/components/ui/button.tsx` keeps the existing variant names, so the
diff to call sites stays small.

| Variant | Look | Use |
|---|---|---|
| `default` | Solid `--primary`, hover `--primary-hover` | The primary action. At most one per page area. |
| `outline` | Border `--input`, hover `--accent` | Secondary actions. Most buttons are this variant. |
| `secondary` | Filled `--secondary`, hover darker | Low-emphasis filled button (segmented control, toolbar). |
| `ghost` | No border, hover `--accent` | Actions inside cards, rows and menus. |
| `destructive` | Solid `--danger` | Only the confirm button of a destructive dialog. |
| `destructive-outline` (new) | Border `--input`, text `--danger-foreground`, hover soft danger | A destructive action placed on a page (Delete, Decommission). |
| `link` | `--primary` text, underline on hover | Inline links. |

### 3.2 Sizes

| Size | Height | Use |
|---|---|---|
| `sm` | 28px (`h-7`) | Inside tables, cards and list rows |
| `default` | 32px (`h-8`) | Page headers, toolbars |
| `lg` | 40px (`h-10`) | Dialog footers, sticky wizard footers |
| `icon` | 32×32 | Icon-only button in headers |
| `icon-sm` (new) | 28×28 | Icon-only button in rows |

Today `default` is 36px and `icon` is 36×36. Changing them to 32px makes every
page slightly denser. We accept this, because the new layouts in pieces 2 and 3
are designed for it.

An icon-only button must have an `aria-label`. A dev-mode `console.warn` in
`Button` flags a `size="icon"` or `size="icon-sm"` button without one.

A button that runs an async action shows a spinner and a verb ending in "…"
("Rotating…") while the request runs. This becomes a `loading` prop on `Button`.
When it is set, the button is disabled and shows the spinner before its
children.

### 3.3 Migration

Hard-coded brand colors appear 153 times in 20 files. The largest are
`ArtifactBuilder.tsx` (48), `ArtifactDetail.tsx` (20) and `Dashboard.tsx` (17).

1. **Buttons.** Remove the class string
   `bg-[#EE5007] hover:bg-[#FF7442] text-white` from `<Button>` elements, so
   they use `variant="default"`. A script does this, and a person reviews the
   diff.
2. **Buttons that are primary today but should not be.** The Groups page
   "Create Group" button is navy today, and becomes `default`. The artifact
   detail header has several solid buttons: "Saved" becomes `outline` and
   Delete becomes `destructive-outline`, and they stay where they are (the
   header layout is piece 3).
3. **Other brand uses** (text, borders, selected-card backgrounds, rings): map
   them to token classes: `text-primary`, `border-primary`, `bg-primary-soft`,
   `ring-primary`, `bg-primary/10`. This is a manual pass, file by file, with a
   screenshot of each page before and after.
4. **Status colors in pages** (`bg-amber-50`, `bg-red-500`, `bg-emerald-950`
   and similar, about 40 uses): map them to the status tokens where they carry
   status. Leave them where they are decoration, and add a comment.

The guard test from 2.3 proves that step 3 is complete.

### 3.4 Placement rules (for pieces 2 and 3)

These rules are part of the system, but piece 1 does not move any button.

- Order in an action group: secondary buttons, then the primary, then ⋯. The
  primary is the rightmost labelled button.
- A destructive action is never next to the primary. It goes in the ⋯ menu, or
  in a "Danger zone" section at the end of the page.
- A button label names the result ("Start build", "Send command"), never "OK" or
  "Submit".
- A confirmation dialog for a destructive or fleet-wide action names the target
  and the count.

## 4. Theme switching

### 4.1 Behavior

- There are three modes: `system` (default), `light` and `dark`. The choice is
  stored in `localStorage` under `auroraboot_theme`. If storage cannot be read,
  the mode is `system`.
- The mode is applied by adding or removing the `.dark` class on `<html>`. The
  dark token block already exists in `index.css`.
- In `system` mode, a `matchMedia("(prefers-color-scheme: dark)")` listener
  updates the class when the OS setting changes.
- A small inline script in `ui/index.html` sets the class before React loads, so
  a page never flashes in the wrong theme. It contains only the storage read and
  the media query.

### 4.2 Code

- `ui/src/lib/theme.ts`: `getStoredTheme()`, `setStoredTheme(mode)`,
  `applyTheme(mode)`. No React.
- `ui/src/hooks/useTheme.ts`: returns `{ mode, resolved, setMode }` and owns the
  `matchMedia` listener.
- The control is in the sidebar footer, above Logout. On the full sidebar it is
  a three-part control (monitor, sun and moon icons, each with an `aria-label`
  and tooltip). On the icon rail it is one button that cycles through the three
  modes. Its tooltip names the current mode.
- The Login page reads the same stored mode, so the login screen matches.

### 4.3 Dark mode audit

The dark tokens exist, but no page has been checked in dark mode. As part of
this piece:

- Take a screenshot of every route in dark mode at 1440px, including the builder
  steps and open dialogs, and fix every element with unreadable contrast.
  Two known cases: the navy activity icon on the dashboard
  (`text-[#03153A]`), and hard-coded light backgrounds in `InfoTooltip.tsx`.
- Colors on the terminal and log panels (`.terminal-output`, the ANSI output)
  must read on `--navy` in both themes.
- Target contrast: body text at least 4.5:1, and status text on its soft
  background at least 4.5:1.

## 5. Responsive app shell

### 5.1 Sidebar states

| Width | Default state | User control |
|---|---|---|
| ≥ 1024px | Full sidebar, 240px | A collapse button in the sidebar header switches to a 64px icon rail. The choice is stored in `localStorage` as `auroraboot_sidebar`. |
| 768–1023px | Icon rail, 64px | Can be expanded, which overlays the content and does not push it. |
| < 768px | Hidden | A top bar with the logo and a menu button opens the sidebar as a drawer. |

- The drawer uses the existing `@radix-ui/react-dialog`. It traps focus, closes
  on Escape, on a click outside, and when a navigation link is followed.
- On the icon rail, each item has a tooltip and an `aria-label` with its label.
  Section labels are hidden, and a thin divider separates the sections.
- `Layout.tsx` owns this state. `BuilderChip` shows on the full sidebar only.

### 5.2 Content at narrow widths

- The main content padding changes from `p-8` to `p-4` below 768px and `p-6`
  between 768px and 1023px.
- `PageHeader` wraps its actions under the title when they do not fit, instead
  of squeezing the title. The title can wrap.
- Tables already scroll inside their own container (`components/ui/table.tsx`).
  Check each table page, and make sure no parent sets a fixed width that breaks
  this.
- Filter rows that use `grid-cols-4` (Nodes) become one column below 768px.
- Dialogs use nearly the full width below 640px (`max-w-[calc(100vw-2rem)]`).

### 5.3 Acceptance

At 390px, 768px and 1440px, in light and dark, no route scrolls horizontally, and
every primary action can be reached. A script in the test plan checks the first
condition. The screenshots are reviewed by a person.

## 6. Testing

- **Go:** handler and store tests for B1. Run `go test ./pkg/... ./internal/...`.
- **UI unit and component tests (vitest):** B1–B4 regressions; `lib/phase.ts`;
  `StatusBadge` tones; `Button` variants and the `loading` prop; the theme
  functions and hook (stored mode, system mode following a mocked
  `matchMedia`); sidebar collapse persistence and drawer open/close; the
  hard-coded color guard.
- **Build and lint:** `npm run lint`, `npm run build` (`tsc -b` and vite), and
  `npm test` all pass.
- **Visual check:** a Playwright script (kept under `ui/e2e-visual/`, not run in
  CI for now) that starts from a seeded server and takes a screenshot of every
  route at the 3 widths and in both themes. It also reports any route where
  `document.documentElement.scrollWidth > innerWidth`. We compare the before and
  after screenshots for the button migration.

## 7. Delivery

Four PRs, in this order. Each one can be reviewed and reverted on its own.

1. **Bug fixes B1–B4**, including the Go change and the OpenAPI regeneration.
   Small, and no visual change beyond the fixed data.
2. **Tokens and buttons**: `index.css` tokens, `button.tsx` variants and sizes,
   `StatusBadge` tones, the migration of all 153 hard-coded brand colors, and
   the guard test.
3. **Theme switching**: `lib/theme.ts`, the hook, the inline script, the sidebar
   control, and the fixes from the dark mode audit.
4. **Responsive shell**: sidebar states, drawer, padding, `PageHeader` wrapping,
   filter rows, dialog width.

PR 2 has the largest visual effect, so it includes before and after screenshots
of every page.

## 8. Risks

- **The migration misses dynamic class strings** (classes built with `cn()` or
  template strings). The guard test catches every hex literal left in a `.tsx`
  file, including those.
- **`--primary` changing from navy to orange** could change an element that
  relied on navy. Search for every `primary` class before the change, and
  review each one. Today there are 8 uses, most of them in `components/ui`.
- **Smaller default button size.** Some headers might wrap differently. The
  screenshot comparison shows it.
- **B4 changes how bulk commands target nodes.** API users of the selector are
  not affected. Only the Nodes page stops using it.

## 9. Decisions

These defaults were chosen during the design. They can be changed in review.

- The `node_count` JSON name stays snake_case to match existing consumers,
  although other group fields are camelCase.
- The theme defaults to `system`.
- The bulk command targets node IDs, not a selector.
- Existing variant names are kept. No `brand` variant is added: `default` is the
  primary.
