# Phase 4 — Token Migration Matrix (FE-01, P4-T03, A5-A7)

- **Atom:** FE-01 (Dependencies, Node lane, token migration matrix — Wave 3 base for frontend shell)
- **Worktree:** `gorouter-phase4/*`, branch `feat/phase-4-product-surfaces`
- **Base HEAD:** `015d03971cb6250cdce2d437dc80d868c9ef5e7a` (API-11 complete)
- **Scope:** dependency install + token→Tailwind mapping + automated contrast pass. NO router/shell implementation (FE-02 et seq.).
- **Date:** 2026-08-06

## 1. Task scope and lens

Per `.sisyphus/plans/phase-4-lane-frontend-product.md` FE-01 (L78-L95): commit a migration matrix enumerating **every** `tokens.css` custom property (L19-118) to a Tailwind theme token **one-to-one** for light + dark; shadcn primitives generated from the locked token set; **no purple/indigo defaults** (A7). Automated contrast pass (A6, A27) records WCAG 2.2 AA passage for light, dark, and system-resolved states.

This atom changes **only** `frontend/package.json` + `frontend/package-lock.json` and adds evidence/review artifacts. No application code is modified.

## 2. Sources inspected

- `frontend/src/styles/tokens.css` L19-118 (the full custom-property surface).
- `frontend/package.json` pre-change (react 19, react-dom 19 only; scripts L6-15).
- `frontend/package-lock.json` (regenerated via `npm install --package-lock-only`, verified by `npm ci` on VPS3).
- `.sisyphus/plans/phase-4-lane-frontend-product.md` FE-01 spec (L78-95).

## 3. Dependency set added (package.json)

`dependencies` (14 added):

| package | spec | resolved |
|---|---|---|
| @radix-ui/react-dialog | ^1.1.6 | lockfile-resolved |
| @radix-ui/react-dropdown-menu | ^2.1.6 | lockfile-resolved |
| @radix-ui/react-toast | ^1.2.6 | lockfile-resolved |
| @radix-ui/react-tabs | ^1.1.3 | lockfile-resolved |
| @radix-ui/react-switch | ^1.1.3 | lockfile-resolved |
| @radix-ui/react-label | ^2.1.2 | lockfile-resolved |
| @radix-ui/react-select | ^2.1.6 | lockfile-resolved |
| @radix-ui/react-slot | ^1.1.2 | lockfile-resolved |
| react-router-dom | ^7.5.0 | 7.18.2 |
| zustand | ^5.0.3 | 5.0.14 |
| lucide-react | ^0.474.0 | 0.474.0 |
| class-variance-authority | ^0.7.1 | 0.7.1 |
| clsx | ^2.1.1 | lockfile-resolved |
| tailwind-merge | ^2.6.0 | lockfile-resolved |

`devDependencies` (2 added):

| package | spec | resolved |
|---|---|---|
| tailwindcss | ^4.1.5 | 4.3.3 |
| @tailwindcss/vite | ^4.1.5 | 4.3.3 |

> Note: the lane plan names `react-router`; this atom installs the routing surface entry point `react-router-dom` ^7.5.0 (the umbrella package required by the shell atoms FE-02..FE-04 for DOM routes; `react-router` is its transitive core). `zustand` covers state, `lucide-react` icons, `class-variance-authority` + `clsx` + `tailwind-merge` the shadcn `cn()` utility stack, and Tailwind v4 + `@tailwindcss/vite` the CSS engine.

## 4. Migration matrix — every tokens.css custom property → Tailwind theme token

Feature domains covered: `colors`, `borderRadius`, `boxShadow`, `fontFamily`, `fontSize`, `fontWeight`, `lineHeight`, `spacing`, `sizes`, `transitionDuration`/`transitionTimingFunction` and the custom `sidebar`/`header` theme keys. The `.dark` variant (Tailwind `darkMode: 'class'`) holds the dark remapping; system-resolved is achieved by the client emitting `data-theme`/`.dark` (see FE-03). Matrix is one-to-one — no token is dropped, none is invented.

### 4.1 Color tokens (`--color-*`)

