# kbu — KubeUI

<p align="center">
  <img src="docs/icon.svg" width="128" alt="kbu icon" />
</p>

[![GitHub Release](https://img.shields.io/github/v/release/vulcanshen/kbu)](https://github.com/vulcanshen/kbu/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/vulcanshen/kbu)](https://go.dev/)
[![License](https://img.shields.io/badge/license-GPL--3.0-blue)](LICENSE)
[![Kubetools](https://img.shields.io/static/v1?label=Curated&message=Kubetools&color=2a7f62)](https://collabnix.github.io/kubetools/#cluster-with-core-cli-tools)
[![Charm in the Wild](https://img.shields.io/static/v1?label=Listed%20in&message=Charm%20in%20the%20Wild&color=6B5CE7)](https://github.com/charm-and-friends/charm-in-the-wild#cloud-and-devops)

**Language**: English · [繁體中文](README-zh_TW.md)

> [!WARNING]
> **v2.0 rename note.** kbu is the same tool previously released as **km8** (v1.7.x and earlier). Everything you know still works — the command binary is now `kbu`, the config directory moved from `~/.config/km8/` to `~/.config/kbu/` with a one-shot auto-migration on first launch, and `$KM8__*` env vars are still read as a fallback, kept permanently for backward compatibility (see the Environment variables table). Upgrade is drop-in; no manual steps required.

**A single-pane Kubernetes workspace** — `Tab` / `Space` / `Enter` / `Esc` drive everything. No hotkey memorization, no setup, no learning curve. Relatives navigation, YAML compare, and an embedded persistent shell are built in; any other terminal tool you trust rides along through the shell.

> _When in doubt, hit_ **`Space`**.

## Demo

### Getting around kbu

![basics](docs/demo-basics.gif)

### Select multiple namespaces at once (v2.1)

![namespace](docs/demo-namespace.gif)

### Navigate Kubernetes by relatives

![relatives](docs/demo-relatives.gif)

### Edit live resources via the Space menu

![yaml-edit](docs/demo-yaml-edit.gif)

### Diff two resources side-by-side

![compare](docs/demo-compare.gif)

### Helm as a first-class resource

![helm](docs/demo-helm.gif)

### TUI + persistent shell in one window

![alterm](docs/demo-alterm.gif)

## Four keys to drive kbu

| Key | Behavior |
|---|---|
| **`Tab`** | Switch panel focus (or `1` / `2` / `3` directly) |
| **`Enter`** | Drill in / commit a choice |
| **`Space`** | *What can I do here?* — opens a contextual menu or cheatsheet on every panel and every tab |
| **`Esc`** | Back out — pop one drill level / close any popup |

When in doubt, press `Space`. Power-user shortcuts (`P` pin / `S` sort or shell / `D` drag-pin or delete / `Alt+Shift+S` panel-2 sort / `C` compare or context / `Y` YAML / `E` edit / `N` ns / `>` settings) exist for speed — every one is also reachable through the `Space` menu, so nothing's required to memorize unless you want it.

**Mouse works too**, since v1.6: left-click focuses a panel and moves the cursor, double-click drills, right-click opens the same context menu as `Space`, and the wheel scrolls half-page. Press `>` to open the Settings popup if you want to flip mouse off and stay keyboard-only.

## Install

### Quick Install (macOS/Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/vulcanshen/kbu/main/install.sh | sh
```

### Quick Install (Windows PowerShell)

```powershell
irm https://raw.githubusercontent.com/vulcanshen/kbu/main/install.ps1 | iex
```

### Homebrew (macOS/Linux)

```bash
brew install vulcanshen/tap/kbu
```

### Scoop (Windows)

```powershell
scoop bucket add vulcanshen https://github.com/vulcanshen/scoop-bucket
scoop install kbu
```

### From source

```bash
go install github.com/vulcanshen/kbu/cmd@latest
```

### Build locally

```bash
git clone https://github.com/vulcanshen/kbu.git
cd kbu
go build -o kbu ./cmd/
./kbu
```

### Uninstall

```bash
# macOS/Linux
curl -fsSL https://raw.githubusercontent.com/vulcanshen/kbu/main/uninstall.sh | sh

# Windows PowerShell
irm https://raw.githubusercontent.com/vulcanshen/kbu/main/uninstall.ps1 | iex
```

## Quick Start

```bash
kbu
```

Connects to your current kubeconfig context. Press `Enter` to drill, `Space` for the contextual menu, `Esc` to back out, `Tab` to move between panels.

Inspired by [Lens IDE](https://k8slens.dev/), [lazygit](https://github.com/jesseduffield/lazygit), [lazydocker](https://github.com/jesseduffield/lazydocker), and [k9s](https://github.com/derailed/k9s). Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

---

> The rest of this README is the operations manual — read on if you want the full feature surface, every keybinding, and configuration details.

## Features

- **Zero learning curve** -- every action surfaces through the `Space` menu. Power-user hotkeys (`P` pin / `S` sort / `C` compare / `Y` YAML / `E` edit / `N` ns / `>` settings / ...) exist for speed but you can ignore the whole cheat sheet — `Space` walks you through the same menus, in context, every time. Onboarding doc: *"When in doubt, hit Space."*
- **Compose, don't replace** -- Alterm (the embedded persistent shell, `Alt+t`) means any other terminal tool you'd normally drop out of kbu for rides along inside it. Use kbu for navigation and inspection; use whatever you trust for write-side operations — no scrollback split, no context switch, and Alterm keeps env / cwd / shell history intact across `Alt+t` toggles
- **Pinned resource kinds (`P` + `D` drag-and-drop)** -- panel 1's sidebar grows a Pinned section at the top. `P` on any resource row toggles pin / unpin, and the order persists into the config file. Pins **move** rather than duplicate — a pinned kind disappears from its original category and reappears under Pinned, so each kind has exactly one home. With two or more pinned kinds, press `D` on a pinned row to enter modal drag-and-drop: `j`/`k` swap the locked kind with its neighbour, `Enter` or `D` commits the new order, `Esc` and anything else reverts to the snapshot taken at entry. The header reads `Pinned 󰩐 [D]rop` while dragging, the dragged row paints lavender, and a sticky toast carries the keyboard contract; `Space` mid-drag opens a trimmed drop-only menu if the contract slips out of memory. Pin / sort / future-per-kind settings share the same per-kind config block, so a CRD that briefly goes away (operator reinstall, etc.) keeps its pin and sort silently and restores both the moment it comes back
- **YAML Compare popup (`C`)** -- panel 2 row-level diff. `C` on a row marks it as the **compare anchor** (status-bar `[C]ompare` chip surfaces while the anchor is set); `C` on a different row of the same kind opens a side-by-side or unified YAML diff. `C` on the anchor itself cancels — the same key toggles all three states (mark / diff / cancel). The `[C]ompare` chip uses anti-correlated dimming with `[C]ontext` — on panel 2 (where the table hijacks `C` for compare actions) `[C]ompare` lights up and `[C]ontext` dims; on panels 1 / 3 the reverse, so the brightness handoff signals "which `C` action fires on the active panel". Panel-2 menu opened while in compare mode surfaces "Compare to anchor" as the **first item** (it's the primary intent when the user opened the menu on a candidate row); Mark / Unmark sub-cases stay in the row-action slot. The diff popup has its own action menu (`Space`) to switch layout live, and the default layout (Unified) is persisted in config. Compare YAML is pre-cleaned (status / managedFields / resourceVersion / uid stripped) so the diff focuses on what the user actually authored
- **List-view sort (`S` on sidebar, `Alt+Shift+S` on panel 2)** -- per-kind multi-column sort persisted across restarts. Pick a column → direction → the picker loops back to the column step so additional tiers can be stacked without re-invoking the flow. Each tier renders its priority and direction in the panel-2 header (`Name (1) ↑ · Restarts (2) ↓ …`); single-tier chains collapse to just the arrow to keep the simple case visually quiet. Reset row at the bottom drops the entire chain in one shot; per-column `Unset` removes a single tier from the direction step. `Esc` is the only way out — the picker never closes itself between operations. Comparators are type-aware: `Age` / `Updated` use the underlying timestamp (not the rendered "5d3h" string); `Ready` parses "N/M" as a pair of ints; `Restarts`, `Desired`, `Current`, `Up-to-date`, `Available`, `Active`, `Rev` use the int form so "10" sorts above "2". Unknown columns silently skip so a stale config doesn't break the sort. No saved sort = `(namespace, name)` ascending, matching kubectl's cross-namespace default
- **Mouse support** -- click a panel to focus it + move the cursor, double-click to drill (synthesizes `Enter`), right-click to open the row's context menu (synthesizes `Space`), wheel scrolls half-page (synthesizes `u` / `d`). 13 popups respond too: list popups commit on left-click, viewer popups (YAML / Compare / App Log / Help) keep wheel scroll, the confirm dialog deliberately makes left-click a no-op so a stray click can't trigger a destructive delete / edit / rollback. Mouse can be turned off in the Settings popup (`>`) and a `scroll_direction: natural | reverse` setting flips the wheel for users who prefer the inverse mapping
- **Settings popup (`>`)** -- app-level surface with a cog glyph in the title. Currently carries Mouse on/off + Scroll Direction; future global settings drop in here. The popup is its own escape hatch: even when Mouse is off, clicking remains possible inside the popup so users can turn mouse back on
- **Layer-based popup borders (v1.7.4)** -- every popup picks its border color from a `lavender → sapphire` gradient based on nesting depth: layer 1 (any first-tier popup) uses lavenphire25, layer 2 (a popup over another popup — e.g. the Diff menu inside Compare, or Confirm inside Breadcrumb) uses lavenphire50, layer 3 lavenphire75, layer 4+ sapphire. The visual stack always reads "this thing is on top of what's underneath" without needing to think about it. Toast warnings keep their Catppuccin Peach border as a dedicated warning signal; PTY popups (Alterm, `kubectl edit`, `kubectl exec`) are layer-1 always — they're context-shift targets that replace the popup tree rather than stack on it (popup-convention §1.10), so they share one border color regardless of which menu chain launched them. In-place picker swaps (e.g. sort column → direction) keep their original layer — the picker is the same instance, just with new content
- **28 built-in resource types + CRD support** -- dynamic discovery of Custom Resources at startup, across KubeConfig / Cluster / Workloads / Network / Config / Storage / RBAC / Autoscaling / Helm categories. The Helm category only registers when the `helm` CLI is on `PATH`
- **Real-time Watch updates** -- resources refresh automatically via Kubernetes Watch API
- **Vim-style navigation** -- `j`/`k`, `u`/`d` page scroll, `gg`/`G`, `/` search
- **3-panel lazygit-style layout** -- numbered sidebar, list, and detail panels with scroll indicator
- **Drill-down navigation** -- Deployment / DaemonSet / StatefulSet / Job → Pods → Containers; CronJob → Jobs; HPA → target workload; PVC → mounting Pods; PDB → protected Pods; Helm Release → each native K8s object the chart deployed
- **Relatives tab — Lens-style navigation** -- every detail panel (except Namespaces) lists the resource's navigable references (owners, selected pods, scaleTargetRef, mounted-by pods, ...). `Enter` drills into the cursor's ref — the panel re-renders showing *that* resource's Relatives, building a chain (Deployment → Pod → ConfigMap → consumer Pods, ...). `Esc` pops one level. Panel 3's bottom-left border surfaces the tab-contextual hint (`enter: drill` at depth 1, `enter: drill  esc: back` once you've drilled). `Space` opens a breadcrumb popup so you can jump panels 1+2 back to any chain ancestor (confirms first). Tab label shows `Relatives N` at depth>1. `Y` opens the YAML of whichever entry the cursor is on. Cycle detection blocks revisiting an ancestor; fetch failures toast and stay put. 26 of 27 resource kinds covered — ConfigMaps / Secrets / ServiceAccounts surface *reverse* refs (which Pods use me, which RoleBindings name this SA as a subject, ...); Helm releases surface their `Deployed Resources` so each chart-deployed K8s object is one drill away
- **Helm releases (when `helm` is on `PATH`)** -- a dedicated `Helm > Releases` sidebar category lists every release in the cluster (`helm list -A` polled every 3s; no Helm watch API). Panel 2 columns: `NAME / NAMESPACE / CHART / APP VER / REV / STATUS / UPDATED`. Press `Space` on a release row to open a doc menu (Manifest / Creator Notes / User Values / Merged Values / Hooks); pick one with `Enter` to fetch via `helm get ...` and view the result in the YAML popup. The menu stays open behind the YAML so consecutive docs flow without re-opening. Panel 3 carries a `History` tab in place of Events — table view of every revision (REV / STATUS / DATE / CHART / DESCRIPTION) with the current deployed rev marked `●`. `Space` on a non-current row asks to roll back; confirm shows the exact `helm rollback` command and runs it asynchronously, with the result surfaced as a toast. Helm-managed K8s objects (label `app.kubernetes.io/managed-by: Helm` or annotation `meta.helm.sh/release-name`) are marked with a `` glyph in panel 2 and block `E` (kubectl edit) with a "Helm-managed (read-only)" toast — use `helm upgrade` / `rollback` instead. Press `.` on any non-Releases list to hide all helm-managed objects (panel 2 bottom-left always shows the `.: helm` hint)
- **YAML popup (`Y`)** -- raw `kubectl get -o yaml` of the selected resource in a full-screen overlay, running as a **vim-style buffer**: `hjkl` moves a cursor (auto-scrolls to keep it visible), `w`/`b`/`e` word motion, `0`/`$` line start/end, `gg`/`G` buffer top/bottom, `u`/`d` half-page. Press `v` to enter character-wise visual mode (anchor at cursor, `hjkl` extends the selection); `y` in visual mode copies the selected substring, `y` outside visual mode copies the full YAML. `/` search with `n`/`N` step-through (full-row highlight, cursor snaps onto each match), `E` dispatches `kubectl edit` directly, `Esc` exits visual first / closes second. YAML lives in the popup, not the detail panel, so vertical layout no longer wraps long YAML lines awkwardly
- **Pod log streaming with auto-follow** -- multi-container support with `<container>|<log>` format; the Logs tab sticks to the tail by default. The Logs tab label carries an inline Nerd Font glyph showing the follow state — `▶` (live, U+F0753) when auto-follow is on, `⏸` (paused, U+F0754) after the user scrolls up. The glyph stays on the tab whether or not it's active so panel-3's tab bar stays the same width when you switch tabs. Scroll up (`k`/`↑`/`u`/`gg`) to pause and read history; press `G` to catch up and resume following. Panel 3's bottom-left border surfaces `u/d: page  gg: top  G: live` as an at-a-glance cheat sheet
- **Aggregate logs for all workload kinds** -- selecting a workload row streams logs from **every Pod** the workload manages into a single Logs tab. Lines are prefixed `<pod-hash>│<container>│<text>` with each segment in its own stable color, so during a rollout you can spot at a glance which pod is throwing errors without drill-down. Covers Deployment (current ReplicaSet, falls back to selector on RBAC miss), StatefulSet, DaemonSet, Job, ReplicaSet, CronJob (across all retained Jobs). Pod churn: stream snapshots at row-select; re-select the row to refresh
- **Aggregate child events for workload kinds** -- the Events tab on a workload row merges events from the workload AND its child Pods, sorted newest first. The Object column ("`Pod/web-abc-xyz`" vs "`Deployment/web`") names each event's source so the chain is visible inline. CronJob is 3-tier: CronJob's own events + every owned Job's events + every Pod's events, so "why did last night's cron fail" reads from one tab instead of `kubectl describe` × N
- **Events tab follow-latest (v1.7.10)** -- same live (▶) / paused (⏸) glyph in the tab title as the Logs tab; `G` re-attaches the tail, `k` / scroll-up pauses and freezes the current view against incoming watcher ticks. Bottom-left border reads `u/d: page  gg: top  G: live`. Rationale: aggregate events on a busy workload arrive in bursts — pausing lets the user read a snapshot without new events pushing it off-screen
- **Conditions tab** -- new detail-panel tab showing `.status.conditions` as a `TYPE / STATUS / REASON / MESSAGE / AGE` table (same as `kubectl describe`'s Conditions section). Status `False` rows highlighted red. Appears for kinds that populate conditions (Pod / Node / PVC / Deployment / StatefulSet / DaemonSet / Job / HPA / Ingress); hidden for kinds without (ConfigMap, Secret, Service, etc.). Critical when events have expired past TTL — conditions reflect *current* state, events reflect *recent* state
- **Status column coloring — abnormal-only** -- panel 2's Status column (every kind that has one — Pod / Node / Namespace / PVC / PV / Helm Release) plus the Events Type column paint **only** abnormal values. Yellow for transitional / degraded (Pending / Terminating / SchedulingDisabled / Released / pending-* / Init:*), red for failures (Failed / Error / CrashLoopBackOff / ImagePullBackOff / NotReady / Lost / Warning). Healthy values (Running / Bound / Active / Deployed / Normal) stay at the row's base foreground. Color = signal, not decoration — your eye is drawn only to rows that need attention. Cursor / lock rows pick the darker Catppuccin Latte variant so pastels don't wash out on the reverse-video bg
- **Row switch debounce (300ms)** -- panel 2 j/k mashing used to fire one detail fetch + one log-stream Start per row, even for rows the cursor flew past. The dispatch is now debounced: every row switch bumps a sequence counter and schedules the fetch / stream Start 300ms later, so a rapid scroll through 49 rows fires exactly one fetch (for the row you stopped on) instead of 49. Lie-as-lock invariant preserves: panel 2 still feels instantly responsive because the cheap state mutations (Stop previous stream, clear retry throttle) run inline; only the expensive work defers. Matches the existing sidebar `switchSeq` debounce window for muscle-memory consistency
- **Edit & shell exec via embedded PTY** -- `E` runs `kubectl edit` and `S` runs `kubectl exec -it -- /bin/sh`, both inside an in-app virtual terminal so the editor and shell session never touch the host terminal scrollback. Editor honors `$KUBE_EDITOR` / `$EDITOR` (or `config.yaml editor`)
- **Alterm internal terminal** -- `Alt+t` toggles an embedded shell (login shell with full env / cwd) inside kbu — like `ssh localhost` in a popup. Run `kubectl apply -f`, `helm`, anything you'd normally drop out of kbu to do. The shell is **persistent**: pressing `Alt+t` while the popup is visible hides it without killing the shell; pressing it again reattaches (cwd, history, env, background jobs all preserved). An `[Alt-t]erm` chip on the right of the status bar shows when the shell is alive in the background — bracket-hotkey format matching `[C]ontext` / `[N]amespace` (one rule across the statusbar: anything bracketed is a hotkey). Independent of `kubectl edit` / `kubectl exec` — you can keep Alterm running while editing a resource or exec'ing into a container in a separate popup
- **PTY popups always layer 1** -- Alterm, `kubectl edit`, and `kubectl exec` all render with the layer-1 border color (lavenphire25). They're context-shift targets that REPLACE the popup tree rather than stack on top of it (popup-convention §1.10 — the entry handler closes every blocking popup beneath), so the layer concept is "this is the single popup on screen". One border color across all PTY surfaces regardless of which menu chain launched them. The title (`Alterm: hostname` vs `Edit: pod/foo` vs `Shell: pod/foo → ctnr`) carries the kind distinction
- **PTY scrollback** -- 10k-line history for all PTY popups (Alterm, shell exec, edit). `PgUp` / `PgDn` page, `Home` / `End` jump to top / live. Disabled in alt-screen apps (vim, less, htop) so they keep their own paging
- **Per-container colored log labels** -- multi-container pods are visually distinguishable line-by-line; stable color per container name
- **Resource deletion** -- `D` (uppercase, both as a hotkey and via the `Space` menu) with confirmation dialog. Namespace deletes carry a stronger warning (`!!! Delete namespace "X"? This will remove ALL resources inside it.`) because the cascade to every workload inside the namespace makes it the single most dangerous delete kbu exposes. Events / Nodes stay blocked outright — Events are system-generated immutable records, Nodes are admin infra actions outside kbu's scout-tool audience
- **Search/filter** -- `/` to search in the sidebar and table panels, and in the namespace/context picker popups. Sidebar search also matches category names (e.g. "cluster" expands the Cluster category). Search clears automatically when focus moves to another panel — selection persists, the filter doesn't
- **Clipboard copy (`y`)** -- copies the focused element's content via OSC 52 (works through tmux/SSH, no `xclip`/`pbcopy` required). Semantics track focus: if the focus target has a cursor (sidebar kind, panel-2 row, panel-3 Relatives/History row, YAML popup in visual mode), `y` copies just that row / selection — tab-separated raw values ready for `awk`/`cut` on cursor-row targets, verbatim substring for YAML visual selection. If it doesn't (panel-3 Logs/Events/Conditions, App Log popup, YAML popup outside visual mode), `y` copies the entire focus content
- **Toast notifications with levels** -- info-level (1s, popup-layer border + `󰵅 kbu` title, hint reads `auto-dismiss`) for confirmations like "Copied!"; warning-level (2s Catppuccin Peach + `󰀦 kbu` title) for blocked actions like Relatives cycle detection or drill failures; sticky variant for modal state contracts (drag mode hint) — hint switches to `Esc: close`, no auto-dismiss timer
- **Multi-namespace selection (v2.1)** -- `N` opens a checkbox picker. `Enter` toggles the namespace under the cursor and live-applies (panel 2 re-fetches at once); the popup stays open so you can check several in a row, and checked namespaces read green. Move with `j`/`k` (`u`/`d` page, `gg`/`G` jump), `/` to search, `Esc`/`Space` to close. "All Namespaces" and specific namespaces are mutually exclusive — checking a specific one clears All, unchecking the last falls back to All. A specific selection lists each namespace separately (client-go's namespaced List, in name order, rendered incrementally as each returns); "All Namespaces" keeps the single cluster-wide list — no fetch-all-then-filter. Namespaced-resource lists gain a leading Namespace column (first, ahead of Name; middle-truncated like Name), and the status bar shows the single namespace, "All Namespaces", or "N selected". The selection persists across restarts and is reconciled against the namespaces that still exist on launch — deleted ones are dropped, and if none survive it falls back to All. `C` switches context (uppercase — trigger keys are uppercase to avoid mis-triggering while typing search queries)
- **KubeConfig ▸ Contexts (v2.2)** -- a read-only view of your kubeconfig contexts, in a new "KubeConfig" category that leads the sidebar. Panel 2 lists Name / Cluster / User / Namespace / Server with the currently-connected context marked by a trailing `*`; panel 3's Info tab adds TLS + Auth summaries, and `Y` opens a kubeconfig-shaped YAML view. Reads the kubeconfig directly (works even when the cluster is unreachable). Strictly read-only — no edit / delete, Enter is inert, and switching context stays on the `C` picker. Credentials are never shown: only non-secret metadata is surfaced and the YAML is built from an allowlist, so tokens / passwords / keys always render as `<redacted>`
- **Session-local context** -- switching context in kbu doesn't touch `~/.kube/config`. Run `kubectl` in another terminal in parallel without interference
- **Session state persistence (v1.7.10)** -- quit and relaunch drops you back where you were. kbu records the current `(context, namespace, kind, panel-2 row cursor, focused panel, panel-3 active tab)` on every quit to a `state.yaml` file sitting alongside `config.yaml` in the config directory. Next launch applies the recorded context + namespace before the k8s client connects, restores the sidebar cursor to the recorded Kind, and snaps the panel-2 cursor onto the last-selected object once its watcher tick arrives. Focused panel (sidebar / table / detail) is restored too, so `q` from panel 2 lands you back on panel 2. Panel 3's active tab (v1.7.11) is restored by tab name via `SwitchToTabByName`, silent-falling back to the per-kind default if the recorded tab doesn't exist on the newly-selected Kind. Recorded values that no longer exist (namespace deleted, CRD uninstalled, object churned away) fall back to defaults with an INFO line in the App Log — no toast, no warning; you'll notice via `!` if you care. `state.yaml` is kept separate from `config.yaml` on purpose: config is your hand-authored preferences (comments preserved), state is auto-rewritten every quit — mixing them would break the "config is my document" trust boundary
- **Panel-aware selection styling** -- the focused panel's cursor row gets a bright lavender chip; the *unfocused* panel's selected row keeps a softer bg + bold so you can always see which resource each panel "remembers" while you work in another. Unfocused panels dim down to a muted overlay grey so the eye lands on the focused panel without losing the "you are here" memory of the other two
- **Detail tabs** -- panel 3's tab list is per-kind. Workload kinds (Pods / Deployments / StatefulSets / DaemonSets / Jobs / CronJobs) lead with `Logs` because switching rows is most often a "what is this thing doing right now" gesture — Relatives is a deliberate drill action that warrants the extra tab switch. Order is `Logs` / `Relatives` / `Events` / `Conditions` (Conditions only for kinds that populate `.status.conditions`). Non-workload kinds lead with `Relatives` so `Space` jumps from a Relatives entry land on the same tab the user came from. Helm releases get `Relatives` / `History`. Panel 3 has no `/` search — cursor tabs (Relatives / History) don't tolerate row filtering, and Logs read better as a plain follow-tail view; use `Y` + your editor to grep large content
- **Long values wrap, never truncate** -- applies to YAML, Events, and Logs; wrap points reflow on panel resize
- **Panel expand** -- `z` toggles full-screen on the focused Table or Detail panel; pressing `z` again restores the 3-panel layout
- **Theme system** -- drop a `theme.yaml` into config directory to override colors
- **Help & App Log overlays** -- `?` / `!` popup on top of main UI
- **Error notifications** -- status bar badge + status line message
- **Crash logging** -- panics written to the kbu log directory
- **Audit logging** -- every `kubectl edit` and `kubectl delete` recorded to `audit-*.log`

## Key Bindings

### Primary interaction: four keys

Most of the time you're driving kbu with just four keys:

| Key | Behavior |
|---|---|
| **`Tab`** | **Panel** — move focus to the next panel (or use `1` / `2` / `3` to jump directly) |
| **`Enter`** | **Into** — drill into the selected resource / commit a popup choice. Does **not** forward focus to another panel (use `Tab` / `1` / `2` / `3` for that) |
| **`Space`** | **Menu** — open a contextual popup wherever focus is: sidebar cheatsheet + Pin / Unpin / Sort / Drag actions (panel 1), per-row action menu split into item operations + panel-level Sort entry / container Shell menu / empty-list hint (panel 2), Logs / Events / Relatives-drill / Relatives-breadcrumb / History rollback (panel 3 by tab). Also closes any open popup (mirror open) |
| **`Esc`** | **Back** — pop one drill level / close any popup |

Where a contextual menu exists, `Space` is enough — you don't need to memorize the per-action keys.

Tab navigation also responds to `h`/`l` (or `[`/`]`) for switching panel 3 tabs.

### Mouse (since v1.6)

| Gesture | Behavior |
|---|---|
| **Left-click** on a panel row | Focus that panel + move the cursor to the clicked row |
| **Double-click** | Synthesizes `Enter` (drill into the cursor row) |
| **Right-click** on a row | Synthesizes `Space` (opens the row's context menu / cheatsheet) |
| **Wheel up / down** | Synthesizes `u` / `d` (half-page move). Direction can be flipped via Settings popup (`scroll_direction: natural | reverse`) |
| **Left-click** inside a list popup | Commits that row (same as cursor + `Enter`) |
| **Right-click** inside any popup | Closes it (same as `Esc`) |

Menu-style popups (panel 2 menu, sort picker, namespace / context picker, breadcrumb, helm doc menu, hint, settings, confirm) ignore the wheel — content is short and half-page semantics don't fit. Viewer popups (YAML / Compare / App Log / Help) **do** scroll on wheel. The confirm dialog deliberately makes left-click a no-op so a stray click can't trigger a destructive delete / edit / rollback — you confirm with keyboard `Enter` / `y` only.

Mouse can be disabled in the Settings popup (`>`); the popup itself stays mouse-reachable in that state so you can flip it back on.

### Accelerators — cursor + power triggers

```
 cursor    j k         u d         gg G        / (search inside current panel)
 trigger   Y YAML      E edit      N namespace
 panel 1   P pin       S sort      D drag-and-drop pinned (modal)    C context
 panel 2   S shell     Alt+Shift+S sort    D delete    C compare anchor
 expand    z           z toggles full-screen on current panel
 helm      .           . toggles helm-managed visibility on panel 2
 settings  >           > (shift+.) opens the global Settings popup
```

`S` / `C` / `D` are panel-aware overloads — same letter, different action depending on which panel has focus, mirroring how `P` only makes sense on panel 1. On panel 2, sort needs the `Alt+Shift+S` chord because bare `S` is already Shell — the modifier carves out a panel-2 sort gesture without breaking that. Trigger keys are deliberately uppercase to avoid misfiring while typing in a `/` search field.

### Global

| Key | Action |
|---|---|
| `>` | Open the global Settings popup (mouse on/off, scroll direction; future settings) |
| `Alt+t` | Toggle Alterm (spawn / show / hide; shell stays alive across hide) |
| `y` | Copy focused element to clipboard (OSC 52) -- cursor row when the focus has one, whole content otherwise |
| `!` | App log |
| `?` | Help |
| `q` | Quit kbu (saves session state on the way out) |
| `Ctrl+C` | Quit kbu -- also works while a panel search is active |

### Panel 1 sidebar Space menu

The action region is split into two labelled groups when both apply:

| Key | Action |
|---|---|
| `P` | **item operation** — Pin / Unpin the cursor's resource kind. Pinned kinds appear in a top "Pinned" section and **move** out of their original category. Order persists per-context to config |
| `S` | **item operation** — Open the Sort flow for the cursor's resource kind. Column picker loops with the direction picker so multi-column chains can be built without re-invoking. Reset row drops the whole chain |
| `D` | **panel operation** — Enter drag-and-drop reorder mode for the cursor's pinned kind. Only surfaces when the cursor is on a pinned row AND there's at least one other pinned kind to swap with |

### Panel 2 context menu (`Space` on any row)

Per-row menu with resource-aware items — `Y` YAML / `E` Edit / `S` Shell / `D` Delete plus a contextual **`C` Compare** entry (Mark anchor / Compare to anchor / Unmark anchor depending on state). When in compare mode + cursor on a comparable row, "Compare to anchor" floats to the top of the item group — it's the primary intent when the user opened the menu on a candidate row. A separator below them carves out a **panel operation** region: `[Alt-S]ort panel 2 list` opens the same column picker the panel-1 Space menu opens, scoped to the kind currently being viewed. Use `j`/`k` + `Enter` or hit the letter directly. Mark / Unmark of the Compare anchor closes the menu on commit (pure state mutation — the menu has done its job, and keeping it open would hide the anchor-row lavender highlight on panel 2); Compare-to-anchor keeps the menu open per §1.8 (target diff popup launches on top, Esc returns to the menu). Helm-managed rows hide `E`/`D` (Rule A: read-only — edits would be overwritten by `helm upgrade`/`rollback`); resources without containers hide `S`.

Two cursor-only entries are appended for navigation discoverability (no single-letter hotkey, reached via `j`/`k` + `Enter`):
- `Enter ↘` — drill into the row's children (`pods` / `jobs` / etc., per kind). Same action as pressing `Enter` on the row directly.
- `Esc ↖` — back to the parent list. Only appears when you're already inside a kind-level drill chain (e.g. viewing a Deployment's Pods). Same action as pressing `Esc` directly.

**Container drill exception:** when the cursor is on a container row (one level deeper than a Pod's pods list), the Space menu collapses to just `Shell` — the Esc entry is dropped because Esc is the universal pop-one-level gesture and a one-row menu doesn't need a redundant "back" affordance.

### Compare mode

While the anchor is set, panel 2's bottom-left border shows an `esc: exit compare` hint. The locked row paints with a lavender (Mocha) bold-reverse-video bg, the same accent as Pinned items and the Settings ON toggle — "user-set state on this row". A fixed-width `<icon> Compare` chip on the status bar confirms the mode is on without claiming variable cells for the resource name (the popup itself shows `left vs right` when you engage). Compare lock auto-clears when focus leaves panel 2 or when the anchored row falls out of the watcher stream (deleted / namespace-filtered away).

### Helm-specific

| Key | Where | Action |
|---|---|---|
| `Space` | Panel 2, Release row | Open the doc menu — pick `Manifest` / `Notes` / `User Values` / `Merged Values` / `Hooks` |
| `Space` | Panel 3, History tab, non-current row | Roll back to that revision (confirm popup shows the exact `helm rollback` command) |
| `.` | Any non-Releases panel 2 list | Toggle visibility of helm-managed objects |

### PTY popups (Alterm, edit, shell exec)

| Key | Action |
|---|---|
| `PgUp` / `PgDn` | Scroll history by one page |
| `Home` / `End` | Jump to top of history / back to live |
| Any other key | Snap back to live, key forwards to subprocess |

Scrollback is disabled when a full-screen app (vim, less, htop) takes over the PTY via alt-screen; those keys forward to the app instead so it keeps its own paging.

## Editing Resources

Pressing `E` on a resource (or picking `Edit` from the `Space` menu) runs **`kubectl edit <kind>/<name> -n <ns> --context <ctx>`** inside an embedded PTY popup. Behavior is identical to running the same command in a terminal: strategic merge patch, `resourceVersion` conflict detection, no `last-applied-configuration` annotation side-effect.

The editor is resolved by kubectl itself in this priority order:

1. `$KUBE_EDITOR` (kbu sets this if `editor` is configured in `config.yaml`)
2. `$EDITOR`
3. `vi` (Linux/macOS) or `notepad` (Windows)

When the editor exits, the popup closes and the table refreshes via the resource watch — no manual reload needed.

### Why an embedded PTY?

Earlier versions of kbu ran the editor through `tea.ExecProcess` and applied the result with `kubectl apply -f`. That approach leaked kubectl's confirmation messages into the host terminal's scrollback after quitting kbu, and the apply-vs-edit semantic mismatch surprised users coming from `kubectl edit`. The PTY popup keeps everything inside kbu and uses `kubectl edit` directly so behavior is exactly what `kubectl edit` users expect.

### Note for nvim users

If your nvim setup has noticeable shutdown lag inside the popup (LSP attach/detach, plugin teardown), set `editor: "nvim --noplugin"` in `config.yaml` to skip plugin loading for the kubectl-edit session only. Your everyday `nvim` is unaffected.

## Context Isolation

kbu maintains its own **session-local** context. Switching context with `C` inside kbu **does not** modify `~/.kube/config` or the `KUBECONFIG` environment variable in any other terminal.

All `kubectl` subprocesses spawned by kbu (edit, delete, shell exec) receive an explicit `--context <name>` flag, so they always target the cluster kbu is showing — regardless of what `kubectl`'s default context is set to.

This means you can safely run kbu in one terminal while using `kubectl` in another without either session interfering with the other's context.

## Configuration

Config files are in the OS-appropriate config directory. Set `XDG_CONFIG_HOME` to override on any platform:

| OS | Default Path |
|---|---|
| Linux | `$XDG_CONFIG_HOME/kbu/` or `~/.config/kbu/` |
| macOS | `~/Library/Application Support/kbu/` |
| Windows | `%APPDATA%/kbu/` |

Logs (crash and audit) are written to the `logs/` subdirectory of the config directory.

The v1.7.10 session state file (`state.yaml`) sits alongside `config.yaml` in the same directory. See the "Session state persistence" feature bullet above for what gets stored + when; it's auto-managed and generally shouldn't be hand-edited (config.yaml is the file for that).

### config.yaml

```yaml
default_context: ""      # kubeconfig context (default: current-context)
default_namespace: ""    # namespace filter (default: all namespaces)
editor: ""               # exposed to kubectl as $KUBE_EDITOR
                         # (default: kubectl falls back to $EDITOR → vi / notepad)
alterm_shell: ""         # shell launched by Alterm (default: $SHELL → /bin/sh).
                         # Bare names are resolved via $PATH at popup-open time
                         # (Go exec.Command semantics); absolute paths are used
                         # verbatim. Lets you pick e.g. fish inside alterm
                         # while keeping zsh as host shell.
alterm_login_shell: false # Flip true to launch Alterm with `-l` so it sources
                         # ~/.zprofile / ~/.bash_profile / /etc/profile.
                         # Default false matches the v1.7.2 baseline (non-login
                         # interactive — .bashrc/.zshrc still loads, no
                         # /etc/profile PS1 surprise). Set true when kbu is
                         # launched from a non-login parent (Raycast, Alfred,
                         # cron, tmux configured non-login) and your PATH
                         # lives in .zprofile rather than .zshrc.

# Compare popup defaults (v1.6+). `layout` picks the diff render —
# "unified" (default) is a single column with -/+ markers,
# "split" is side-by-side.
compare:
  layout: unified

# Mouse settings (v1.6+). Both fields optional; omitting either
# falls back to the defaults below.
mouse_opt_config:
  enabled: true                # set false to disable click + double-click + right-click + wheel
  scroll_direction: natural    # "natural": wheel-up = cursor up. "reverse" swaps the mapping.

# Per-kind preferences (v1.6+). Keyed by kubectl name
# ("pod" / "deployment" / "configmap" / ...). Each entry is
# optional and unknown kinds are preserved across the rewrite,
# so a CRD that briefly uninstalls won't lose its pin / sort.
#
# Sort is a multi-tier chain (v1.7+); tier 0 is the primary,
# tier 1 the first tiebreaker, etc. The v1.6 single-mapping
# shape is still accepted on load — it lifts to a one-tier
# chain and is rewritten in the new sequence form on save.
resource_kind_config:
  pod:
    pinned:
      order: 10              # sparse — increments of 10, so manual YAML
                             # tweaks can wedge a kind between two existing pins
    sort:
      - column: Restarts     # column title from the kind's panel-2 columns
        direction: desc      # "asc" or "desc"
      - column: Name         # tier 1 — tiebreaker when Restarts is equal
        direction: asc
  configmap:
    pinned:
      order: 20
    sort:                    # single-tier chain also valid
      - column: Age
        direction: desc
```

### Environment variables

Override the corresponding config slot for one-shot runs without editing the YAML — useful for CI / scripted demos / quick "try this shell" sessions.

> **v2.0 rename note.** The `KBU__*` names below replaced the pre-v2.0 `KM8__*` names. The old `KM8__*` names are still read as a fallback (kept permanently for backward compatibility) — a `KM8__CONFIGPATH` in your `~/.zshrc` from a v1.7.x install keeps working. If both a `KBU__` and its legacy `KM8__` counterpart are set, `KBU__` wins.

| Variable | Effect | Precedence |
|---|---|---|
| `KBU__CONFIGPATH` | Use this file as the config file instead of the default layout (`$XDG_CONFIG_HOME/kbu/config.yaml` etc.). Theme file path is NOT affected — it still lives under the OS config directory. Absolute path recommended; relative path resolves against CWD at load/save time. | `KBU__CONFIGPATH` > default layout |
| `KBU__STATEPATH` | Use this file as the session state file instead of `<config-dir>/state.yaml`. Same TrimSpace-then-empty-check pattern as `KBU__CONFIGPATH`. Handy for sandbox / test runs where you want per-run state without touching the real state file. | `KBU__STATEPATH` > default layout |
| `KBU__ALTERM_SHELL` | Use this binary as the Alterm shell. Bare names are looked up on `$PATH` at popup-open time (Go `exec.Command` semantics); absolute paths run verbatim. Leading / trailing whitespace is trimmed. | `KBU__ALTERM_SHELL` > `alterm_shell` config > `$SHELL` > `/bin/sh` |
| `KBU__ALTERM_LOGIN_SHELL` | Force the Alterm shell into login mode (`-l`) or out of it. Truthy values: `true` / `1` / `yes` (and uppercase). Any other value disables login mode. Use when launched from a non-login parent and your PATH is set in `.zprofile`. | `KBU__ALTERM_LOGIN_SHELL` > `alterm_login_shell` config > `false` |

Example:

```sh
# Try fish in Alterm without editing config.yaml
KBU__ALTERM_SHELL=/opt/homebrew/bin/fish kbu

# Point kbu at a per-project config (e.g. checked into the repo)
KBU__CONFIGPATH="$PWD/.kbu.yaml" kbu
```

### theme.yaml

Drop a `theme.yaml` to customize colors. Only override what you need -- unspecified fields keep defaults.

```yaml
sidebar:
  background: ""                       # empty = terminal transparent
  foreground: "#cdd6f4"
  selected_bg: "#bac2de"               # focused-panel cursor bg (reverse-video)
  selected_fg: "#1e1e2e"
  unfocused_selected_bg: "#353648"     # other-panel "remembered" selection bg
  unfocused_selected_fg: "#cdd6f4"
  category_fg: "#89b4fa"

table:
  header_bg: ""                        # empty = sits on the panel canvas, fg alone signals header
  header_fg: "#89b4fa"
  row_fg: "#cdd6f4"
  selected_row_bg: "#bac2de"           # focused-panel cursor bg (reverse-video)
  selected_row_fg: "#1e1e2e"
  unfocused_selected_row_bg: "#b4befe" # other-panel "remembered" selection bg — Catppuccin lavender chip
  unfocused_selected_row_fg: "#1e1e2e"
  alternating_bg: ""

detail:
  border_color: "#585b70"
  label_fg: "#89b4fa"
  value_fg: "#cdd6f4"
  tab_active_bg: "#45475a"
  tab_active_fg: "#cdd6f4"
  tab_inactive_fg: "#7f849c"           # Catppuccin overlay1 (v1.7.5)

status_bar:
  background: ""                       # empty = terminal transparent
  foreground: "#cdd6f4"
  context_fg: "#89b4fa"

status_line:
  background: ""                       # empty = terminal transparent
  foreground: "#89b4fa"

status:
  running: "#a6e3a1"
  pending: "#f9e2af"
  error: "#f38ba8"
  unknown: "#7f849c"                   # Catppuccin overlay1 (v1.7.5)
```

## Requirements

- **kubectl** on `$PATH` (for edit, delete, and shell exec)
- A valid **kubeconfig** (`~/.kube/config` or `$KUBECONFIG`)
- A running Kubernetes cluster
- **A Nerd Font Mono variant** for the terminal (e.g. JetBrains Mono Nerd Font Mono, FiraCode Nerd Font Mono). kbu's popup titles + row markers use Nerd Font glyphs from the Material Design icon range; the Mono variants are designed to render every glyph at exactly 1 cell, so column / border alignment is stable. The proportional (non-Mono) variants and terminals running East-Asian-Ambiguous=double (some tmux + iTerm2 CJK setups) can paint these glyphs at 2 cells — kbu still works, but helm-managed rows + popup top borders may sit 1 cell off-grid. Switch to the Mono variant or set ambiguous-width=single if you see drift.

## License

[GPL-3.0](LICENSE)
