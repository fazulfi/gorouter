# Audit: Upstream 9Router "Recent Requests" Behavior

**Commit:** `79918c7830695bbca4a45c9fea4a42c3e9fd73d1` (v0.5.40, 2026-07-20)  
**Date:** 2026-07-29  
**Auditor:** Sisyphus-Junior (source-only, no runtime)  

---

## Purpose

Verify every observable property of the upstream "Recent Requests" system — data
source, delivery mechanism, buffer limits, ordering, insertion, scroll behavior,
pause/follow mechanics — so the gorouter "Latest 50 / smart-follow" design can
be grounded in confirmed facts rather than assumptions.

---

## 1. Files and Symbols

### 1.1 Primary data layer

| File | Role |
|------|------|
| `src/lib/db/repos/usageRepo.js` | All usage persistence, ring buffer, `getActiveRequests()`, `getUsageStats()`, `getRecentLogs()` |
| `src/lib/db/index.js` | Barrel re-export (lines 62: `appendRequestLog, getRecentLogs`) |
| `src/lib/driver.js` | SQLite adapter (referenced via `getAdapter()`) |

### 1.2 API routes

| File | Role |
|------|------|
| `src/app/api/usage/stream/route.js` | SSE endpoint — real-time push for active + recent requests |
| `src/app/api/usage/stats/route.js` | REST GET — full stats with `period` param |
| `src/app/api/usage/logs/route.js` | REST GET — calls `getRecentLogs(200)` |
| `src/app/api/usage/request-logs/route.js` | REST GET — calls `getRecentLogs(200)` (identical to logs) |

### 1.3 Frontend components

| File | Role |
|------|------|
| `src/shared/components/UsageStats.js` | Main dashboard component — contains `RecentRequests` sub-component (lines 42-83), SSE client (lines 279-300) |
| `src/shared/components/RequestLogger.js` | Separate "Request Logs" tab — polling-based |

### 1.4 Key symbols

| Symbol | File | Purpose |
|--------|------|---------|
| `RING_CAP` | `usageRepo.js:27` | `const RING_CAP = 50` |
| `global._recentRing` | `usageRepo.js:25` | `{ items: [], initialized: false }` |
| `recentRing` | `usageRepo.js:32` | Local alias for the global |
| `pushToRing(entry)` | `usageRepo.js:100-104` | Appender + FIFO trim |
| `ensureRingInitialized()` | `usageRepo.js:119-128` | Lazy hydrate from `usageHistory` table |
| `getActiveRequests()` | `usageRepo.js:216-238` | Builds `{ activeRequests, recentRequests, errorProvider }` |
| `statsEmitter` | `usageRepo.js:21` | `EventEmitter` — drives SSE push |
| `scheduleStatsEvent()` | `usageRepo.js:43-51` | Debounced emit (150ms pending, 250ms update) |
| `saveRequestUsage(entry)` | `usageRepo.js:241-311` | Transactional DB insert + `scheduleStatsEvent` |
| `sendPending` / `send` | `stream/route.js:17-38` | SSE push callbacks |

---

## 2. Data Source

### 2.1 In-memory ring buffer (`recentRing`)

**Confirmed** — `usageRepo.js:25`, `usageRepo.js:100-104`:

```js
// usageRepo.js:25
if (!global._recentRing) global._recentRing = { items: [], initialized: false };
// usageRepo.js:32
const recentRing = global._recentRing;
// usageRepo.js:100-104
function pushToRing(entry) {
  recentRing.items.push(entry);
  if (recentRing.items.length > RING_CAP) {
    recentRing.items = recentRing.items.slice(-RING_CAP);
  }
}
```

The ring is a global in-process array (Next.js hot-module scope). It is **not**
persisted to disk — survives hot reloads but lost on server restart.

**Initialization** (`usageRepo.js:119-128`): on first call to `getActiveRequests()`,
reads the last `RING_CAP` rows from `usageHistory` and populates the ring
(reversed to chronological order). Only fires once (`recentRing.initialized` guard).

### 2.2 SQLite `usageHistory` table

Used directly in two places:

- **`getUsageStats()`** (`usageRepo.js:371-402`): `SELECT … FROM usageHistory ORDER BY id DESC LIMIT 100`, then dedup + `slice(0, 20)` for its own `recentRequests` field.
- **`getRecentLogs(limit = 200)`** (`usageRepo.js:742-769`): `SELECT … FROM usageHistory ORDER BY id DESC LIMIT ?`, formats rows as pipe-delimited text strings.

### 2.3 Sources compared

