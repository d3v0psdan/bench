# Bench UI/UX standard

The rules every screen in `app/src` follows. A change that breaks one of them is not
done. Each rule says why (with a source), how to meet it, and how it is checked:

- **lint**: `bun run lint:ui` fails the build (`app/scripts/ui-lint.ts`, run in CI).
- **review**: rules a script can't check are checked by a reviewer before a UI change is called done.

The standard grew out of a GUI audit of the app (`docs/ui-audit.md`, since closed; it is in git history).

## 1. Consent and control

**C1. Confirm before anything costly or hard to undo.** Downloads and installs,
deleting anything, removing a site, stopping Bench, anything that asks for admin rights,
deleting mail. The confirm is a `ConfirmAction` dialog that names the object and lists
what will happen (version, size, location, what stops, what is lost), with a specific
verb on the button ("Install PHP 8.4", "Delete mysql"), never "OK" or "Yes".
*Why:* NN/g, Confirmation Dialogs Can Prevent User Errors (If Not Overused),
https://www.nngroup.com/articles/confirmation-dialog/ . *Check:* lint (consequential
endpoints and `stop`/`runSetup` only in files that render `ConfirmAction`), review.

**C2. Don't confirm what is easy to reverse.** Toggles (autostart, theme), switching a
site's PHP version, starting or stopping one service: act at once and make the reverse
one click away (the same control, or an "Undo" toast action). Confirming everything
trains people to click Yes without reading. *Why:* same NN/g article. *Check:* review.

**C3. Every long operation can be cancelled, and cancel really stops it.** Anything that
can run past a few seconds registers as a daemon task (`GET /api/tasks`) and shows a
cancel button (icon plus tooltip "Cancel") while `task.cancellable` is true. When a step
can't be interrupted safely, the button disappears and the phase says why
("initializing"). *Why:* NN/g, User Control and Freedom,
https://www.nngroup.com/articles/user-control-and-freedom/ . *Check:* review; daemon
tests cover the cancel path.

**C4. Long operations never lock the app.** Dialogs close while work continues ("Keep
going in the background"); progress lives in the Activity tray and page, survives
navigation and window reloads (it comes from the daemon), and finishes with a toast that
has a next-step action. *Why:* NN/g, Progress Indicators Make a Slow System Less
Insufferable, https://www.nngroup.com/articles/progress-indicators/ . *Check:* review.

**C5. Elevated or machine-wide changes list exactly what changes before consent, and can
be undone.** *Why:* mkcert's `-install` / `-uninstall`, https://github.com/FiloSottile/mkcert .
*Check:* review.

## 2. Feedback and truthful status

**F1. Every async surface has all five states:** idle, loading (skeleton for anything
that can take over 300ms), empty (says what belongs here and how to add it), error
(what happened, what to do, a retry), success. A skeleton always resolves. *Why:* NN/g,
Skeleton Screens 101, https://www.nngroup.com/articles/skeleton-screens/ . *Check:* review.

**F2. Feedback within 100ms of every action; pending resolves with a timeout.** Buttons
show a spinner and disable while pending; every request is wrapped in `withTimeout`.
*Why:* NN/g, Response Times,
https://www.nngroup.com/articles/response-times-3-important-limits/ . *Check:* lint
(`api(` calls without `withTimeout`), review.

**F3. Status comes from the daemon, never from client memory.** Setup done, health,
installed versions and progress are derived from API data, so a relaunch shows the
truth. Read counts and lists from each response; a copy kept in state goes stale.
*Why:* NN/g, Visibility of System Status,
https://www.nngroup.com/articles/visibility-system-status/ . *Check:* review.

**F4. Never show a healthy value you haven't checked.** No hard-coded green. If PHP 8.4
is the effective version but not installed, every place that shows it says so and offers
the fix. Global health (the header pill) reflects failing checks, not only that the
daemon answers. *Why:* NN/g heuristic 1 (as F3). *Check:* review.

**F5. Report partial success as partial.** "demo.test linked, but Bench can't serve it
yet" beats "failed" after the site was saved. Refresh client state after every mutation,
success or not. *Why:* NN/g, Error-Message Guidelines,
https://www.nngroup.com/articles/error-message-guidelines/ . *Check:* review.

**F6. Errors are written for a person using this GUI.** Say what failed, why, and the
next step as a button when there is one. No CLI instructions as the only fix, no raw
backticks, no bare file paths (offer "Open logs folder"). Errors render next to the
control that caused them; console-only errors are bugs. *Why:* as F5. *Check:* lint
(silent `void openUrl(` / `void revealItemInDir(`), review.

**F7. A dropped connection reads "Reconnecting", not "stopped".** *Why:* as F3.
*Check:* review.

## 3. Onboarding and guidance

**O1. A first run has one guided path** (Get started checklist on Overview): each step's
state is computed from the daemon, each step has its action inline, progress shows as
"n of N", and the guide collapses (never vanishes) while a step is incomplete. *Why:*
Apple HIG, Onboarding,
https://developer.apple.com/design/human-interface-guidelines/onboarding . *Check:* review.

**O2. Dismissible means informational.** Tips and notices can be dismissed for good.
A notice about something still broken can't be dismissed, or comes back when the broken
state changes. *Why:* Windows InfoBar guidance,
https://learn.microsoft.com/en-us/windows/apps/design/controls/infobar . *Check:* review.

**O3. Explain a known conflict once, where it is hit** (Laravel Herd holding ports):
what conflicts, what Bench changed on its own, what to do, and a "Check again" button.
*Check:* review.

## 4. Organization

**N1. Every object opens a detail view from the row itself** (service, PHP version,
check, message; sites are pages). The row stays scannable; the detail shows everything
the API knows, with copy buttons and actions. Controls inside the row stop propagation.
*Why:* NN/g, Progressive Disclosure,
https://www.nngroup.com/articles/progressive-disclosure/ . *Check:* review.

**N2. One home per object; other appearances link to it.** *Why:* NN/g, Consistency and
Standards, https://www.nngroup.com/articles/consistency-and-standards/ . *Check:* review.

**N3. One identifier per object across surfaces** (a site is its host, `demo.test`),
one status vocabulary (Running, Stopped, Failed, and for checks OK, Warning, Failing),
one label per action. *Check:* review.

**N4. Every daemon capability has a GUI home** (the daemon API rule goes both ways).
*Check:* review.

**N5. The command palette reaches every page and object and runs the main actions.**
*Check:* review.

## 5. Visual and layout

**V1. Tokens only.** Colors come from `index.css` tokens (`bg-card`, `text-success`,
`shadow-card`); no hex values or Tailwind palette colors in components. *Check:* lint.

**V2. One primary action per view.** `brand` (blue) is the page's one main action;
`default` (black) confirms inside dialogs; per-row actions are `outline` `size="sm"` or
icon buttons. *Why:* Apple HIG, Buttons,
https://developer.apple.com/design/human-interface-guidelines/buttons . *Check:* review.

**V3. Alignment.** Controls in a row share one vertical center; actions in a card or alert
are centered on the card, not pinned to its top; left edges of titles, cards and lists
line up; page content spans the same width on every page. *Check:* review, screenshot.

**V4. Layout uses flex/grid gaps** (`gap-*`), never `space-x-*` / `space-y-*`.
*Check:* lint.

**V5. Icons are Phosphor**, regular weight, sized by the component. No shield icons.
*Check:* lint.

**V6. Menus size to their content**; no item wraps. *Check:* review.

**V7. Lean page headers.** A title, its live status beside it, actions on the right. No
tagline under the title. Secondary details (ports, addresses, .env lines) live in the
object's detail panel, not in page toolbars. *Check:* review, screenshot.

**V8. Layouts follow their container, not the window** (`@container` queries): the
sidebar may be open or closed. Every page works from the 760px minimum window. Rows
truncate with an ellipsis; don't wrap truncating rows in `ScrollArea` (its table
layout defeats `truncate`), use a native `overflow-y-auto`. *Check:* review, screenshots
at 760px, about 820px with the sidebar open, and full width.

