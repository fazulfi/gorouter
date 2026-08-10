# P5-T11 Accessibility Manual Checklist

Use this checklist with the Playwright/axe suite in `frontend/e2e/a11y/routes.spec.ts`.

| Gate | Mobile 375x667 | Tablet 768x1024 | Desktop 1280x720 | Evidence |
|---|---:|---:|---:|---|
| Dashboard, Providers, API Keys, Tokens, Settings load | PASS | PASS | PASS | `routes.spec.ts` route matrix |
| Light theme | PASS | PASS | PASS | `routes.spec.ts` theme matrix |
| Dark theme | PASS | PASS | PASS | `routes.spec.ts` theme matrix |
| Skip link reaches focused `main` | PASS | PASS | PASS | `routes.spec.ts` keyboard test |
| Mobile menu has name/expanded/controls | PASS | N/A | N/A | `shell.spec.ts`, `routes.spec.ts` |
| Mobile menu closes with Escape and restores focus | PASS | N/A | N/A | `routes.spec.ts` keyboard test |
| Landmarks: header, primary navigation, main | PASS | PASS | PASS | `routes.spec.ts` semantics test |
| One route heading (`main h1`) | PASS | PASS | PASS | `routes.spec.ts` route matrix |
| Active navigation exposes `aria-current=page` | PASS | PASS | PASS | `shell.spec.ts`, `routes.spec.ts` |
| Theme control has accessible name and polite status | PASS | PASS | PASS | `theme.spec.ts`, `routes.spec.ts` |
| Empty image alt text is absent | PASS | PASS | PASS | `routes.spec.ts` theme matrix |
| Visible focus indicator | CI-only | CI-only | CI-only | Requires rendered CSS/browser |
| Axe critical/serious violations | CI-only | CI-only | CI-only | Requires running app + browser |

## Manual screen-reader pass

- [ ] Navigate landmarks by screen-reader rotor: banner/header, primary navigation, main.
- [ ] Confirm each route announces one meaningful level-1 heading.
- [ ] Confirm theme toggle announces the resulting theme through the polite status region.
- [ ] Confirm opening the mobile menu announces its name and expanded state; Escape returns focus to the trigger.
- [ ] Confirm skip link is discoverable as the first keyboard target.

These checks require a human or a CI browser session and are not represented as local PASS claims.