| Surface | Backing store | Max rows queried | Final count |
|---------|--------------|------------------|-------------|
| Dashboard Recent Requests card | Ring buffer | 50 (RING_CAP) | 20 (after dedup + slice) |
| Usage Stats REST (`/api/usage/stats`) | SQLite `usageHistory` | 100 | 20 (after dedup + slice) |
| Request Logger REST (`/api/usage/request-logs`) | SQLite `usageHistory` | 200 | 200 (no dedup) |
| Pxpipe History | File `events.jsonl` | 100 (default, max 500) | variable |

---

## 3. Polling / SSE Mechanism

### 3.1 Dashboard SSE stream (`/api/usage/stream`)

**Confirmed** — `stream/route.js`:

```
On connect:
  1. state.send() → lightweight push (getActiveRequests → activeRequests + recentRequests + errorProvider)
  2. Then full recalc via getUsageStats() → cache in state.cachedStats
  3. Push full stats

On statsEmitter "update" event:
  → state.send() → full recalc again → push

On statsEmitter "pending" event:
  → state.sendPending() → lightweight push (activeRequests + recentRequests only)

Keepalive: 25-second "ping" comments
```

The SSE stream **replaces the entire `recentRequests` array** on every push. It
does not send incremental diffs or individual new entries.

**Client side** (`UsageStats.js:279-300`):

```js
// SSE connection - real-time updates for activeRequests + recentRequests only
useEffect(() => {
  const es = new EventSource("/api/usage/stream");
  es.onmessage = (e) => {
    const data = JSON.parse(e.data);
    setStats((prev) => {
      if (!prev) return prev;
      return {
        ...prev,
        activeRequests: data.activeRequests,
        recentRequests: data.recentRequests,
        errorProvider: data.errorProvider,
        pending: data.pending,
      };
    });
  };
  return () => es.close();
}, []);
```

Only `activeRequests`, `recentRequests`, `errorProvider`, `pending` fields are
merged from SSE. All other stats fields (`byModel`, `byProvider`, totals, etc.)
come from the REST `/api/usage/stats` fetch and are never overwritten by SSE.

### 3.2 Request Logger polling

**Confirmed** — `RequestLogger.js`: `setInterval(fetchLogs, 3000)` with an
"Auto Refresh (3s)" toggle. This is a separate surface from the Recent Requests
card — it lives in the "Request Logs" tab, not the "Overview" tab.

---

## 4. Buffer Size / Limits

### 4.1 Ring buffer cap: 50

**Confirmed** — `usageRepo.js:27`:

```js
const RING_CAP = 50;
```

When `pushToRing` is called and the array exceeds 50, the **oldest** entries are
dropped via `slice(-RING_CAP)`.

### 4.2 Display limit after dedup: 20

**Confirmed** — `usageRepo.js:236`:

```js
.slice(0, 20);
```

This applies after dedup in both `getActiveRequests()` and `getUsageStats()`.
The display never shows more than 20 entries, even though the ring holds 50.

### 4.3 Dedup rules

**Confirmed** — `usageRepo.js:222-236`:

```js
const seen = new Set();
recentRequests = [...recentRing.items]
  .sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp))
  .map(/* extract promptTokens, completionTokens, status */)
  .filter((e) => {
    if (e.promptTokens === 0 && e.completionTokens === 0) return false;
    const minute = e.timestamp ? e.timestamp.slice(0, 16) : "";
    const key = `${e.model}|${e.provider}|${e.promptTokens}|${e.completionTokens}|${minute}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  })
  .slice(0, 20);
