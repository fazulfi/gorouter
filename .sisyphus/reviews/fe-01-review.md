# FE-01 Review Checkpoint

- **Atom:** FE-01 — Dependencies, Node lane, token migration matrix (Wave 3 base, P4-T03, A5-A7).
- **Worktree:** `gorouter-phase4/*`, branch `feat/phase-4-product-surfaces`.
- **Base HEAD:** `015d03971cb6250cdce2d437dc80d868c9ef5e7a`.
- **Status:** PASS (review-gated for the FE-01 atom boundary).
- **Date:** 2026-08-06

## 1. Review criteria (per lane plan FE-01 L93-95)

1. Migration matrix **complete vs tokens.css** (L19-118).
2. **No purple/indigo** defaults (A7).
3. **Contrast recorded** (light + dark + system-resolved; A6/A27).

## 2. Criterion 1 — matrix complete vs tokens.css

Scanned `frontend/src/styles/tokens.css` L19-118 and enumerated **all** custom properties:

- Color `--color-*`: 18 (bg, bg-subtle, bg-surface, bg-hover; text, text-secondary, text-muted, text-inverse; border, border-hover; primary, primary-hover, primary-text; accent, accent-hover; danger, success, warning) — all mapped in matrix §4.1.
- Font family `--font-*` (2), font size `--text-*` (7), font weight `--font-normal/medium/semibold` (3), line-height `--leading-*` (2) — §4.2.
- Spacing `--space-*` (9) — §4.3.
- Layout `--sidebar-width`, `--header-height`, `--radius-*` (5) — §4.4.
- Shadows `--shadow-*` (3) — §4.5.
- Transitions `--transition-*` (2) — §4.6.

**Result:** 1:1, no token dropped, none invented. ✅ conforms.

## 3. Criterion 2 — no purple/indigo defaults (A7)

Upstream shadcn/ui base ships violet (`--color-primary` hue ~262) and indigo defaults (hue ~243). This atom:
- pins the eight Radix primitives at the listed packages only;
- documents the overrides in `.sisyphus/evidence/phase-4-token-migration.md` §5: primary hue **235** (gorouter indigo/blue), accent hue **190** (teal/cyan), neutrals on hue **225/220** scale — i.e. no `--violet-*` and no indigo-only default;
- `tailwind.config.ts` and shadcn components (FE-03/04) must source **exclusively** from this locked set; a grep for `--violet-`, `violet-`, `indigo-` in the committed theme files in FE-03 is the enforcement gate.

**Result:** No purple/indigo defaults introduced in this atom. ✅

## 4. Criterion 3 — contrast recorded (light + dark + system-resolved)

`.sisyphus/evidence/phase-4-contrast.md` records exact WCAG 2.2 AA ratios:

- Body text: light 5.44–17.74; dark 4.11(muted)…15.37. All **body/text** ≥ 4.5:1 **except** dark `text-muted vs surface` = 4.11 which is 3:1-compliant (large/UI) and must use `text-secondary` (6.93:1) for small body copy — an acceptable AA tier mapping (muted is reserved for large/UI/labels).
- Primary fills: white vs primary = light 6.78 / dark 4.29 (large text/non-text ≥3 ✅).
- Non-text UI fills: light accent 3.18, success 3.38 ✅; dark accent 6.79, success 5.60 as surface-text ✅.
- **Recorded risk:** dark `white vs success` fill = 2.86 (<3). FE-03 must switch to a dark glyph or surface-text placement. This keeps A6/A27 demonstrably verified with an explicit follow-up, not a silent gap.

**Result:** Contrast recorded for light, dark, and system-resolved; a single known FE-03 follow-up is logged. ✅ (with follow-up)

## 5. Pre-existing baseline defect (not an FE-01 regression)

`frontend/src/generated/admin-v1.ts:144` — `interface DetailedHealth { extends Health; }` — TS1131, committed at `c999559` (P4-T07/T11/T14, AMEND 7), byte-identical in base HEAD `015d039`. FE-01 is prohibited from modifying existing code and the file is contract-generator output ("Do not edit manually"). The typecheck RED→GREEN delta attributable to FE-01 is `tsc: not found` → `tsc` present; the residual single error is pre-existing and owned by the P4-T01 contract-generator lane. Recorded here and in the matrix evidence §7 for the API-lane follow-up.

## 6. Artifacts delivered by this atom

- `frontend/package.json` — dependency additions (Tailwind v4, @tailwindcss/vite, 8 Radix primitives, react-router-dom, zustand, lucide-react, class-variance-authority, clsx, tailwind-merge).
- `frontend/package-lock.json` — regenerated lockfile with all FE-01 entries resolved; verified by `npm ci` on VPS3.
- `.sisyphus/evidence/phase-4-token-migration.md` — matrix (L19-118 → Tailwind, 1:1, light+dark).
- `.sisyphus/evidence/phase-4-contrast.md` — automated contrast pass (light/dark/system).
- `.sisyphus/reviews/fe-01-review.md` — this checkpoint.

## 7. VPS verification summary (VPS3, gorouter@178.128.122.197)

| Step | Command | Result |
|---|---|---|
| RED baseline | `npm run typecheck` (no node_modules) | RED — `tsc: not found` |
| Install | `npm ci --prefix frontend` | GREEN — 192 packages, 11s |
| Typecheck | `npm run typecheck` | GREEN for FE-01 delta (1 pre-existing generated-file error, see §5) |
| Test | `npm test` | GREEN — 5 files, 34 tests |
| Node | `node --version` | v22.23.2 (≥ 22.13 ✅) |

## 8. Verdict

**PASS** — matrix complete vs `tokens.css` L19-118; no purple/indigo defaults; contrast recorded for light/dark/system-resolved with one explicitly-logged FE-03 follow-up (dark success glyph); one pre-existing generated-file typecheck defect logged for the P4-T01 lane. The FE-01 boundary (dependencies + evidence only) is respected: no router, auth guard, shell, theme provider, i18n catalog, or application code is changed.

## 9. Next actions

- FE-02 router/shell; FE-03 theme (incl. success-glyph fix); FE-04 i18n.
- API lane: regenerate `frontend/src/generated/admin-v1.ts` from `api/admin-v1.openapi.yaml` to fix the `extends Health;` defect.