## 6. Accessibility

**A1. Contrast:** text 4.5:1, large text and UI parts (focus rings, switch tracks,
borders that carry meaning) 3:1, in both themes. *Why:* WCAG 2.2 SC 1.4.3 and 1.4.11.
*Check:* review against tokens.

**A2. Severity is never color alone:** always a word or an icon too. *Why:* WCAG 2.2 SC
1.4.1. *Check:* review.

**A3. Everything works by keyboard** with a visible focus ring; icon buttons have
labels; targets are at least 24px. *Why:* WCAG 2.2 SC 2.1.1, 2.4.7, 2.5.8. *Check:* review.

**A4. Information isn't hover-only.** A failing check shows its reason as text, not only
in a tooltip. *Why:* WCAG 2.2 SC 1.4.13. *Check:* review.

**A5. Motion respects `prefers-reduced-motion`**, and transitions stay within 100 to
300ms. *Why:* WCAG 2.2 SC 2.3.3. *Check:* review.

**A6. Live regions:** fresh errors are `role="alert"`; persistent notices use
`role="status"` or none. A lasting red alert passes `role="status"`, because
`variant="destructive"` defaults to `alert`. *Why:* WAI-ARIA APG Alert pattern.
*Check:* review.

## 7. Desktop app

**D1. Closing the window keeps Bench in the tray;** the tray shows status and the top
actions. **D2.** One instance; the window remembers its size. **D3.** No browser leaks
(context menu, reload). **D4.** Bench can start at sign-in and brings sites back.
*Why:* Tauri system tray, single-instance and autostart plugins,
https://v2.tauri.app/learn/system-tray/ . *Check:* review.

## 8. Copy

**W1. Sentence case; one verb per action everywhere** ("Try again", "Copy .env").
The labels, one per action: **Try again**, **Copy .env**, **New service**, **Add site**
(then **New Laravel app**, **Link a project**, **Park a folder**, **Proxy a local port**),
**Start with Bench** (a service's autostart), **Open site**, **Open in editor**, **Open
terminal here**, **Open database**, **Open console**, **Open in client**, **Show folder**,
**Show file**, **Show log file**, **Open logs folder**, **Update .env**, **Delete site**.
**W2.** Users' words, not internals ("Bench", not "benchd", outside About).
**W3.** No em-dashes. **W4.** No native `alert()` / `confirm()`.
*Why:* Microsoft Writing Style Guide, https://learn.microsoft.com/en-us/style-guide/capitalization .
*Check:* lint (W3, W4), review.

## Shared building blocks

Meet the rules with these instead of re-implementing them:

| Need | Use |
|---|---|
| Confirm a consequential action (C1) | `ConfirmAction` (`components/confirm-action.tsx`) |
| Show a running task with cancel (C3, C4) | `TaskProgress`, the Activity tray and page (`components/activity.tsx`) |
| Section of settings rows | `SectionCard` + `SectionRow` (`components/page.tsx`) |
| Status word with dot (A2, N3) | `StatusDot`, `CHECK_TONES` / `CHECK_LABELS` (`components/page.tsx`) |
| Informational notice (O2) | `DismissibleAlert` (`components/page.tsx`) |
| Open a URL or folder with a visible failure (F6) | `openExternal`, `revealPath` (`lib/open.ts`) |
| Request with a timeout (F2) | `withTimeout` (`lib/async.ts`) |