```

- **Zero-token filter**: entries where both `promptTokens` and `completionTokens`
  are 0 are excluded.
- **Minute-level dedup**: after sorting newest-first, duplicate entries with the
  same `model|provider|promptTokens|completionTokens|minute` key are collapsed.
  This prevents the same DB-sourced event from appearing twice when the ring is
  hydrated from SQLite and also receives new entries.

### 4.4 Pxpipe limit: 100 / 500

**Confirmed** — `src/app/api/pxpipe/stats/route.js:9`, `src/lib/pxpipe/events.js:123`:

```js
const recentLimit = Math.min(Number(searchParams.get("limit")) || 100, 500);
```

This is a separate system (file-based JSONL, unrelated to the usage ring).

---

## 5. Ordering

### 5.1 Display ordering: newest-first

**Confirmed** in all three code paths:

- `getActiveRequests()`: `[...recentRing.items].sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp))` → newest first.
- `getUsageStats()`: `ORDER BY id DESC LIMIT 100` → newest first.
- `getRecentLogs()`: `ORDER BY id DESC LIMIT ?` → newest first.

The React `RecentRequests` component (`UsageStats.js:42-83`) iterates the array
in order and renders a `<tbody>` with `requests.map((r, i) => …)`, so the first
row in the array is the top row on screen. **Result: newest entry at the top.**

### 5.2 Ring insertion ordering: chronological append

**Confirmed** — `pushToRing`:

```js
recentRing.items.push(entry);
```

Entries are appended at the end. Since `getActiveRequests` sorts by timestamp
descending, the sort dominates — the insertion position does not affect display
order.

---

## 6. Insertion Behavior

### 6.1 When a new request completes

**Confirmed** — `saveRequestUsage()` (`usageRepo.js:241-311`):

1. Transactional dedup check: if an identical row (same timestamp, provider,
   model, connectionId, apiKey, token counts) exists, skip insert.
2. If no duplicate: INSERT into `usageHistory`.
3. `pushToRing(entry)` — appends to ring. If ring > 50, oldest trimmed.
4. `scheduleStatsEvent("update", 250)` — emits `"update"` after 250ms debounce.

### 6.2 Dedup impact on display

Because `getActiveRequests()` sorts by **timestamp** (not insertion order), a
new entry with a slightly earlier timestamp could appear **above** or **below**
other entries from the same minute. The minute-level dedup means only one entry
per unique `model|provider|tokens|minute` combo survives.

### 6.3 No streaming insertion of individual entries

The SSE stream always pushes the entire computed `recentRequests` array (up to
20 entries). There is no server-sent event for "one new request" — the full set
is recalculated and serialized each time.

---

## 7. Scroll Behavior

### 7.1 Confirmed: No auto-scroll mechanism

**The Recent Requests component has zero auto-scroll logic.** Verified by
searching for the following patterns across `UsageStats.js` and all imported
dependencies:

| Pattern | Present in RecentRequests? | Present in the codebase (elsewhere)? |
|---------|--------------------------|--------------------------------------|
| `useRef` on scroll container | **No** | Yes — `ConsoleLogClient.js:12` (`logRef`) |
| `scrollTop` assignment | **No** | Yes — `ConsoleLogClient.js:68`, `EndpointPageClient.js:97` |
| `scrollIntoView` | **No** | Yes — `SecurityWarning.js:15` (anchor nav only) |
| `scrollTo` | **No** | No |
| `autoScroll` state variable | **No** | No |
| `follow` state variable | **No** | No |
| `pause` state variable | **No** | No |

The Recent Requests card's scroll container (`UsageStats.js:66`):

```jsx
<div className="flex-1 overflow-y-auto">
```

This is a plain CSS scroll container with no imperative scroll management.
When new data arrives via SSE, React reconciles the DOM — the existing scroll
position is **preserved by the browser** (the scroll container is not reset).

### 7.2 Sticky header only

**Confirmed** — `UsageStats.js:69`:

```jsx
<thead className="sticky top-0 bg-bg z-10">
```

The table header sticks to the top of the scroll container. This is cosmetic
only — it does not affect scroll behavior.

### 7.3 Comparison with ConsoleLogClient

The closest parallel in the codebase is `ConsoleLogClient.js`, which **does**
have auto-scroll:

```jsx
const logRef = useRef(null);
// ...
useEffect(() => {
  if (!logRef.current) return;
  logRef.current.scrollTop = logRef.current.scrollHeight;
}, [logs]);
```

No equivalent pattern exists in `RecentRequests`.

### 7.4 Implication

**If a user sees the Recent Requests list and expects it to auto-scroll to the
bottom (newest entry), that expectation is not met by the upstream design.**
The table shows newest entries at the top (reverse-chronological), which is
the natural reading order: most recent first. The scroll position stays wherever
the user left it.

There is no "stick to bottom" / "follow newest" / "auto-scroll" design in the
upstream system.

---

## 8. Pause / Follow Behavior

### 8.1 Confirmed: No pause or follow mechanism

Searching every file referenced above for the following patterns yields nothing:

- No "Pause" button, toggle, or state variable in `RecentRequests` or `UsageStats`
- No "Follow" or "Auto-scroll" toggle in `RecentRequests` or `UsageStats`
- The only toggle in the tab family is the "Auto Refresh (3s)" switch in the
  **separate** `RequestLogger` component, which controls polling frequency, not scroll.

### 8.2 Contrast with RequestLogger

`RequestLogger.js` has an auto-refresh toggle:
- When on: polls `GET /api/usage/request-logs` every 3 seconds
- When off: stops polling
- **Still no scroll behavior** — the table just refreshes in place

### 8.3 Contrast with ConsoleLogClient

`ConsoleLogClient.js` has:
- Auto-scroll to bottom on new logs (always-on, no toggle)
- No pause/follow button

No component in the upstream codebase at this commit has a "pause auto-scroll"
or "follow latest" feature.

---

## 9. What Is Unknown (Source-Backed)

From read-only source analysis, the following cannot be determined:

| Question | Why unknown |
|----------|-------------|
| Whether any user reported confusion about scroll position | No issue tracker references in commit |
| Whether the sticky header was added in response to overflow complaints | Git history at this commit is snapshotted, not traced for blame per line |
| Whether the 20-entry display limit was chosen empirically | No comment explains the `slice(0, 20)` rationale |
| Whether the `RING_CAP=50` was chosen to leave headroom above the 20 display | No comment on intention |
| Whether real-world deployment has >20 recent requests within the same minute, triggering dedup losses | Requires runtime data |
| Whether the SSE push frequency causes visible flickering on the Recent Requests table | Requires runtime observation |
| Whether the sort-by-timestamp (not insertion-order) ever produces surprising ordering | Requires runtime data with out-of-order timestamps |
| Whether the Pxpipe "recent" (100-500 entries) and usage "recent" (20 entries) were intentionally different limits | No cross-reference comment |

---

## 10. Implications for "Latest 50 / Smart-Follow" Design

The following conclusions are drawn from **confirmed source observations**,
not architecture decisions.

### 10.1 Scroll behavior is a greenfield feature

The upstream has **no** auto-scroll, no "stick to bottom", and no pause/follow.
If the gorouter needs smart-follow (auto-scroll-to-bottom-when-newest-arrives
with a pause-on-manual-scroll mechanism), this must be built from scratch.
None of the upstream patterns (ConsoleLogClient's always-on auto-scroll,
RequestLogger's polling toggle) directly map to a smart-follow UX.

### 10.2 50-entry ring matches the "Latest 50" target

The existing `RING_CAP = 50` is an exact match for a "Latest 50" feature.
However, the current display caps this to 20 after dedup. A "Latest 50" design
must decide:
- Show all 50 ring entries (bypass dedup / increase slice limit), or
- Apply the same dedup logic but raise the slice cap from 20 to 50.

### 10.3 Dedup semantics matter for a live view

The minute-level dedup key means that if the same model produces the same token
counts in the same calendar minute, only one entry appears. This is fine for
aggregate usage stats but may be confusing in a real-time feed. The "smart-follow"
design should decide whether to preserve this dedup or skip it for the live view.

### 10.4 No incremental push pattern

The SSE endpoint always sends the full 20-entry array. For a 50-entry live feed,
the bandwidth cost is small (20–50 small JSON objects), but the React re-render
cost scales with the count. The upstream pattern of full-array replacement is
fine for 20 but may need memoization or virtualization at 50.

### 10.5 Ring is ephemeral

The upstream ring buffer is in-memory (global variable). Server restart = empty
ring. The gorouter's buffer design must decide:
- Keep ephemeral (like upstream), or
- Add persistence (SQLite-backed ring)

### 10.6 No pagination exists

All recent-request paths use a flat slice from the front (newest). There is no
cursor-based pagination. If the gorouter needs "Latest 50" to be navigable
beyond the first page, pagination is an addition, not a port.

---

## 11. Cross-References

| Topic | See also |
|-------|----------|
| SSE stream implementation | `src/app/api/usage/stream/route.js` |
| Ring buffer data flow | `src/lib/db/repos/usageRepo.js:100-128`, `216-238` |
| Dedup + slice logic | `usageRepo.js:222-236` |
| RecentRequests component | `src/shared/components/UsageStats.js:42-83` |
| SSE client merge | `UsageStats.js:279-300` |
| RequestLogger polling | `src/shared/components/RequestLogger.js` |
| ConsoleLogClient auto-scroll (comparison) | `src/app/(dashboard)/dashboard/console-log/ConsoleLogClient.js:65-68` |
| Pxpipe file-based events | `src/lib/pxpipe/events.js` |
| Usage page layout | `src/app/(dashboard)/dashboard/usage/page.js` |
| Gitbook docs reference | `gitbook/content/en/features/quota-tracking.md` (search `Recent Requests`) |
| CHANGELOG related | `CHANGELOG.md` line 237 — `getRecentLogs` missing-await fix |

---

*Report generated from read-only source analysis of commit
`79918c7830695bbca4a45c9fea4a42c3e9fd73d1`. No runtime execution, no build,
no deployment performed.*