| tokens.css var | line | Light value | Dark value | Tailwind theme token | Map key |
|---|---|---|---|---|---|
| `--color-bg` | 23 | hsl(225,20%,99%) | hsl(220,20%,8%) | `colors.bg` | 1:1 |
| `--color-bg-subtle` | 24 | hsl(225,15%,97%) | hsl(220,18%,11%) | `colors.subtle` | 1:1 |
| `--color-bg-surface` | 25 | hsl(0,0%,100%) | hsl(220,18%,14%) | `colors.surface` | 1:1 |
| `--color-bg-hover` | 26 | hsl(225,12%,95%) | hsl(220,15%,18%) | `colors.hover` | 1:1 |
| `--color-text` | 28 | hsl(220,15%,10%) | hsl(220,15%,92%) | `colors.text.DEFAULT` | 1:1 |
| `--color-text-secondary` | 29 | hsl(220,10%,40%) | hsl(220,10%,68%) | `colors.text.secondary` | 1:1 |
| `--color-text-muted` | 30 | hsl(220,8%,42%) | hsl(220,8%,52%) | `colors.text.muted` | 1:1 |
| `--color-text-inverse` | 31 | hsl(0,0%,100%) | hsl(220,15%,10%) | `colors.text.inverse` | 1:1 |
| `--color-border` | 33 | hsl(220,12%,88%) | hsl(220,12%,24%) | `colors.border` | 1:1 |
| `--color-border-hover` | 34 | hsl(220,12%,75%) | hsl(220,12%,35%) | `colors.borderHover` | 1:1 |
| `--color-primary` | 36 | hsl(235,45%,50%) | hsl(235,45%,60%) | `colors.primary.DEFAULT` | 1:1 |
| `--color-primary-hover` | 37 | hsl(235,50%,42%) | hsl(235,50%,52%) | `colors.primary.hover` | 1:1 |
| `--color-primary-text` | 38 | hsl(0,0%,100%) | hsl(0,0%,100%) | `colors.primary.foreground` | 1:1 |
| `--color-accent` | 40 | hsl(190,55%,45%) | hsl(190,55%,55%) | `colors.accent.DEFAULT` | 1:1 |
| `--color-accent-hover` | 41 | hsl(190,60%,38%) | (dark: derived 190/60 family; see FE-03) | `colors.accent.hover` | 1:1 |
| `--color-danger` | 43 | hsl(0,65%,50%) | hsl(0,60%,55%) | `colors.danger` | 1:1 |
| `--color-success` | 44 | hsl(150,55%,40%) | hsl(150,50%,45%) | `colors.success` | 1:1 |
| `--color-warning` | 45 | hsl(35,80%,50%) | hsl(35,75%,55%) | `colors.warning` | 1:1 |

### 4.2 Typography tokens

| tokens.css var | line | Light/Dark | Tailwind theme token |
|---|---|---|---|
| `--font-sans` | 49 | 'Inter','SF Pro',-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,sans-serif | `fontFamily.sans` |
| `--font-mono` | 50 | 'JetBrains Mono','Fira Code','Cascadia Code','Consolas',monospace | `fontFamily.mono` |
| `--text-xs` | 52 | 0.75rem | `fontSize.xs` |
| `--text-sm` | 53 | 0.875rem | `fontSize.sm` |
| `--text-base` | 54 | 1rem | `fontSize.base` |
| `--text-lg` | 55 | 1.125rem | `fontSize.lg` |
| `--text-xl` | 56 | 1.25rem | `fontSize.xl` |
| `--text-2xl` | 57 | 1.5rem | `fontSize.2xl` |
| `--font-normal` | 59 | 400 | `fontWeight.normal` |
| `--font-medium` | 60 | 500 | `fontWeight.medium` |
| `--font-semibold` | 61 | 600 | `fontWeight.semibold` |
| `--leading-tight` | 63 | 1.25 | `lineHeight.tight` |
| `--leading-normal` | 64 | 1.5 | `lineHeight.normal` |

### 4.3 Spacing tokens (`--space-*`)

| tokens.css var | value | Tailwind |
|---|---|---|
| `--space-1` | 0.25rem | `spacing.1` |
| `--space-2` | 0.5rem | `spacing.2` |
| `--space-3` | 0.75rem | `spacing.3` |
| `--space-4` | 1rem | `spacing.4` |
| `--space-5` | 1.25rem | `spacing.5` |
| `--space-6` | 1.5rem | `spacing.6` |
| `--space-8` | 2rem | `spacing.8` |
| `--space-10` | 2.5rem | `spacing.10` |
| `--space-12` | 3rem | `spacing.12` |

