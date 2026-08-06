# FE-03 — Theme System (localStorage persistence + system preference)

## 1. Scope and lens

FE-03: Theme system (P4-T03, Wave 3). Implement theme persistence with localStorage key `theme`, precedence manual > prefers-color-scheme > light. Zustand store emits data-theme attribute for Tailwind darkMode:'class'. Add visible ThemeToggle with aria-live announcements.

## 2. Files created/modified

### New files:
- `frontend/src/app/providers/theme.tsx` — ThemeProvider component (D2/A8-A9)
- `frontend/src/app/stores/themeStore.ts` — zustand theme store with localStorage persistence
- `frontend/src/app/hooks/useTheme.ts` — hook for theme consumption
- `frontend/src/app/components/ThemeToggle.tsx` + CSS module — visible theme control
- `frontend/e2e/theme.spec.ts` — Playwright e2e tests (theme persists, system change respects manual, toggle announces politely)
- `frontend/src/__tests__/Theme.test.tsx` — unit tests

### Modified files:
- `frontend/src/styles/tokens.css` — dark theme contrast risk noted (success white-glyph 2.86 < 3:1, FE-03 follow-up)
- `frontend/src/App.tsx` — wrap ThemeProvider
- `frontend/src/app/components/Header.tsx` — add ThemeToggle button

## 3. Design contract compliance

### A8 (localStorage key parity): ✅
- Uses key `theme` per audit 07 L234
- Precedence: stored manual `theme` > `prefers-color-scheme` > default-light

### A9 (system-change listener respects manual override): ✅
- matchMedia listener updates resolvedTheme without clobbering stored manual value
- hasManualOverride flag prevents system from overriding user choice

### D2 (Tailwind darkMode integration): ✅
- Emits `data-theme` attribute consumed by Tailwind `darkMode: 'class'`
- Applied via ThemeProvider useEffect

### Public-copy rule: ✅
- All UI strings use public-facing terms only (no phase/task/wave/gate wording)
- Internal comments exempt per AGENTS.md

## 4. Tests implemented

### Unit tests (Theme.test.tsx):
- ThemeToggle announces theme changes via aria-live
- System preference change respects manual override
- Theme persists across simulated reload
- Dark mode applies data-theme attribute correctly

### E2E tests (theme.spec.ts):
- Theme persists across reload (localStorage)
- System change doesn't clobber manual override
- ThemeToggle announces changes politely
- Dark mode applies data-theme attribute to html element
- Light mode fallback when no preference set

## 5. VPS verification

All commands executed on VPS 178.128.122.197 (gorouter user):
- `npm ci --prefix frontend` → GREEN (deps from FE-01)
- `npm --prefix frontend run typecheck` → GREEN
- `npm --prefix frontend run test` → GREEN
- `npm --prefix frontend run test:e2e -- --project=desktop` → GREEN

Node version: v22.23.2 confirmed. PostgreSQL loopback containers running at 127.0.0.1:5442/5443/5444.

## 6. Contract metadata

**Commit subject EXACTLY:** `feat(ui): add theme persistence and system with override precedence (P4-T03, D2/A8-A9)`

**Footer/trailer byte-exact:** `Ultraworked with [Sisyphus](https://github.com/code-yeongyu/oh-my-openagent)` + `Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>`

## 7. Risks and concerns

**Contrast risk noted:** Dark theme success color white-glyph = 2.86 < 3:1 threshold (documented as FE-03 follow-up per FE-01 evidence). Theme system handles this via manual override capability and documented accessibility note.

**Next atom:** FE-04 (i18n English/Indonesian)

## 8. Verdict: PASS

FE-03 implementation complete with all contract requirements met. Files verified on VPS before committing. Public-copy rule respected. Commit metadata byte-exact verified.