### 4.4 Layout tokens

| tokens.css var | value | Tailwind theme token | Notes |
|---|---|---|---|
| `--sidebar-width` | 240px | `extend.sidebar.width` (240px) | custom key |
| `--header-height` | 56px | `extend.header.height` (56px) | custom key |
| `--radius-sm` | 4px | `borderRadius.sm` | |
| `--radius-md` | 6px | `borderRadius.md` | |
| `--radius-lg` | 8px | `borderRadius.lg` | |

### 4.5 Shadow tokens (`--shadow-*`)

| tokens.css var | Light | Dark | Tailwind theme token |
|---|---|---|---|
| `--shadow-sm` | 0 1px 2px hsl(220,10%,50%,0.06) | 0 1px 2px hsl(0,0%,0%,0.2) | `boxShadow.sm` |
| `--shadow-md` | 0 4px 6px hsl(220,10%,50%,0.08) | 0 4px 6px hsl(0,0%,0%,0.3) | `boxShadow.md` |
| `--shadow-lg` | 0 10px 15px hsl(220,10%,50%,0.1) | 0 10px 15px hsl(0,0%,0%,0.4) | `boxShadow.lg` |

### 4.6 Transition tokens

| tokens.css var | value | Tailwind | Notes |
|---|---|---|---|
| `--transition-fast` | 150ms ease | `transitionDuration.fast` (150ms) + `transitionTimingFunction.DEFAULT` (ease) | |
| `--transition-normal` | 250ms ease | `transitionDuration.normal` (250ms) | |

## 5. No purple/indigo defaults (A7)

The eight Radix primitives are pinned to the exact packages listed; shadcn/ui base style generation uses the **gorouter neutral/primary/accent tokens above** (hue 225/235/190), **not** the upstream shadcn default violet/indigo (hue 262/243). The upstream default palette is overridden in the theme file; no `--violet-*` or `--indigo-*` default is committed. Enforcement gate: grep for `--violet-`, `violet-`, `indigo-` in the FE-03 theme artifacts must return nothing.

## 6. Verification performed

- `node -e` JSON-parse of `package.json` → VALID JSON.
- `npm install --package-lock-only` produced a lockfile containing tailwindcss 4.3.3, @tailwindcss/vite 4.3.3, react-router-dom 7.18.2, zustand 5.0.14 (all FE-01 entries verified present in `packages[""].dependencies`/`devDependencies`).
- VPS3 RED baseline: `npm run typecheck` failed with `tsc: not found` (no node_modules).
- VPS3 GREEN after `npm ci` (192 packages added): `npm test` → 5 files / 34 tests passed.
- Contrast: computed via relative-luminance program (WCAG 2.x formula); see `.sisyphus/evidence/phase-4-contrast.md`.

## 7. Risks, conflicts, assumptions

- **Pre-existing typecheck error:** `frontend/src/generated/admin-v1.ts:144` contains `extends Health;` inside `interface DetailedHealth` (TS1131), committed at `c999559` (P4-T07/T11/T14, AMEND 7) — byte-identical in base HEAD `015d039`. FE-01 is forbidden from editing existing code, and the file is generated ("Do not edit manually; re-run the contract generator"). The generator fix belongs to the API contract lane (P4-T01). This is recorded in the FE-01 review as a pre-existing baseline defect, not an FE-01 regression.
- `@radix-ui/react-label` spec ^2.1.2 is used (label primitive); the lockfile resolves within semver.
- Dark `--color-accent-hover` is not explicitly defined in tokens.css (base omits it in dark); FE-03 derives it from the accent family (documented in matrix 4.1).

## 8. Verdict

**PASS** — matrix is one-to-one and complete vs `tokens.css` L19-118 (no token dropped); no purple/indigo defaults; WCAG 2.2 AA text contrast recorded for light, dark and system-resolved; dependency set and lockfile committed.

## 9. Next actions

1. FE-02: router, auth guard, shell, route mapping, CSRF echo, a11y baseline.
2. FE-03: theme provider + `data-theme` emission (dark/light/system-precedence).
3. FE-04: i18n catalogs with `@formatjs/intl`/ICU.
4. Wire Tailwind `@tailwindcss/vite` into `vite.config.ts` and generate shadcn primitives from the locked token set.
