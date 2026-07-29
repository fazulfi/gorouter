# 9router-go — Keputusan Arsitektur

> File ini mencatat semua keputusan arsitektur 9router-go yang sudah disepakati.
> Dibuat: 2026-07-28

---

## Ringkasan Keputusan

| # | Pertanyaan | Keputusan |
|---|-----------|-----------|
| 1 | Scope | 3 fungsi: reverse proxy SSE + provider routing/fallback + admin dashboard (langsung semua) |
| 2 | Database | PostgreSQL langsung (pgx) |
| 3 | HTTP Framework | **chi** (std `net/http` compatible, `httputil.ReverseProxy`, middleware ecosystem) |
| 4 | Admin panel | Backend API (Go) + Frontend repo terpisah dalam monorepo (`/frontend`), build terpisah |
| 5 | Process model | Single binary, multi-goroutine (1 listener, 1 PG pool, ribuan goroutine concurrent) |
| 6 | Auth model | Basic auth + cookie session (session disimpan di PostgreSQL) |
| 7 | Deployment | ~~pending~~ |
| 8 | Provider routing | Model string routing (`{namespace}/{model}({variant})`), kompatibel dengan OpenCode clients |
| 9 | Endpoints | Semua endpoint yang ada di 9router-mw — core proxy, OAuth, admin API |
| 10 | SSE protocol | OpenAI-compatible (`data: {"choices":[{"delta":{...}}]}` + `data: [DONE]`) |
| 11 | Multi-tenant | Single-tenant (satu user admin, current model) |
| 12 | Rate limiting | Skip dulu (handle di nginx kalau perlu) |
| 13 | Logging & observability | Structured JSON logging ke stdout + `/metrics` endpoint Prometheus |
| 14 | OAuth integration | Opsi 3 — semua lewat main server (`httputil.ReverseProxy`), tanpa port listener terpisah |
| 15a | Build urutan | Lapis demi lapis: database layer → provider routing → SSE proxy → admin API → frontend |
| 15b | Jadwal | Full-time dalam seminggu (push terus sampai selesai) |

---

## Detail Keputusan

### 1. Scope
- **Keputusan:** 9router-go mencakup 3 fungsi — reverse proxy SSE, provider routing/fallback, admin dashboard.
- **Tidak ada MVP bertahap.** Semua fitur langsung dibangun dari awal dalam banyak phase.
- **Alasan:** Rewrite total, tidak ada hubungan dengan 9router-mw.

### 2. Database
- **Keputusan:** PostgreSQL langsung dari awal.
- **Driver:** `pgx` (conn pool, concurrent safe).
- **Tidak pakai SQLite.** Writer RPC tidak diperlukan karena Go goroutine aman concurrent ke 1 pool.
- **Alasan:** SQLite + writer RPC adalah workaround Node.js cluster. Go native concurrent, PG pool langsung aman.

### 3. HTTP Framework
- **Keputusan:** `chi`
- **Alasan:**
  - `http.ResponseWriter` + `http.Flusher` — SSE langsung jalan tanpa adapter
  - `httputil.ReverseProxy` — proxy ke provider tinggal setel URL
  - Middleware ecosystem: recoverer, logger, requestID, timeout, throttle, compress
  - `http.FileServer` + `embed` — static file serving untuk admin panel
  - Standard `context.Context` — passing DB/Redis gampang

### 4. Admin Panel
- **Keputusan:**
  - Backend API di Go (admin CRUD endpoints)
  - Frontend di repo yang sama (`/frontend`), build terpisah
  - Frontend bisa React/Vue/vanilla (belum diputuskan)
- **Arsitektur:** Backend serve API, frontend build static files yang di-serve via Go `embed` atau nginx.

### 5. Process Model
- **Keputusan:** Single binary, multi-goroutine
- **Arsitektur:**
  - 1 binary → 1 listener → ribuan goroutine concurrent
  - 1 PostgreSQL pool (`pgx`) untuk semua goroutine
  - Zero coordination overhead: tidak perlu writer RPC, tidak perlu Redis queue untuk DB writes
- **Alasan:** Go `net/http` handle concurrent connections dalam 1 proses tanpa cluster mode. Berbeda dengan Node.js yang butuh multiple process.

### 6. Auth Model
- **Keputusan:** Basic auth + cookie session
- **Session store:** PostgreSQL (table sessions)
- **Tidak pakai JWT.** JWT stateless — lebih kompleks tanpa benefit signifikan untuk single-tenant.

### 7. Deployment
- **Keputusan:** ~~pending (akan ditentukan nanti)~~

### 8. Provider Routing
- **Keputusan:** Model string routing seperti 9router-mw sekarang
- **Format:** `{namespace}/{model}({variant})` — contoh: `oc/deepseek-v4-flash-free(high)`
- **Endpoint:** `POST /v1/chat/completions` — parse model, cocokkan ke provider connection via combo/priority table
- **Kompatibel penuh dengan OpenCode clients.**

### 9. Endpoints
- **Keputusan:** Semua endpoint yang ada di 9router-mw, termasuk:
  - **Core proxy:** `POST /v1/chat/completions`, `POST /v1/completions`, `POST /v1/embeddings`, `GET /v1/models`
  - **OAuth:** authorize, callback, start-proxy, poll-status (untuk Codex, xAI, dll)
  - **Admin API:** health, provider connections CRUD, API keys, usage stats, settings, login/logout

### 10. SSE Protocol
- **Keputusan:** OpenAI-compatible (sama persis dengan 9router-mw sekarang)
- **Format:**
  ```
  data: {"choices":[{"delta":{"content":"Hello"}}]}
  data: {"choices":[{"delta":{"content":" world"}}]}
  data: [DONE]
  ```
- **Alasan:** Drop-in replacement untuk 9router-mw. Clients tidak perlu update config.

### 11. Multi-tenant
- **Keputusan:** Single-tenant (satu user admin, current model)
- **Tiap API key punya akses ke semua provider connections.**
- **Alasan:** Sesuai dengan model 9router-mw sekarang. Multi-tenant bisa ditambah nanti kalau diperlukan.

### 12. Rate Limiting
- **Keputusan:** Skip dulu.
- **Rate limiting bisa ditambah nanti sebagai middleware chi tanpa perubahan arsitektur.**
- **Alasan:** Nggak kritis untuk MVP. Provider rate limit tetap jalan di sisi upstream.

### 13. Logging & Observability
- **Keputusan:**
  - Structured JSON logging ke stdout (tiap request: method, path, status, latency, upstream, bytes)
  - `/metrics` endpoint dalam format Prometheus (pakai `prometheus/client_golang`)
  - Systemd journal capture otomatis dari stdout
- **Tidak pakai tracing/OpenTelemetry untuk awal.**

### 14. OAuth Integration
- **Keputusan:** Semua lewat main server, `httputil.ReverseProxy`, tanpa port listener terpisah.
- **Tidak perlu port 1455/56121 seperti 9router-mw sekarang.**
- **Alasan:** Di Go, `httputil.ReverseProxy` bisa handle proxy tanpa listener port tambahan.

### 15. Timeline & Milestones

#### 15a — Urutan Build (Lapis demi lapis)
1. **Layer 1 — Database & Foundation:** PostgreSQL schema, migrations, pgx pool, repository pattern, config loader
2. **Layer 2 — Provider Routing:** Model parser, provider registry, combo/priority logic, connection manager
3. **Layer 3 — SSE Proxy:** `/v1/chat/completions` streaming, `httputil.ReverseProxy`, SSE flush, error handling, fallback
4. **Layer 4 — Admin API:** Auth, CRUD endpoints, health, usage stats, frontend static serving
5. **Layer 5 — Frontend:** Dashboard UI (terpisah, build sendiri)

#### 15b — Jadwal
- **Full-time dalam seminggu.** Push terus sampai selesai.

---

## Arsitektur Folder (draft)

```
9router-go/
├── cmd/
│   └── 9router-go/          # main.go entrypoint
├── internal/
│   ├── config/              # env/config loader
│   ├── db/                  # PostgreSQL schema, migrations, queries
│   │   ├── schema/          # DDL
│   │   ├── repos/           # repository pattern
│   │   └── migrate/         # migration runner
│   ├── proxy/               # SSE streaming, ReverseProxy logic
│   │   ├── router/          # model parser, provider registry
│   │   ├── upstream/        # HTTP client ke provider
│   │   └── sse/             # SSE flush, [DONE] handling
│   ├── api/                 # HTTP handlers (chi)
│   │   ├── middleware/       # auth, logging, recoverer, cors, metrics
│   │   ├── v1/              # chat/completions, models, embeddings
│   │   ├── admin/           # provider CRUD, apiKeys, settings, login
│   │   └── oauth/           # Codex, xAI, dll
│   ├── auth/                # session management, password
│   ├── oauth/               # OAuth state machine, provider clients
│   ├── metrics/             # Prometheus metrics collector
│   └── model/               # domain types
├── frontend/                # dashboard UI (repo terpisah, build sendiri)
│   ├── package.json
│   └── src/
├── .env.example
├── go.mod
└── go.sum
```

---

*Catatan: Keputusan #7 (Deployment) masih pending — akan ditentukan saat implementasi.*

---

## Keputusan Tambahan (28 Juli 2026)

| # | Pertanyaan | Keputusan |
|---|-----------|-----------|
| 16 | Frontend stack | **React + Vite** + shadcn/ui + Tailwind |
| 17 | Redis untuk OAuth state | **Tidak.** PostgreSQL dengan kolom `expires_at` (tabel `oauth_sessions`, auto-cleanup via `WHERE expires_at < NOW()`) |
| 18 | Config management | **godotenv** (`.env` file) |
| 19 | OAuth state storage | **PostgreSQL** — ikut upstream, zero additional dependency |
| 20 | Target parity | **Full parity bertahap** — seluruh fitur original tetap target akhir, implementasi dan rilis dibagi per fase |
| 21 | Batas MVP production | **Super-full upstream parity** — production pertama wajib mencakup seluruh spesifikasi/fitur original, kualitas enterprise, dan repository GitHub production-ready; fase hanya gate internal |
| 22 | Kebijakan kompatibilitas | **Kontrak sama, internals diperbaiki** — API/UX/data semantics eksternal dipertahankan; bug, keamanan, concurrency, dan reliability internal diperbaiki serta perbedaannya didokumentasikan |
| 23 | Gate provider | **Semua provider wajib tervalidasi nyata** — seluruh provider dan executor harus lulus pengujian dengan akun/kredensial nyata sebelum production pertama |
| 24 | Target deployment | **Cross-platform lengkap** — Linux, Windows, macOS, Docker, tray/desktop launcher, dan installer wajib production-ready serta diuji sebelum rilis pertama |
| 25 | Bentuk distribusi | **Satu binary dengan mode berbeda** — binary Go yang sama menyediakan mode server, CLI, tray/desktop, updater, dan helper melalui subcommand/flags |
| 26 | Migrasi pengguna | **Konfigurasi ulang bersih** — tidak ada migrasi otomatis/manual state original; pengguna memasukkan ulang provider, API keys, settings, dan data lain |
| 27 | Penyimpanan secret provider | **PostgreSQL plaintext terbatas** — token/API key/cookie disimpan di PostgreSQL seperti kontrak original; keamanan mengandalkan permission DB, disk encryption, dan secret management deployment |
| 28 | Horizontal scaling | ~~Single-instance default + distributed active-active resmi~~ — **dibatalkan oleh keputusan #79**; v1 hanya satu server/satu binary multi-goroutine dan distributed mode ditunda |
| 29 | PostgreSQL desktop | **Bundled dan otomatis** — installer Windows/macOS menyediakan, menginisialisasi, menjalankan, meng-upgrade, membackup, dan memulihkan PostgreSQL lokal tanpa setup manual pengguna |
| 30 | Antarmuka konfigurasi utama | **Dashboard utama, CLI setara untuk operasi penting dan automation** — dashboard menjadi pengalaman utama; CLI mempertahankan workflow host, scripting, dan administrasi penting |
| 31 | Grammar model | **Dukung syntax original dan extension baru** — pertahankan provider/model, alias tanpa slash, combo, inferred route, dan resolution order original; tambahkan namespace/model(variant) tanpa merusak kompatibilitas |
| 32 | Format API client-facing | **Semua format original tetap native** — OpenAI Chat/Responses, Claude, Gemini, Gemini CLI, Codex, dan format lain mempertahankan request/response, terminal event, streaming, JSON, NDJSON, serta binary semantics masing-masing |
| 33 | Fitur host-local berisiko tinggi | **Opt-in per fitur** — MITM, tunnel, process spawning, updater, Headroom, Pxpipe, dan MCP host controls tetap tersedia untuk parity tetapi memerlukan aktivasi eksplisit dan permission yang jelas |
| 34 | Model repository | **Public open-source penuh** — backend, frontend, CLI, installer, deployment, dan tooling dipublikasikan sebagai source terbuka |
| 35 | Lisensi | **Apache License 2.0** — public open-source dengan patent grant eksplisit; atribusi dan kompatibilitas seluruh sumber/dependency upstream wajib diaudit |
| 36 | Strategi update | **Auto-update opt-in dengan rollback** — desktop mendukung update artefak bertanda tangan dan pemulihan versi sebelumnya; server/container diperbarui melalui deployment tooling |
| 37 | Gate performa | **Balanced enterprise pada 4 vCPU/16 GB** — proxy overhead p95 <20 ms, minimal 2.000 concurrent streams, minimal 100.000 RPM endpoint ringan, tanpa kehilangan stream atau cancellation |
| 38 | Gate reliability | **72 jam soak dan zero loss** — tidak ada crash, deadlock, goroutine leak, kehilangan usage/state, duplicate side effect, atau terminal stream ganda; restart dan failover wajib diuji |
| 39 | Gate keamanan | **Automated security gate** — SAST, dependency/license/SBOM scan, secret scan, fuzzing, race detector, authz/SSRF/trusted-proxy tests, signed artifacts, dan provenance wajib; external penetration test tidak menjadi blocker rilis pertama |
| 40 | Versi PostgreSQL server | **PostgreSQL 16+** — versi 16, 17, dan 18 wajib didukung dan diuji untuk deployment server tunggal; penyebutan distributed dibatalkan oleh keputusan #79–#80 |
| 41 | Kompatibilitas konfigurasi | **Pertahankan konfigurasi original + namespace baru** — env, CLI flags, paths, port, dan command original diterima sebagai compatibility aliases; dokumentasi utama menggunakan namespace baru |
| 42 | Nama produk | **gorouter** — nama resmi produk, binary, CLI, namespace konfigurasi, package, dan repository; menggantikan nama kerja `9router-go` |
| 43 | Command installer | **`gorouter` saja** — installer tidak menyediakan executable/shim `9router`; kompatibilitas lama terbatas pada konfigurasi dan kontrak yang dipilih, bukan nama binary |
| 44 | Default listener dan domain publik | **Bind `127.0.0.1` secara default** — akses LAN/public harus opt-in; dashboard menyediakan wizard untuk menambahkan domain, mengatur reverse proxy/TLS, memvalidasi DNS, dan menjalankan health check otomatis |
| 45 | HTTPS dan reverse proxy | **Hybrid untuk satu server** — desktop/single-server dapat memakai managed HTTPS/ACME bawaan gorouter atau external reverse proxy; dashboard mendeteksi mode, mengotomasi managed TLS, serta menghasilkan dan memvalidasi konfigurasi eksternal; mode distributed/Ingress dihapus dari v1 |
| 46 | Eksposur domain publik | **API dan dashboard langsung** — setelah domain publik diaktifkan, keduanya tersedia melalui HTTPS dan dilindungi oleh kebijakan auth/authz masing-masing |
| 47 | Login dashboard | **Password lokal saja** — cookie-backed server session di PostgreSQL; wajib memakai password hashing kuat, secure/httpOnly/SameSite cookies, CSRF protection, login lockout/backoff, session revocation, dan recovery flow |
| 48 | Backup dan disaster recovery | **Otomatis + point-in-time recovery tanpa enkripsi aplikasi** — backup terjadwal, retention, restore verification, dan download backup; server mendukung WAL/PITR, desktop memakai snapshot terkelola. File backup tidak dienkripsi oleh gorouter |
| 49 | Retensi data operasional | **90 hari untuk semua data usage, request details, dan logs** — penghapusan otomatis setelah 90 hari; retention dapat dikonfigurasi |
| 50 | Penyimpanan konten request | **Metadata saja secara default** — simpan model/provider/token/cost/latency/status/error/tool names; prompt, response, dan body hanya dapat dicapture melalui debug mode opt-in yang eksplisit |
| 51 | Ekstensibilitas provider | **Compatible provider dinamis, provider khusus melalui source** — OpenAI-compatible dan Anthropic-compatible dapat ditambahkan langsung dari dashboard; seluruh provider original built-in; protokol/OAuth nonstandard baru masuk melalui perubahan source, review, dan release resmi tanpa plugin SDK |
| 52 | Sinkronisasi upstream | **Audit dan port berkala** — bot memantau perubahan upstream dan menghasilkan issue/diff report; perubahan provider/model/protokol di-port manual dengan test serta review |
| 53 | Versioning | **SemVer ketat + deprecation policy** — breaking change hanya pada major version; kontrak lama diberi warning dan periode deprecation minimal satu siklus rilis bila memungkinkan |
| 54 | Release channels | **Beta + stable** — beta digunakan untuk validasi sebelum rilis; stable hanya diterbitkan setelah seluruh gate production lulus |
| 55 | Arah dashboard | **Feature parity dengan UX redesign** — seluruh capability dan workflow original dipertahankan, sedangkan information architecture, accessibility, responsive layout, dan visual design boleh dirancang ulang |
| 56 | Lokalisasi v1 | **English + Indonesia** — dashboard, CLI, installer, docs utama, dan user-facing errors tersedia dalam kedua bahasa; English menjadi fallback |
| 57 | Dokumentasi stable v1.0 | **Dokumentasi enterprise lengkap** — quickstart, seluruh config/API/provider/OAuth, deployment satu server lintas platform, security, backup/restore, troubleshooting, migration/deprecation, contributor, dan architecture docs wajib selesai; distributed mode tidak termasuk v1 |
| 58 | Observability dashboard | **Parity original + enterprise views** — console live stream, usage/cost, request details metadata, quotas/cooldowns, provider health, background jobs, audit events, active streams, serta status proses/server tunggal wajib tersedia; status distributed dihapus dari v1 |
| 59 | Audit trail | **Append-only audit log wajib** — semua perubahan konfigurasi dan tindakan sensitif mencatat asal/aktor, waktu, tindakan, target, before/after tersanitasi, hasil, dan instance; tidak dapat diubah lewat UI serta dapat diekspor |
| 60 | Admin API | **Full Admin API publik dan versioned** — dashboard, CLI, scripts, CI, dan sistem eksternal memakai kontrak yang sama; operasi host berisiko tetap opt-in dan policy-restricted |
| 61 | Auto-add akun provider | **Langsung aktif setelah validasi** — API key/token, bulk provisioning, OAuth/device-code, model discovery, deduplication, dan auto-import lokal opt-in didukung; akun otomatis masuk routing pool setelah seluruh validasi wajib lulus |
| 62 | Aktivasi dan status validasi akun | **Original-compatible dengan status bertahap** — akun langsung disimpan, aktif, dan dapat masuk routing; sukses validasi menjadi `active`; belum diuji, bulk import, discovery unsupported, timeout, atau error sementara menjadi `unknown` dan divalidasi ulang di background; quota/rate-limit mempertahankan akun dengan cooldown; hanya penolakan auth definitif seperti 401/403 atau kebutuhan re-auth permanen yang menonaktifkan routing |
| 63 | Kredensial Admin API | **Personal Access Token (PAT) khusus Admin API** — PAT terpisah dari API key model; dashboard tetap menggunakan session login, sedangkan CLI, CI, scripts, dan integrasi eksternal menggunakan PAT untuk mengakses Admin API |
| 64 | Kewenangan PAT | **Setiap PAT memiliki akses penuh ke seluruh Admin API** — tidak ada scope atau tipe read-only; keamanan mengandalkan pemisahan PAT dari API key model, penyimpanan aman, pencabutan token, dan audit trail |
| 65 | Masa berlaku PAT | **Dapat dipilih saat pembuatan** — admin dapat membuat PAT tanpa kedaluwarsa atau menetapkan tanggal kedaluwarsa; PAT dapat dicabut manual dan dashboard mencatat waktu terakhir digunakan |
| 66 | Pemulihan PAT | **Login dashboard lalu revoke** — jika PAT hilang atau bocor, admin masuk menggunakan password dashboard, mencabut PAT tersebut, lalu membuat PAT baru; tidak ada reset otomatis seluruh PAT |
| 67 | Penyimpanan PAT | **Hash-only dan hanya ditampilkan sekali** — database menyimpan hash, prefix identifikasi, nama, waktu dibuat, expiry opsional, last-used, dan status revoke; nilai PAT asli tidak dapat dilihat kembali |
| 68 | PAT untuk operasi host | **Diizinkan dari mana saja** — PAT full-access boleh menjalankan MITM, tunnel, process spawning, updater, Headroom, Pxpipe, MCP host controls, dan operasi host sensitif lainnya apabila fitur terkait telah diaktifkan; setiap tindakan wajib masuk audit trail |
| 69 | Jumlah PAT | **Tidak dibatasi** — admin dapat membuat PAT terpisah untuk setiap CI, script, perangkat, atau integrasi; dashboard menampilkan identitas dan aktivitas masing-masing agar revoke serta audit tetap terisolasi |
| 70 | Akses API key model | **Semua model aktif** — setiap API key client AI dapat menggunakan seluruh provider, combo, alias, dan model yang aktif; tidak ada allowlist model atau quota per key pada v1 |
| 71 | Siklus API key model | **Hash-only dengan expiry opsional** — key hanya ditampilkan sekali saat dibuat; database menyimpan hash, prefix, nama, waktu dibuat, expiry opsional, last-used, dan status revoke; key berlaku sampai expiry atau dicabut |
| 72 | Auth endpoint model dari localhost | **API key tetap wajib** — seluruh request client-facing `/v1/*`, `/v1beta/*`, dan compatibility routes harus memakai API key valid walaupun berasal dari loopback; tidak ada trusted-localhost bypass |
| 73 | Publikasi dashboard | **Dashboard selalu boleh dipublikasikan** — dashboard mengikuti listener/domain yang dikonfigurasi; setiap client yang dapat menjangkaunya boleh mencoba login, dengan HTTPS, password, CSRF protection, serta lockout/backoff sebagai kontrol akses |
| 74 | Pemulihan password dashboard | **CLI lokal reset** — password disimpan sebagai hash satu arah dan tidak dapat dilihat dari database; `gorouter admin password reset` mengganti hash di PostgreSQL dan mencabut seluruh session dashboard aktif |
| 75 | Efek perubahan password | **Cabut semua session, PAT tetap aktif** — setiap perubahan atau reset password memaksa seluruh dashboard login ulang; PAT Admin API tidak dicabut karena merupakan kredensial automation yang terpisah |
| 76 | Masa session dashboard | **Sliding expiry 30 hari** — session diperpanjang berdasarkan aktivitas yang sah dan kedaluwarsa setelah 30 hari tanpa aktivitas; logout, revoke, atau perubahan password dapat mengakhirinya lebih awal |
| 77 | Pengelolaan session dashboard | **Tidak ada halaman session per perangkat** — session berakhir melalui expiry, logout dari session tersebut, atau perubahan/reset password yang mencabut seluruh session; dashboard tidak menampilkan daftar session aktif |
| 78 | Auth dalam distributed mode | ~~Session dan PAT global di semua instance~~ — **dibatalkan oleh keputusan #79** karena distributed multi-instance ditunda |
| 79 | Model runtime final v1 | **Satu server, satu binary, multi-goroutine** — gorouter menjalankan ribuan request concurrent dalam satu proses Go pada satu mesin; tidak ada active-active, controller/worker instance, load balancer internal, leader election, atau koordinasi multi-instance pada v1; distributed mode ditunda |
| 80 | Scope distributed v1 | **Dihapus seluruhnya dari v1** — active-active, leader election, shared multi-instance state, status multi-instance, failover antar-instance, dan mode load balancer/Ingress gorouter bukan bagian rilis pertama; external reverse proxy tetap boleh melindungi satu server |
| 81 | Eksekusi background jobs | **Scheduler internal dalam binary** — token refresh, quota polling, retention cleanup, backup, provider health, dan maintenance dijalankan oleh goroutine scheduler dengan locking lokal, timeout, retry/backoff, status persisten bila diperlukan, serta observability dashboard |
| 82 | Recovery background job | **Lanjutkan atau ulang secara aman** — job memakai lease/checkpoint PostgreSQL; setelah restart, job terputus dideteksi lalu dilanjutkan atau diulang secara idempotent tanpa duplicate side effect |
| 83 | Konfigurasi jadwal background job | **Tetap di source tanpa override** — interval, timeout, retry, dan enablement job ditentukan oleh implementasi; tidak dapat diubah melalui `.env`, dashboard, atau Admin API; dashboard hanya menampilkan status dan riwayat |
| 84 | Kegagalan job kredensial provider | **Exponential backoff dan cooldown** — akun tetap tersimpan, diberi status degraded/cooldown, dan fallback menggunakan akun/provider lain; hanya kegagalan autentikasi definitif yang menonaktifkan akun dari routing |
| 85 | Kegagalan job backup | **Routing tetap berjalan dengan alert kritis** — backup dan restore verification di-retry memakai backoff; dashboard terus menampilkan kondisi kritis sampai backup tervalidasi kembali, tetapi request AI dan perubahan konfigurasi tidak dihentikan |
| 86 | Kegagalan job retensi | **Retry dengan warning** — penghapusan data melewati retensi diulang memakai backoff dan dashboard menampilkan warning; routing dan pencatatan data baru tetap berjalan |
| 87 | Eksekusi manual background job | **Semua job menyediakan Run now** — dashboard dan Admin API/PAT dapat memicu token refresh, quota/provider health, revalidasi, retention cleanup, backup, restore verification, update check, dan job lain; locking, idempotency, status, serta audit trail tetap wajib |
| 88 | Jadwal token refresh OAuth | **Per akun, 5 menit sebelum `expires_at`** — refresh tidak memakai interval harian; scheduler menghitung waktu dari expiry setiap kredensial, menjalankan proactive refresh lima menit sebelumnya, memakai backoff/cooldown untuk kegagalan sementara, dan meminta login ulang hanya untuk penolakan refresh definitif |
| 89 | Token OAuth tanpa expiry | **Refresh setiap 6 jam** — jika provider tidak menyediakan `expires_at` yang dapat dipercaya, gorouter menjadwalkan refresh kredensial tersebut setiap enam jam; kegagalan sementara tetap mengikuti backoff/cooldown |
| 90 | Refresh OAuth bersamaan | **Concurrency terbatas dengan jitter** — setelah startup atau saat banyak akun jatuh tempo, refresh diproses bertahap dengan jeda acak; akun paling dekat expiry diprioritaskan untuk mencegah rate limit dan lonjakan resource |
| 91 | Batas concurrency refresh OAuth | **Maksimal 4 refresh bersamaan** — scheduler memakai semaphore global empat pekerjaan; batas per-provider, backoff, dan jitter tetap dapat menurunkan concurrency efektif |
| 92 | Request saat refresh OAuth | **Tunggu singleflight maksimal 10 detik lalu fallback** — seluruh request untuk akun yang sama berbagi satu operasi refresh; bila akun alternatif tersedia, request menunggu paling lama sepuluh detik lalu fallback jika refresh belum selesai atau gagal |
| 93 | Request refresh tanpa akun fallback | **Tetap menunggu refresh** — jika hanya ada satu akun yang dapat melayani model tersebut, request tetap menunggu melewati sepuluh detik sampai refresh selesai atau mencapai timeout refresh/provider; tidak mencoba token lama dan tidak mengembalikan 401 palsu |
| 94 | Timeout operasi refresh OAuth | **30 detik per percobaan** — satu panggilan refresh dibatalkan setelah tiga puluh detik; setelah timeout, scheduler/request menerapkan retry, backoff, cooldown, atau fallback sesuai konteks |
| 95 | Jumlah percobaan refresh OAuth | **Maksimal 3 percobaan per rangkaian** — setiap percobaan memiliki timeout 30 detik dan jeda backoff+jitter; setelah tiga kegagalan, akun masuk cooldown dan routing memakai fallback bila tersedia |
| 96 | Override refresh per provider | **Diizinkan berdasarkan audit provider** — aturan umum 5 menit sebelum expiry atau 6 jam tanpa expiry menjadi default; provider khusus boleh memiliki lead-time/refresh-age built-in yang diuji dan didokumentasikan, tidak dapat diubah bebas oleh admin |
| 97 | Sifat proyek | **Rewrite 9Router original, bukan produk greenfield dari nol** — source upstream menjadi baseline spesifikasi, perilaku, kontrak, dan fitur. Implementasi internal ditulis ulang ke Go sesuai keputusan pengguna, tetapi fitur atau semantik upstream tidak boleh dihilangkan atau diganti diam-diam |
| 98 | Fallback akun dan model | **Sama seperti upstream** — request ke satu model tetap pada model tersebut dan mencoba akun provider yang tersedia sesuai aturan cooldown/fallback akun. Perpindahan otomatis ke model lain hanya berlaku ketika pengguna memilih combo berisi beberapa model, mengikuti urutan dan strategi combo upstream |
| 99 | Metode rewrite upstream | **Port modul demi modul** — setiap modul JavaScript upstream dipetakan ke modul Go tujuan, kontrak perilaku dan tes parity ditetapkan lebih dulu, lalu implementasi Go menggantikannya lapis demi lapis. Modul tidak dianggap selesai hanya karena fiturnya tampak bekerja; kontrak, edge case, dan interaksi upstream yang relevan harus terbukti setara |
| 100 | Lokasi source upstream selama rewrite | **Di luar repository gorouter** — clone upstream tetap berada pada workspace audit terpisah dan dikunci ke commit yang diaudit. Repository gorouter tidak membawa submodule atau snapshot JavaScript upstream; repository hanya menyimpan implementasi baru, tes parity, dokumentasi pemetaan, dan atribusi yang diperlukan |
| 101 | Definition of Done modul | **Parity tests plus source review** — modul Go dinyatakan selesai hanya setelah kontrak upstream terinventarisasi, unit/integration/golden parity tests lulus, happy path serta edge case/error path teruji, dan implementasi direview kembali terhadap source upstream yang dipetakan. Sekadar berfungsi pada happy path tidak cukup |
| 102 | Bug upstream saat rewrite | **Usulkan perbaikan dengan tes kompatibilitas, lalu minta keputusan pengguna** — saat menemukan bug atau desain lemah, assistant menjelaskan perilaku upstream, dampak, opsi mempertahankan atau memperbaiki, dan trade-off. Jika pengguna memilih perbaikan, regression test wajib ditambahkan, kontrak eksternal dipertahankan sebisa mungkin, dan perbedaan didokumentasikan. Tidak ada perubahan perilaku yang diputuskan diam-diam |
| 103 | Fleksibilitas proses rewrite | **Source-backed tetapi tidak kaku** — upstream adalah baseline untuk memahami kontrak dan parity, bukan perintah untuk menyalin semua desain apa adanya. Untuk setiap fitur atau modul, assistant harus menawarkan pilihan mempertahankan, meng-upgrade, menyederhanakan, atau mengubah beserta dampak kompatibilitas, risiko, dan biaya; keputusan final selalu milik pengguna sebelum implementasi |
| 104 | Combo routing | **Pertahankan seluruh mode upstream lalu harden internalnya** — fallback berurutan, round-robin, capability auto-switch, dan fusion multi-model dengan judge tetap tersedia dengan UX dan kontrak utama yang kompatibel. Implementasi Go harus meningkatkan keamanan concurrency, propagasi cancellation, timeout, observability, dan determinisme tanpa menghapus mode atau mengubah semantik secara diam-diam |
| 105 | Arsitektur translator protokol | **Pertahankan direct translators dan native passthrough, tambah typed IR pada jalur yang aman** — semantik thinking blocks, tools, images, errors, terminal events, NDJSON, dan format biner tidak boleh dipaksa melewati representasi lossy. Typed intermediate representation digunakan hanya ketika kontrak source-target dapat direpresentasikan tanpa kehilangan informasi, dengan golden parity tests per pasangan format |
| 106 | Implementasi OAuth provider | **Port per provider sesuai mekanisme upstream** — loopback acak, callback port tetap, device code, cookie/PAT import, IDE import, dashboard callback relay, serta variasi provider lain dipertahankan sebagai implementasi provider-specific. Tidak dipaksa menjadi satu core OAuth seragam; perbaikan keamanan/reliabilitas tetap diajukan kepada pengguna dan diuji tanpa menghapus perilaku provider |
| 107 | Penyimpanan request detail | **Metadata troubleshooting lengkap, body opt-in** — secara default simpan provider/model, identitas akun yang dianonimkan, token, biaya, latency, status, error teredaksi, tool names, fallback/retry trace, dan metadata operasional relevan. Prompt, response, body, serta payload sensitif hanya disimpan ketika debug capture diaktifkan eksplisit, konsisten dengan keputusan #50 |
| 108 | Token-saver dan request mutator | **Port mengikuti perilaku upstream** — RTK, Headroom, Caveman, Ponytail, Pxpipe, serta hook sejenis dipindahkan dengan urutan, fail-open behavior, dan efek mutasi yang kompatibel. Lapisan sandbox/resource-limit baru tidak ditambahkan otomatis; setiap usulan hardening yang dapat mengubah perilaku harus dimintakan keputusan pengguna |
| 109 | Penyimpanan state fallback akun | **Hot state di memory dengan checkpoint penting di PostgreSQL** — cooldown, model locks, rotasi, dan health state yang sering diakses berjalan di memory untuk hot-path rendah latency. State yang dibutuhkan agar restart tidak menghapus cooldown atau status penting disimpan/checkpoint ke PostgreSQL dan dipulihkan secara deterministik saat startup |
| 110 | Legacy compatibility paths | **Dipertahankan selamanya** — endpoint dan rewrite kompatibilitas upstream, termasuk bentuk historis seperti double `/v1/v1/*`, tetap didukung sebagai kontrak gorouter dan tidak dijadwalkan untuk deprecation/penghapusan. Tes kontrak wajib menjaga perilaku ini lintas rilis |
| 111 | Export/import manual | ~~Konfigurasi dan secrets dalam arsip terenkripsi~~ — **dibatalkan oleh keputusan #200**; gorouter tidak menyediakan manual export/import konfigurasi melalui dashboard, CLI, atau Admin API |
| 112 | Frekuensi monitoring upstream | **Harian** — bot memeriksa commit, tag, release, registry provider/model, translator, executor, OAuth, dan kontrak relevan upstream setiap hari. Bot hanya membuat laporan/issue terkelompok; perubahan tetap dipilih, di-port, diuji, dan direview manual sesuai keputusan pengguna |
| 113 | Client disconnect pada streaming | **Grace 500ms seperti upstream** — setelah disconnect terdeteksi, gorouter menunggu 500ms untuk menyaring disconnect semu lalu membatalkan request upstream, reader/writer, translator, timer, dan goroutine terkait melalui context cancellation. Terminal event tidak boleh dikirim ganda dan resource tidak boleh bocor |
| 114 | Penyimpanan PAT dan model API key | **DIBATALKAN oleh #120** — rancangan enkripsi reversibel dan pengelolaan passphrase dinilai terlalu rumit dan tidak digunakan |
| 115 | Retensi riwayat state runtime | **7 hari** — transisi cooldown, model lock, account health, refresh state, dan checkpoint runtime yang relevan disimpan selama tujuh hari untuk troubleshooting. Snapshot terkini tetap tersedia untuk pemulihan cepat; cleanup scheduler menghapus riwayat lebih lama secara idempotent |
| 116 | Streaming stall watchdog | **Default 6 menit berbasis aktivitas byte mentah** — setiap raw upstream byte mereset timer meskipun translator belum menghasilkan output. Provider khusus boleh memiliki override built-in yang dibuktikan audit, diuji, dan didokumentasikan; tidak ada override admin bebas |
| 117 | Sumber kunci enkripsi token | **DIBATALKAN oleh #120** — tidak ada passphrase runtime, unlock saat startup, atau rotasi kunci credential tambahan |
| 118 | First-chunk timeout | **Default 200 detik dengan override built-in per provider** — timer menunggu byte/chunk upstream pertama sekitar 200 detik. Provider atau protokol khusus boleh memiliki nilai source-defined berdasarkan audit, golden/integration tests, dan dokumentasi; admin tidak mengubahnya bebas |
| 119 | Sumber pricing model | **Built-in terversi plus override admin** — gorouter membawa harga default per provider/model berdasarkan audit upstream dan sumber provider. Admin dapat mengubah harga per model melalui dashboard atau Admin API; override, nilai sebelum/sesudah, actor, dan hasil dicatat dalam audit trail |
| 120 | Model penyimpanan credential final | **Sederhana dan restart otomatis** — kredensial provider seperti API key, OAuth access/refresh token, cookie, dan token provider disimpan sebagai restricted plaintext di PostgreSQL sesuai #27 dan langsung dapat digunakan kembali setelah restart; tidak ada unlock, passphrase runtime, rotasi secret otomatis, atau kunci tambahan. PAT Admin dan model API key buatan gorouter tetap hash-only sesuai #67/#71; export membawa hash+metadata agar token lama tetap valid setelah restore. Enkripsi hanya diterapkan pada arsip backup/export, terpisah dari penyimpanan credential runtime |
| 121 | Penambahan provider account | **Pertahankan perilaku upstream** — account langsung disimpan, aktif, dan eligible untuk routing. Validasi sukses memberi status active; hasil belum pasti, bulk import, discovery unsupported, timeout, atau error sementara memberi status unknown dan background revalidation. Hanya penolakan autentikasi definitif yang menonaktifkan account, konsisten dengan #62 |
| 122 | Pemilihan account provider | **Pertahankan priority, cooldown, dan fallback upstream** — urutan priority pengguna dihormati, account yang sedang cooldown dilewati, dan kegagalan sementara mencoba account berikutnya untuk model yang sama. Round-robin atau health-weighted tidak menggantikan semantik ini tanpa keputusan baru, konsisten dengan #98 |
| 123 | Dashboard provider | **Workflow parity dengan UX redesign** — kemampuan tambah, edit, hapus, reorder, test, import, bulk provisioning, cooldown/status, model, proxy pool, dan workflow provider upstream dipertahankan. Information architecture, accessibility, responsiveness, visual hierarchy, dan feedback pengguna boleh didesain ulang tanpa menghilangkan kemampuan |
| 124 | Durabilitas pencatatan usage | **Async durable queue** — hot path request memasukkan event usage ke antrean durable di PostgreSQL, lalu worker background memproses detail dan agregat. Respons/stream tidak menunggu seluruh agregasi selesai, tetapi event yang telah diterima tidak boleh hilang ketika proses restart |
| 125 | Pemilihan proxy pool | **Pertahankan semantik upstream** — assignment proxy per account, urutan/rotasi, fallback, status pengujian, cooldown, dan workflow deployment helper dipertahankan. Implementasi internal boleh diperkuat tanpa mengubah kontrak atau perilaku utama secara diam-diam |
| 126 | Pengendalian fitur host-local opt-in | **CLI lokal dan dashboard** — local CLI selalu dapat mengaktifkan/menonaktifkan fitur host-local. Dashboard juga dapat melakukannya apabila admin telah login dan policy deployment mengizinkan; semua perubahan dan hasilnya wajib dicatat dalam audit trail |
| 127 | Transport API key model | **Pertahankan seluruh transport original** — dukung `Authorization: Bearer`, `x-api-key`, `x-goog-api-key`, dan query `?key=` untuk compatibility. Nilai key wajib disensor dari log, URL observability, metrics, error, dan audit trail |
| 128 | Kebijakan CORS | **Pisahkan Model API dan Admin** — route model/client-facing mempertahankan CORS kompatibel dengan original; Admin API dan dashboard hanya mengizinkan same-origin atau origin yang dikonfigurasi. Perilaku preflight dan header diuji per keluarga route |
| 129 | Batas jaringan operasi host sensitif | **PAT dari mana saja** — apabila fitur host terkait telah diaktifkan, PAT full-access yang valid dapat menjalankan shutdown, reset password, auto-import, MCP/tunnel/process controls, updater, dan operasi sensitif lain dari jaringan mana pun. Keputusan ini menegaskan #68; seluruh tindakan wajib diaudit dan tetap mengikuti HTTPS serta auth yang berlaku |
| 130 | Cakupan credential auto-import | **Sama seperti upstream** — port mekanisme, sumber aplikasi, path, deteksi, parsing, preview/konfirmasi, deduplication, dan batas akses file sesuai implementasi original per provider. Tidak memperluas scan di luar perilaku upstream tanpa keputusan baru |
| 131 | Shutdown dan restart runtime | **Graceful drain** — berhenti menerima request baru, menunggu stream aktif sampai selesai, mem-flush state dan usage, lalu shutdown/restart melalui integrasi service manager. Tidak ada batas waktu pemaksaan atau pembatalan stream pada drain normal; konsekuensinya shutdown/update dapat tertahan tanpa batas oleh stream yang tidak pernah selesai |
| 132 | Batas waktu graceful drain | **Tunggu seluruh stream sampai selesai** — gorouter tidak membatalkan stream aktif karena deadline drain. Listener berhenti menerima request baru, tetapi proses baru boleh berhenti setelah seluruh stream selesai dan state penting ter-flush |
| 133 | Eksekutor update mode server | **External service manager** — gorouter memeriksa, mengunduh, memverifikasi, dan menyiapkan update atau instruksi update; pergantian runtime, restart, health validation, dan rollback dijalankan melalui systemd, Windows Service, container tooling, atau deployment tooling eksternal |
| 134 | Trusted proxy | **CIDR/IP eksplisit** — loopback dipercaya secara default; reverse proxy tambahan hanya dipercaya bila alamat/CIDR-nya dikonfigurasi eksplisit. Forwarding header dari peer lain dibuang dan gorouter membentuk identitas client dari koneksi langsung |
| 135 | Force shutdown | **Tersedia hanya melalui CLI lokal** — graceful drain normal tetap menunggu seluruh stream tanpa batas. Jika proses tertahan oleh stream macet, operator host dapat menjalankan force shutdown eksplisit setelah konfirmasi; tindakan ini memutus stream aktif dan wajib diaudit |
| 136 | Akses jaringan custom provider | **Boleh menghubungi seluruh alamat** — custom provider dapat memakai internet publik, loopback, LAN, private network, link-local, atau alamat internal lain tanpa pembatasan SSRF dari gorouter. Fleksibilitas ini dipilih sadar; kompromi dashboard/PAT dapat memberi akses ke layanan internal dan risiko tersebut wajib didokumentasikan |
| 137 | Pemeriksaan DNS custom provider | **Tidak ada pemeriksaan keamanan DNS** — gorouter tidak memvalidasi perubahan resolusi domain untuk mencegah DNS rebinding dan mempercayai hostname custom provider saat koneksi dilakukan |
| 138 | Dukungan provider lokal | **Best-effort** — Ollama dan gateway compatible lokal dapat dikonfigurasi dan digunakan, tetapi bukan release gate resmi serta tidak dijamin bekerja pada setiap platform atau konfigurasi jaringan |
| 139 | Prinsip kompleksitas desain | **Fitur tambahan boleh, overengineering tidak** — assistant boleh mengusulkan peningkatan di luar upstream bila manfaatnya konkret, tetapi wajib memilih desain paling sederhana yang memenuhi kebutuhan. Jangan menambah passphrase startup, unlock manual, dependency, service, policy, abstraksi, atau langkah operasional baru tanpa ancaman/kebutuhan nyata dan manfaat yang sebanding. Runtime 24/7 harus dapat restart otomatis tanpa interaksi manusia; gunakan logika operasional praktis sebelum mengusulkan kontrol tambahan |
| 140 | Pembaruan registry provider/model built-in | **Mengikuti release gorouter** — registry provider, model, capability, OAuth metadata, dan executor mapping menjadi source terversi yang diperbarui melalui channel beta/stable bersama binary. Tidak ada download registry runtime terpisah |
| 141 | Packaging dashboard | **Asset frontend di-embed ke binary** — React/Vite dibangun saat release, hasil statis terversi bersama backend lalu di-embed ke executable Go. Instalasi dan update mempertahankan kontrak satu binary tanpa folder dashboard eksternal |
| 142 | Urutan precedence konfigurasi | **Flags > environment > `.env` > database/dashboard > defaults** — override host/deployment eksplisit selalu menang. Dashboard mengelola nilai database ketika tidak dikunci oleh sumber yang lebih tinggi dan harus menunjukkan bila suatu nilai sedang dioverride |
| 143 | Penerapan perubahan konfigurasi | **Hot reload jika aman** — setting runtime-safe diterapkan langsung dan atomik. Setting yang mengubah listener, TLS, database, atau resource fundamental ditandai memerlukan restart; dashboard menampilkan effective value, sumber override, status penerapan, dan kebutuhan restart |
| 144 | Pemilihan metode autentikasi provider | **Eksplisit per account** — setiap provider connection memiliki satu `authType` yang jelas sesuai mekanisme upstream, seperti API key, OAuth, cookie/token import, PAT, atau free-tier. Router tidak menebak atau berpindah metode auth secara otomatis di dalam connection yang sama |
| 145 | Error client model API | **Structured error tersanitasi** — pertahankan HTTP status, type/code, retryability, provider/model, dan pesan yang aman serta kompatibel. Token, cookie, credential, internal URL/path, header sensitif, dan payload upstream rahasia wajib disensor |
| 146 | Penyimpanan log aplikasi | **PostgreSQL selama 90 hari** — structured logs tetap dikirim ke stdout dan salinan yang diperlukan dashboard disimpan di PostgreSQL agar dapat dicari serta di-stream. Cleanup otomatis mengikuti retensi 90 hari |
| 147 | Retensi audit trail | **Disimpan selamanya** — audit events append-only tidak dihapus otomatis, tidak dapat diubah melalui dashboard/Admin API, dan dapat diekspor untuk forensik serta compliance |
| 148 | Model tanpa data pricing | **Cost dianggap nol** — token dan metrik penggunaan tetap dicatat, sedangkan perhitungan biaya menggunakan nol sampai built-in pricing atau override admin tersedia. Dashboard harus membedakan harga yang belum dikonfigurasi agar angka nol tidak disalahartikan sebagai harga provider yang terverifikasi |
| 149 | Perubahan pricing terhadap histori | **Histori tidak dihitung ulang** — cost yang tersimpan saat request terjadi tetap immutable meskipun built-in pricing atau override admin berubah kemudian. Perubahan harga hanya memengaruhi request berikutnya dan tercatat di audit trail |
| 150 | PostgreSQL eksternal pada desktop | **Bundled default, external optional** — instalasi desktop biasa memakai PostgreSQL terkelola otomatis. Advanced user dapat menyediakan PostgreSQL 16–18 sendiri melalui konfigurasi connection yang didukung, tanpa menjalankan bundled instance |
| 151 | Kegagalan PostgreSQL bundled desktop | **Recovery UI dan safe mode** — routing normal tidak dimulai dengan state database yang tidak sehat. gorouter menyediakan UI/CLI pemulihan untuk diagnosis, repair terarah, restore backup, atau pemilihan database; tidak melakukan reset/penghapusan data otomatis |
| 152 | Downgrade schema database | **Migration backward-compatible** — perubahan schema memakai pola expand-first dan menjaga minimal satu versi rilis sebelumnya tetap dapat membaca schema selama rollback window yang didokumentasikan. Perubahan destruktif ditunda sampai compatibility window berakhir |
| 153 | Eksekusi migration saat startup | **Otomatis dengan backup** — sebelum listener routing aktif, gorouter membuat backup/snapshot tervalidasi, mengambil migration lock, menjalankan migration, memverifikasi schema/data, lalu membuka layanan. Kegagalan membawa runtime ke safe mode, bukan reset data |
| 154 | Eksklusivitas runtime per database | **Tolak proses kedua** — satu PostgreSQL database hanya boleh dimiliki satu runtime gorouter aktif. Advisory lock/lease mencegah proses kedua menjalankan listener, scheduler, atau side effect agar single-server semantics tetap terjaga |
| 155 | CLI diagnosis database terpisah | **Tidak diperlukan** — diagnosis dilakukan melalui dashboard/runtime aktif dan tooling PostgreSQL standar. gorouter tidak menambah subcommand read-only khusus hanya untuk membaca database dari proses kedua |
| 156 | Cloud dan sinkronisasi | **Local-only tanpa cloud** — gorouter tidak menyediakan atau bergantung pada layanan cloud/sync. Data dan konfigurasi tetap berada pada instalasi lokal/server pengguna; backup/export menjadi mekanisme pemindahan data. Ini merupakan perubahan scope eksplisit dari fitur cloud/sync original |
| 157 | Pengalaman tray desktop | **Tray dengan dashboard browser** — tray mengontrol start/stop/status, membuka dashboard, dan update/recovery dasar. Seluruh konfigurasi lengkap tetap menggunakan dashboard web yang sama; tidak dibuat UI desktop native kedua |
| 158 | Histori basic chat dashboard | **Sama seperti upstream** — playground/basic chat mempertahankan workflow dan penyimpanan browser/local state original. Percakapan tidak disimpan sebagai histori chat permanen di PostgreSQL |
| 159 | Dukungan MCP | **Pertahankan seluruh workflow upstream** — port konfigurasi, start/stop, SSE/message routes, process lifecycle, dashboard, CLI, dan perilaku host integration original. MCP tetap fitur host-local opt-in sesuai #33 |
| 160 | Dukungan MITM | **Parity penuh pada release pertama** — certificate lifecycle, proxy/listener, alias, dashboard/CLI, instalasi serta penghapusan trust lintas platform, dan security tests wajib tersedia. MITM tetap opt-in dan masuk gate super-full parity #21 |
| 161 | Dukungan tunnel | **Parity penuh pada release pertama** — port workflow tunnel, Tailscale, public endpoint controls, dashboard/CLI, lifecycle, status, dan recovery upstream sebagai fitur opt-in. Tunnel diperlakukan sebagai akses jaringan, bukan cloud data sync yang dihapus oleh #156 |
| 162 | Aktivasi request mutators/token savers | **Sama seperti upstream** — Headroom, Pxpipe, RTK, Caveman, Ponytail, dan hook sejenis mempertahankan toggle, settings, workflow, urutan eksekusi, serta fail-open behavior per fitur. Dashboard boleh didesain ulang tanpa menggabungkan kontrol menjadi satu toggle |
| 163 | Playground modality media | **Pertahankan workflow upstream** — port halaman dan workflow dashboard media yang benar-benar tersedia di original untuk image, audio/TTS/STT, video, embedding, web search/fetch, dan modality lain. Tidak menambah playground baru hanya karena endpoint tersedia jika upstream tidak memilikinya |
| 164 | Pengelolaan binary/helper eksternal | **Kelola otomatis bila upstream melakukannya** — installer/runtime mem-port mekanisme original untuk download, install, detection, update, status, dan recovery helper. Operasi host-level tetap meminta izin sesuai fitur opt-in; dependency yang upstream memang manual tidak otomatis dibundle |
| 165 | Penyedia credential live provider test | **User/operator menyediakan** — live test harness memakai credential dari secret store CI/lab yang disediakan pengguna/operator. Credential tidak disimpan dalam repository; provider yang membutuhkan credential belum lulus live gate sampai secret pengujian tersedia |
| 166 | Waktu live provider matrix dijalankan | **Sebelum beta dan stable** — unit, contract, golden, dan mock integration tests berjalan pada perubahan biasa; seluruh matriks provider berkredensial nyata dijalankan pada release candidate beta, sebelum stable, serta melalui manual run bila diperlukan |
| 167 | Parity provider Free Tier | **Parity penuh** — seluruh provider yang benar-benar ditampilkan dan didukung sebagai Free Tier pada produk original dipertahankan beserta penambahan account, OAuth/token bila digunakan provider tersebut, model discovery, status/test, cooldown, routing, dashboard, dan CLI. Tidak mencampurkan kategori internal lain menjadi workflow produk baru |
| 168 | Workflow Quota | **Pertahankan penuh seperti upstream** — halaman quota, refresh manual/otomatis, status quota per account/model, cooldown, error, dan pengaruhnya terhadap routing dipertahankan. UX boleh dirancang ulang tanpa menghilangkan capability |
| 169 | Console log dashboard | **Live view saja seperti upstream** — dashboard mempertahankan live console/runtime log workflow original tanpa menambahkan download/export log dari halaman tersebut. Retensi internal tetap mengikuti keputusan #146 |
| 170 | Halaman Endpoint dan client setup | **Parity plus client presets** — pertahankan workflow Endpoint original dan tambahkan snippet konfigurasi siap salin untuk client populer yang didukung, memakai binary, URL, API key transport, serta namespace konfigurasi gorouter |
| 171 | Tool Translator dashboard | **Pertahankan seperti upstream** — tool tersedia di dashboard production dengan selector format input/output, editor payload, hasil translation, error feedback, dan workflow original. UX boleh diperbaiki tanpa mengurangi kemampuan |
| 172 | Halaman Skills dan Token Saver | **Tetap terpisah seperti upstream** — capability, settings, status, dan workflow masing-masing dipertahankan pada area yang berbeda; tidak digabung atau dipindahkan hanya demi penyederhanaan internal |
| 173 | Reset dan cleanup usage | **Sama seperti upstream** — pertahankan kontrol reset/cleanup yang benar-benar tersedia di original beserta scope, konfirmasi, dan efeknya. Setiap tindakan manual wajib masuk audit trail; tidak menambah penghapusan rentang baru tanpa keputusan lain |
| 174 | Dashboard dan lifecycle Pxpipe | **Pertahankan penuh seperti upstream** — setup, start/stop, status, timeline, error/recovery, dashboard, CLI, dan integrasi request Pxpipe dipertahankan sesuai perilaku original |
| 175 | Proxy pool dan deployment helper | **Parity penuh seperti upstream** — create, edit, test, rotate, assign per connection, status, cooldown/fallback, dashboard, CLI, serta deployment helper Cloudflare, Deno, dan Vercel yang benar-benar tersedia di original dipertahankan beserta status dan recovery workflow |
| 176 | Kontrol cooldown account | **Sama seperti upstream** — admin dapat memakai reset/unlock cooldown yang benar-benar tersedia di original beserta scope dan efeknya. Tindakan manual dicatat dalam audit trail; tidak menambah global reset baru tanpa keputusan lain |
| 177 | Workflow CLI Tools | **Parity penuh seperti upstream** — daftar tool, detection, install/config/status, model dan endpoint setup, dashboard, terminal menu, serta workflow integrasi tool lokal original dipertahankan |
| 178 | Autostart desktop | **Tidak ada autostart** — gorouter desktop tidak mendaftarkan dirinya untuk berjalan otomatis saat login OS. Pengguna menjalankannya secara manual; installer, tray, dan dashboard tidak menyediakan toggle autostart |
| 179 | Perilaku close desktop | **Server tetap berjalan di tray** — menutup jendela/dashboard desktop hanya menutup atau menyembunyikan UI. Routing tetap aktif; penghentian membutuhkan tindakan Exit eksplisit dan mengikuti graceful shutdown |
| 180 | Pemeriksaan update desktop | **Saat startup saja** — ketika gorouter desktop dijalankan, aplikasi memeriksa update satu kali. Tidak ada polling update harian selama proses hidup; update tetap opt-in sesuai #36 |
| 181 | Shortcut installer desktop | **Tidak membuat shortcut** — installer tidak menambahkan shortcut Desktop, Start Menu, atau shortcut aplikasi tambahan. Pengguna menjalankan gorouter dari binary/lokasi instalasi atau mekanisme platform yang dipilihnya |
| 182 | Feedback Exit saat stream aktif | **Tampilkan jumlah stream dan status drain** — tray menunjukkan jumlah stream yang masih aktif dan durasi menunggu sampai selesai. Pengguna host dapat memilih Force Exit lokal sesuai #135 jika ingin memutus stream |
| 183 | Membuka dashboard saat desktop start | **Buka browser otomatis** — menjalankan gorouter desktop membuka dashboard pada browser default setelah health siap. Tray dapat membuka dashboard kembali kapan pun |
| 184 | Browser pada mode server/headless | **Tidak pernah dibuka otomatis** — mode server/headless hanya menulis URL dan status ke log/terminal serta tidak mencoba menjalankan browser atau UI grafis |
| 185 | Konflik port desktop | **Tampilkan error dan pilihan port** — gorouter tidak memilih port acak dan tidak menghentikan proses lain. Recovery UI/tray menjelaskan port yang bentrok dan menawarkan pengguna memilih konfigurasi port lain |
| 186 | Syarat first-run setup | **Password dashboard saja** — setup awal dianggap selesai setelah admin membuat password lokal. Model API key dan provider account dapat dibuat setelah masuk dashboard; routing baru tersedia sesuai key, provider, account, dan model yang kemudian dikonfigurasi |
| 187 | Domain/TLS pada first run | **Opsional dan dapat dilewati** — default loopback langsung dapat digunakan. Wizard domain/TLS boleh ditawarkan saat setup, tetapi tidak menghalangi penyelesaian first run dan dapat dijalankan kemudian dari dashboard |
| 188 | Alias dan custom model | **Parity penuh seperti upstream** — create, edit, delete, mapping ke model provider, metadata/parameter/capability terkait, resolution order, dashboard, Admin API, dan CLI dipertahankan |
| 189 | Manajemen combo | **Parity penuh seperti upstream** — create, edit, delete, urutan model, strategi sequential fallback, round-robin, capability auto-switch, fusion/judge settings, status, dashboard, CLI, dan Admin API dipertahankan |
| 190 | Disabled-model workflow | **Pertahankan seperti upstream** — admin dapat enable/disable model individual tanpa menghapus provider account melalui dashboard, Admin API, dan CLI. Efek visibility serta routing mengikuti kontrak original |
| 191 | Provider node management | **Parity penuh seperti upstream** — create, edit, delete, test, reorder, base URL, model discovery, routing/fallback node, dashboard, Admin API, dan CLI dipertahankan |
| 192 | Retensi custom model saat discovery | **Tidak pernah dihapus otomatis** — refresh/discovery memperbarui model yang dilaporkan provider, tetapi custom model, alias, dan disabled state buatan admin tetap tersimpan sampai admin mengubah atau menghapusnya sendiri |
| 193 | Test All provider | **Pertahankan seperti upstream** — dashboard menyediakan pengujian massal account/provider per kelompok dengan concurrency terbatas, progress, hasil per account, pembaruan status/cooldown yang kompatibel, dan audit trail |
| 194 | Preferensi bahasa dashboard | **Per browser** — setiap browser menyimpan pilihan English atau Indonesia secara lokal. Default mengikuti locale browser bila didukung, lalu fallback ke English; pilihan bahasa bukan setting global server |
| 195 | Preferensi theme dashboard | **Per browser** — pilihan light, dark, atau system disimpan lokal di browser dan tidak mengubah konfigurasi PostgreSQL/global instalasi |
| 196 | Zona waktu tampilan | **Timezone browser** — database dan Admin API/export menyimpan serta mengirim timestamp UTC yang eksplisit; dashboard mengonversi usage, log, audit, job, cooldown, dan waktu lain ke zona browser |
| 197 | Kanal alert operasional | **Dashboard saja** — kegagalan backup, auth provider, update, job, database, dan alert penting lain ditampilkan melalui dashboard, tray status, serta structured logs. Tidak ada email, webhook, Discord, atau layanan notifikasi eksternal pada v1 |
| 198 | Notifikasi desktop native | **Tidak ada** — gorouter tray tidak mengirim toast/notifikasi OS. Pengguna melihat status melalui ikon/menu tray, dashboard, dan log |
| 199 | Visibilitas health endpoint | **Basic public, detail authenticated** — endpoint publik hanya menyediakan liveness/readiness dan informasi versi minimum yang aman. Detail database, provider, account, scheduler, backup, config, dan diagnostic memerlukan dashboard session atau PAT |
| 200 | Manual export/import konfigurasi | **Tidak tersedia** — gorouter tidak menyediakan transfer konfigurasi manual melalui dashboard, CLI, atau Admin API. Backup/restore otomatis dan disaster recovery sesuai #48 tetap tersedia sebagai mekanisme pemulihan, bukan fitur pemindahan konfigurasi manual |
| 201 | Download backup otomatis | **Tersedia di dashboard tanpa enkripsi aplikasi** — admin dapat mengunduh file backup yang telah dibuat dan diverifikasi scheduler. Ini bukan export konfigurasi terpisah; file mengikuti format backup/disaster-recovery resmi dan harus diperlakukan sebagai file sensitif karena memuat credential runtime |
| 202 | Akses restore backup | **CLI lokal saja** — restore yang bersifat destruktif hanya dapat dipicu dari host melalui CLI gorouter. Dashboard dan remote PAT hanya dapat melihat status, verifikasi, dan daftar backup, tidak menjalankan restore |
| 203 | Retensi backup default | **30 backup harian** — scheduler mempertahankan satu backup harian tervalidasi selama 30 hari secara default. Deployment server juga mempertahankan dukungan WAL/PITR sesuai #48 |
| 204 | Enkripsi file backup | **Tidak dienkripsi oleh gorouter** — backup otomatis maupun file yang diunduh tidak memakai password, passphrase, atau key enkripsi aplikasi. Keamanan mengandalkan permission filesystem, storage/deployment security, dan kontrol akses download; dokumentasi wajib menandai backup sebagai data sangat sensitif |
| 205 | Distribusi Windows | **Installer dan portable ZIP** — installer resmi mengelola binary, tray, bundled PostgreSQL, update, recovery, dan uninstall. Portable ZIP tersedia untuk advanced user yang memilih setup lebih manual |
| 206 | Distribusi macOS | **Signed/notarized app dengan installer image** — aplikasi menu-bar/tray, bundled PostgreSQL, updater/recovery, dan artefak DMG/PKG resmi ditandatangani serta dinotarized |
| 207 | Distribusi Linux | **DEB, RPM, Docker image, dan tarball** — paket distro menyediakan integrasi systemd; container image resmi untuk deployment container; tarball portable untuk advanced setup. Semua memakai binary dan kontrak gorouter yang sama |
| 208 | PostgreSQL pada portable ZIP Windows | **Bundled portable PostgreSQL** — archive membawa runtime PostgreSQL terkelola dengan data lokal. gorouter menginisialisasi, menjalankan, meng-upgrade, membackup, dan menyediakan recovery tanpa installer sistem |
| 209 | PostgreSQL untuk Docker | **Container terpisah melalui Compose** — image gorouter hanya aplikasi. Compose resmi menyediakan image PostgreSQL, volume persistent, health dependency, dan konfigurasi backup; operator tetap dapat memakai PostgreSQL eksternal |
| 210 | PostgreSQL untuk tarball Linux | **External PostgreSQL wajib** — tarball berisi binary/asset gorouter tanpa bundled database. Operator advanced menyediakan PostgreSQL 16, 17, atau 18 dan connection configuration sendiri |
| 211 | PostgreSQL pada paket DEB/RPM | **Gunakan dependency PostgreSQL dari repository distro** — package manager memasang atau menggunakan versi PostgreSQL yang didukung, lalu setup gorouter menginisialisasi database/role secara otomatis. External `DATABASE_URL` tetap didukung |
| 212 | PostgreSQL pada macOS app | **Dibawa dalam app/installer** — runtime PostgreSQL yang sudah diverifikasi ikut artefak signed/notarized sehingga setup desktop dapat berjalan offline dan konsisten; installer mengelola lifecycle serta data di lokasi aplikasi yang sesuai |
| 213 | Lokasi state portable Windows | **Seluruhnya dalam folder portable** — binary, config, `.env`, PostgreSQL runtime/data, backup, dan log berada di tree portable. Folder hanya dipindahkan/disalin ketika gorouter serta PostgreSQL telah dihentikan dengan aman |
| 214 | User service Linux | **User sistem khusus `gorouter`** — paket DEB/RPM membuat account tanpa login shell; service utama berjalan non-root dan permission config, data, backup, serta log dibatasi untuk account tersebut |
| 215 | Elevasi privilege fitur host | **Helper kecil terpisah dengan izin minimal** — binary utama tetap non-root. Operasi opt-in yang benar-benar memerlukan privilege memakai helper terverifikasi melalui polkit, sudo rule spesifik, atau mekanisme platform setara; bukan menjalankan seluruh service sebagai root |
| 216 | Fitur host-local pada Docker | **Nonaktifkan fitur yang tidak cocok** — core API, dashboard, provider, routing, dan workflow yang relevan tetap tersedia. Tray, self-updater, MITM host trust, serta process/host controls yang tidak aman atau tidak bermakna dalam container dinonaktifkan secara eksplisit dan terlihat di UI/API |
| 217 | Kedudukan metode instalasi | **Semua metode instalasi resmi setara** — npm, Winget, Homebrew, DEB/RPM, Docker, installer native, dan portable package ditampilkan menurut platform serta kebutuhan tanpa menetapkan satu Quick Start utama. Jalur npm mempertahankan UX sederhana `npm install -g gorouter` lalu `gorouter`; paket npm hanya mengunduh, memverifikasi, dan menjalankan binary Go yang sesuai, sementara setup PostgreSQL serta first-run tetap otomatis sesuai bentuk distribusinya. |
| 218 | Instalasi binary melalui npm | **Unduh saat `postinstall`** — `npm install -g gorouter` mendeteksi OS dan arsitektur, mengunduh binary native resmi yang sesuai, memverifikasi checksum serta signature/provenance, lalu memasang launcher `gorouter`. npm hanya menjadi installer/launcher lintas platform; runtime utama tetap binary Go. |
| 219 | Setup PostgreSQL melalui instalasi npm | **Otomatis penuh** — pada first run, gorouter mendeteksi atau menyiapkan PostgreSQL terkelola yang sesuai platform, menginisialisasi database dan role, menjalankan migration, memverifikasi health, lalu membuka dashboard tanpa meminta pengguna menyiapkan `DATABASE_URL` secara manual. PostgreSQL eksternal tetap dapat digunakan oleh advanced user sesuai keputusan distribusi sebelumnya. |
| 220 | Platform npm tanpa binary resmi | **Gagal secara jelas** — instalasi berhenti dengan pesan platform/arsitektur tidak didukung, daftar target resmi, serta tautan metode instalasi alternatif. Installer tidak mencoba compile dari source dan tidak diam-diam berpindah ke Docker. |
| 221 | Pembaruan instalasi npm | **Mengikuti versi package npm** — `npm update -g gorouter` memasang binary native yang persis cocok dengan versi package `gorouter` yang terpasang. Binary diganti secara tervalidasi, sedangkan konfigurasi, PostgreSQL, data, backup, dan state pengguna dipertahankan serta migration dijalankan sesuai versi. |
| 222 | Kegagalan setup PostgreSQL otomatis | **Recovery terpandu tanpa reset otomatis** — gorouter menampilkan penyebab yang aman, menyediakan retry, pilihan menggunakan PostgreSQL eksternal, dan jalur recovery. Data atau cluster PostgreSQL tidak dihapus, diinisialisasi ulang, maupun ditimpa secara otomatis ketika setup gagal. |
| 223 | Perilaku default perintah `gorouter` | **Start lalu buka dashboard** — jika server belum berjalan, `gorouter` memulai runtime, menunggu health ready, lalu membuka dashboard di browser default. Jika runtime sudah berjalan, perintah cukup membuka dashboard yang aktif. Mode server/headless tetap tersedia melalui subcommand atau flag dan tidak membuka browser sesuai keputusan #184. |
| 224 | Lifecycle server dari launcher terminal | **Tetap berjalan setelah terminal ditutup** — perintah `gorouter` berfungsi sebagai launcher yang memastikan runtime berjalan melalui background/user service yang sesuai platform. Menutup terminal tidak menghentikan server; penghentian dilakukan melalui tray, CLI, dashboard, atau service manager sesuai mode instalasi. |
| 225 | Password awal dashboard | **Gunakan initial password `12345678`** — instalasi baru menyediakan password dashboard awal tersebut agar pengguna dapat langsung login. Kebijakan perubahan wajib saat login pertama akan ditentukan secara terpisah; penyimpanan runtime tetap menggunakan hash satu arah sesuai keputusan auth. |
| 226 | Konflik port saat startup | ~~Cari port kosong secara otomatis~~ — **dibatalkan oleh keputusan #248**; gorouter tidak mengganti port secara otomatis. |
| 227 | Kewajiban mengganti password awal | **Tidak wajib** — password awal `12345678` dapat terus digunakan setelah login pertama. Dashboard menampilkan peringatan keamanan yang jelas selama password default masih aktif, tetapi tidak menahan akses konfigurasi atau routing. |
| 228 | Listener dashboard dan Model API | **Tetap satu port** — dashboard, Admin API, health, dan seluruh Model API/compatibility routes menggunakan satu listener/effective port yang sama seperti bentuk produk upstream; pemisahan listener tidak menjadi fitur v1. |
| 229 | Waktu pembuatan background service | **Saat first run** — `npm install -g gorouter` hanya memasang dan memverifikasi binary/launcher. Saat perintah `gorouter` pertama kali dijalankan, launcher menyiapkan integrasi user service sesuai platform, memulai runtime, menunggu health ready, lalu membuka dashboard. |
| 230 | Izin pembuatan background service | **Minta elevasi otomatis bila diperlukan** — first run memunculkan prompt izin native OS hanya untuk langkah yang membutuhkan privilege, lalu melanjutkan setup service. Binary utama tetap berjalan dengan privilege minimum dan tidak dijalankan permanen sebagai administrator/root. |
| 231 | Persistensi port otomatis | ~~Simpan port alternatif permanen~~ — **dibatalkan oleh keputusan #248** karena tidak ada pemilihan port otomatis. |
| 232 | Publikasi dengan password default | **Diizinkan tanpa pembatasan khusus** — LAN, domain, dan akses publik tetap dapat diaktifkan ketika password masih `12345678`; gorouter tidak memblokir, meminta pergantian, atau memerlukan konfirmasi risiko khusus berdasarkan status password default. |
| 233 | Uninstall melalui npm | **Hapus aplikasi tetapi pertahankan data** — `npm uninstall -g gorouter` menghentikan dan menghapus integrasi service serta launcher/binary yang dikelola npm, tetapi tidak menghapus PostgreSQL data, konfigurasi, credential, log, maupun backup. Reinstall dapat menemukan dan menggunakan kembali state tersebut setelah verifikasi/migration yang diperlukan. |
| 234 | Kegagalan download binary npm | **Retry lalu gagal aman** — installer mencoba ulang dengan backoff terbatas, membuang file parsial, dan hanya mengganti binary setelah download, checksum, signature/provenance, serta staging instalasi tervalidasi. Instalasi lama yang sehat tidak disentuh; kegagalan menampilkan cara retry. |
| 235 | Subcommand lifecycle service | **Sediakan lifecycle CLI lengkap** — selain perintah default `gorouter`, tersedia `gorouter start`, `gorouter stop`, `gorouter restart`, `gorouter status`, dan `gorouter logs`. Semua subcommand memakai integrasi service platform yang sama dan mempertahankan aturan graceful drain/force shutdown yang telah diputuskan. |
| 236 | Penghapusan seluruh state | **Subcommand purge dengan konfirmasi eksplisit** — tersedia `gorouter uninstall --purge` atau command setara untuk menghentikan service lalu menghapus binary terkelola, PostgreSQL terkelola beserta data, konfigurasi, credential, log, dan backup. Operasi menampilkan cakupan penghapusan, meminta konfirmasi kuat, dan tidak dapat dipicu tanpa sengaja oleh uninstall npm biasa. |
| 237 | Perilaku default `gorouter logs` | **Follow log live** — perintah langsung mengikuti structured runtime logs sampai dihentikan pengguna. Opsi seperti `--lines`, `--since`, dan `--no-follow` menyediakan riwayat terbatas atau output satu kali tanpa mengubah penyimpanan log PostgreSQL/stdout. |
| 238 | Downgrade melalui npm | **Tolak jika schema/state tidak kompatibel** — sebelum menjalankan binary versi lama, gorouter membandingkan compatibility metadata dan schema PostgreSQL. Jika state dibuat oleh versi yang tidak didukung, runtime berhenti aman dan menunjukkan versi kompatibel atau jalur restore manual; tidak mencoba menjalankan atau merestore backup otomatis. |
| 239 | Output `gorouter status` | **Ringkasan runtime dan dependency penting** — tampilkan state service, PID, uptime, versi, effective port, dashboard URL, PostgreSQL health/versi, jumlah stream aktif, serta status update/restart/migration yang relevan tanpa membocorkan credential. |
| 240 | Restart tertahan stream aktif | **Tampilkan progres dan instruksi force** — restart normal tetap menunggu seluruh stream tanpa batas sesuai keputusan drain. CLI memperbarui jumlah/durasi stream aktif dan menjelaskan bahwa operator lokal dapat memakai `gorouter stop --force` atau command force setara bila benar-benar ingin memutus stream. Tidak ada force otomatis. |
| 241 | Output machine-readable CLI lifecycle | **Tambahkan flag `--json`** — tampilan manusia menjadi default, sedangkan start, stop, restart, status, logs, purge, dan command operasional lain menyediakan JSON terstruktur atau event JSON ketika relevan untuk scripting/CI. Exit code tetap stabil dan terdokumentasi. |
| 242 | Startup setelah reboot | **Otomatis hanya pada instalasi server** — DEB/RPM, Windows Service/server mode, Docker, dan deployment server resmi otomatis menjalankan gorouter setelah reboot sesuai service manager. Instalasi npm serta desktop tidak auto-start; pengguna menjalankan `gorouter` ketika ingin memulai runtime. Keputusan ini konsisten dengan larangan autostart desktop #178. |
| 243 | PostgreSQL terkelola saat uninstall biasa | **Hapus runtime, pertahankan data** — uninstall gorouter menghapus binary/runtime PostgreSQL yang dikelola distribusi tersebut, tetapi mempertahankan database cluster/data directory, konfigurasi, dan backup. Reinstall memasang runtime kompatibel lalu memverifikasi serta menggunakan kembali data. Purge eksplisit tetap menghapus semuanya sesuai #236. |
| 244 | Update ketika runtime aktif | **Stage, drain, swap, health-check, dan rollback** — updater mengunduh serta memverifikasi artefak, menyiapkan compatibility/migration, menghentikan penerimaan request baru, menunggu stream aktif sesuai drain policy, mengganti binary secara atomik, menjalankan health gate, dan mengembalikan binary/state yang masih schema-compatible bila startup atau health gagal. |
| 245 | Rentang pencarian port otomatis | ~~Seratus port berurutan~~ — **dibatalkan oleh keputusan #248**. |
| 246 | Konflik ulang pada port tersimpan | ~~Cari lalu simpan port baru~~ — **dibatalkan oleh keputusan #248**. |
| 247 | Fallback ketika kandidat port habis | ~~Minta OS memilih port kosong~~ — **dibatalkan oleh keputusan #248**. |
| 248 | Kebijakan port final | **Port stabil dan startup gagal jika bentrok** — gorouter selalu menggunakan port yang ditentukan oleh precedence konfigurasi atau default. Jika port telah dipakai, gorouter tidak mencari port lain, tidak mengubah konfigurasi, dan tidak membunuh proses pemilik. Startup berhenti dengan diagnosis aman, identitas proses pemilik bila dapat dibaca, serta instruksi menetapkan port eksplisit lalu mencoba kembali. |
| 249 | Rollback update setelah migration schema | **Hanya jika terbukti kompatibel** — binary lama dipulihkan otomatis hanya bila compatibility metadata membuktikan versi tersebut dapat membaca schema yang telah dimigrasikan. Jika tidak kompatibel, gorouter masuk recovery/safe mode dan mempertahankan database tanpa restore otomatis; operator memilih tindakan berikutnya. |
| 250 | Update tertahan graceful drain | **Tetap menunggu tanpa batas** — `npm update -g gorouter` atau updater lain menampilkan jumlah serta durasi stream aktif dan tidak menyelesaikan aktivasi sampai seluruh stream selesai atau operator lokal memilih force. Binary lama tetap melayani stream selama drain dan staged update tidak diaktifkan diam-diam. |
| 251 | Kegagalan health-check binary baru | **Satu startup lalu rollback** — setelah swap, gorouter menjalankan satu percobaan startup dengan health timeout yang terdokumentasi. Jika gagal dan schema masih kompatibel, updater langsung mengembalikan binary lama serta memverifikasi health-nya; tidak melakukan restart loop pada binary baru. |
| 252 | Konflik keputusan dengan upstream | **Selalu tanya ulang pengguna** — ketika audit atau implementasi menemukan perilaku upstream yang bertentangan dengan keputusan terdokumentasi, pekerjaan pada kontrak tersebut berhenti dan assistant menyajikan fakta source, pilihan, dampak kompatibilitas, risiko, serta biaya. Upstream maupun keputusan lama tidak menang otomatis sampai pengguna menegaskan keputusan final. |
| 253 | Keputusan yang dibatalkan | **Simpan sebagai histori, abaikan untuk implementasi** — keputusan lama tetap terlihat dicoret atau ditandai dibatalkan untuk audit trail, tetapi specification, ledger, plan, code, dan tests hanya menggunakan keputusan pengganti terbaru yang aktif. |
| 254 | Bentuk ledger parity | **Per fitur dengan status preserve/change/remove** — sebelum coding, setiap endpoint, provider, OAuth flow, routing behavior, protocol/translator, dashboard workflow, CLI command, persistence/state behavior, background job, packaging, dan host integration dicatat sebagai fitur dengan status final, keputusan referensi, kontrak observable, risiko, serta parity/acceptance tests. Mapping source-ke-Go dapat menjadi detail pendukung, bukan ledger utama. |
| 255 | Struktur spesifikasi final | **Dokumen per domain dengan index pusat** — contracts/API, providers/OAuth, routing/streaming, data/background jobs, dashboard/CLI, packaging/security, serta domain relevan lain memiliki dokumen terpisah yang saling terhubung dari satu index dan ledger parity pusat. |
| 256 | Persetujuan ledger parity | **Setujui seluruh ledger sekaligus** — coding fitur belum dimulai sampai seluruh domain selesai dipetakan, seluruh baris preserve/change/remove konsisten, konflik dibawa kembali kepada pengguna, dan ledger lengkap disahkan pengguna dalam satu approval final. |
| 257 | Gate merge modul Go | **Seluruh Definition of Done modul wajib lulus** — source mapping, kontrak observable, parity/unit/integration/golden tests, review terhadap upstream, dokumentasi, serta bukti edge/error path harus lengkap sebelum modul digabung ke branch utama. Kode parsial tidak digabung hanya karena compile atau happy path berhasil. |
| 258 | Karakter visual dashboard | **Professional utilitarian** — interface harus terasa terpercaya, presisi, efisien, dan berorientasi workflow seperti engineering/operations tool enterprise. Dekorasi diminimalkan; hierarki, status, data, dan tindakan lebih penting daripada efek visual. |
| 259 | Kepadatan informasi desktop | **Compact** — tabel, toolbar, filter, status, dan form menggunakan spacing ringkas agar banyak informasi terlihat tanpa scrolling berlebihan. Kepadatan tetap menjaga keterbacaan, target interaksi, keyboard navigation, contrast, dan accessibility. |
| 260 | Navigasi utama dashboard | **Sidebar berkelompok dan collapsible** — fitur dikelompokkan berdasarkan domain seperti Routing, Providers, Usage, Tools, Host, dan Settings. Top bar memuat konteks global dan tindakan lintas halaman; struktur final tetap mengikuti ledger capability upstream. |
| 261 | Referensi visual normatif dashboard | **App shell dan visual language mengikuti OpenRouter pada screenshot referensi** — proporsi top bar/sidebar/content, density, spacing, typography hierarchy, surface/border treatment, tabel, control, ikon, state, dan aksen ungu dibuat sangat dekat dengan referensi. Identitas, logo, nama, copy, menu, serta capability tetap milik gorouter dan tidak menyalin konten produk OpenRouter yang tidak relevan. |
| 262 | Dukungan tema dashboard | **Light dan dark setara** — light mode mengikuti referensi OpenRouter secara dekat. Dark mode menerjemahkan shell, hierarchy, density, semantic colors, border, focus, table, dan interaction states yang sama dengan contrast/accessibility setara; bukan mengambil tampilan 9Router original. |
| 263 | Isi top navigation | **Gunakan fitur global gorouter dalam pola visual referensi** — top bar mempertahankan komposisi OpenRouter tetapi isinya disesuaikan menjadi global search, model catalog, basic chat, docs/help, runtime status, theme/profile, atau fungsi global gorouter lain yang benar-benar ada. Seluruh konfigurasi domain tetap berada di sidebar. |
| 264 | Warna aksen per tema | **Sama seperti referensi OpenRouter** — light mode memakai purple untuk primary actions, active/focus states, dan highlight utama; dark mode memakai electric lime untuk peran yang sama. Semantic success/warning/error/info tetap memiliki warna tersendiri dan memenuhi contrast. |
| 265 | Theme switcher | **Segmented control tiga pilihan di profile dropdown** — bagian bawah dropdown menyediakan ikon Light, Dark, dan System dengan pola visual seperti referensi. Pilihan disimpan per browser sesuai keputusan #195 dan seluruh interaction/focus/keyboard state wajib accessible. |
| 266 | Isi profile dropdown | **Pola OpenRouter dengan capability gorouter** — dropdown kompak memiliki header identitas admin, Profile, Activity/Audit, Logs, Preferences, Sign Out, dan theme switcher bawah. Item lain hanya ditambahkan bila benar-benar merupakan fitur gorouter; Workspaces, Credits, Labs, atau menu OpenRouter tidak disalin tanpa capability terkait. |
| 267 | Sidebar responsif | **Drawer dari kiri** — pada layar sempit sidebar desktop disembunyikan dan dibuka sebagai left drawer melalui top bar. Konten memakai lebar penuh; drawer memiliki overlay, focus management, keyboard close, dan touch behavior yang accessible. |
| 268 | Selector di atas sidebar | **Tidak ada workspace selector** — gorouter v1 single-server/single-tenant tidak menambahkan konsep workspace yang tidak diperlukan. Sidebar langsung dimulai dari domain capability gorouter. |
| 269 | Pola data dashboard | **Tabel OpenRouter-like** — provider accounts, models, usage, logs, audit, dan data besar menggunakan border tipis, header ringan, baris compact, inline status/action, toolbar search/filter, pagination atau virtualization saat diperlukan, serta responsive treatment tanpa kehilangan informasi penting. |
| 270 | Tipografi dashboard | **System sans seperti referensi** — gunakan system/modern sans dengan ukuran, weight, line-height, tracking, label, heading, tabel, dan muted text yang dekat dengan screenshot OpenRouter. Tidak memakai display font dekoratif atau monospace sebagai font UI utama. |
| 271 | Bentuk form controls | **Compact OpenRouter-like** — tombol, input, select, tab, menu, dan badge memakai radius kecil/medium, border tipis, tinggi ringkas, primary action solid sesuai aksen tema, serta vocabulary control yang konsisten. Pill hanya untuk status/tag yang memang membutuhkan bentuk tersebut. |
| 272 | Feedback dan state UI | **Inline-first** — loading, success, warning, error, validation, cooldown, dan provider status muncul dekat objek/tindakan terkait. Toast dipakai untuk konfirmasi singkat; error penting tidak hanya muncul sebagai toast yang menghilang; modal dipakai hanya ketika flow memang membutuhkan keputusan terfokus. |
| 273 | Global search dashboard | **Dilewati** — global search pada top bar tidak termasuk scope UI yang sedang ditetapkan. Fokus navigasi dan workflow berada pada sidebar serta halaman capability terkait. |
| 274 | Halaman Home/Overview | **Dilewati** — halaman Overview/Home tidak menjadi fokus desain ini dan boleh diabaikan; implementasi UI memprioritaskan halaman operasional/data seperti referensi screenshot. |
| 275 | Konfirmasi aksi berisiko | **Inline confirmation** — konfirmasi ditempatkan dekat aksi dengan dampak yang jelas. Operasi destruktif dapat meminta teks konfirmasi atau password/PAT sesuai tingkat risiko; seluruh tindakan tetap dicatat di audit trail. |
| 276 | Isi dashboard gorouter | **Pertahankan isi dan capability 9Router original dengan UX OpenRouter** — halaman dan capability seperti Endpoint & Key, Providers, Combos, Usage, Quota Tracker, Token Saver, CLI Tools, Media Providers, Proxy Pools, Skills, Console Log, Remote, Settings, serta workflow terkait tetap tersedia. Struktur data, field, status, action, dan warning mengikuti fungsi original secara kurang-lebih; tambahan enterprise seperti health, audit, scheduler, status provider, dan recovery boleh ditambahkan tanpa menghapus capability original. |
| 277 | Referensi halaman Endpoint | **Recreate halaman Endpoint dari referensi gabungan** — app shell, dark/light visual language, section card, API endpoint display, Local/Tunnel/Tailscale controls, warning inline, API Keys section, create action, masked key rows, dates, toggles, spacing, border, iconography, dan responsive behavior mengikuti screenshot 9Router/OpenRouter. Nilai konfigurasi dan capability tetap berasal dari gorouter. |
| 278 | Batas redesign dashboard | **UI/UX OpenRouter, capability 9Router** — tampilan boleh direstrukturisasi agar konsisten dengan OpenRouter, tetapi endpoint, provider workflow, routing, key management, usage, tools, host controls, OAuth, quota, dan data semantics tidak boleh hilang atau diganti tanpa keputusan preserve/change/remove pada ledger parity. |
| 279 | Struktur header halaman | **Header OpenRouter-like** — setiap halaman operasional memakai judul dan deskripsi ringkas di kiri, dengan primary action dan contextual actions di kanan. Pada layar sempit, action turun secara teratur tanpa menutupi judul atau kehilangan hierarchy. |
| 280 | Sistem ikon dashboard | **Outline icons yang konsisten** — sidebar, top bar, tombol, status, dan contextual actions menggunakan satu keluarga outline icon dengan ukuran dan stroke seragam. Ikon mendukung label dan tidak menggantikan teks pada aksi yang ambigu atau kritis. |
| 281 | Tabel lebar pada mobile | **Horizontal scroll terkontrol** — tabel operasional yang tidak dapat dipadatkan tetap mempertahankan kolom penting dan memakai horizontal scrolling pada layar sempit, dengan header/action penting tetap mudah ditemukan; data tidak diubah menjadi kartu secara otomatis jika mengurangi kemampuan membandingkan baris. |
| 282 | Struktur halaman detail | **Sections + tabs** — halaman detail Provider, Combo, Proxy Pool, API Key, dan entitas kompleks lain memakai header ringkas, ringkasan/status utama, lalu tabs untuk kelompok informasi besar dan sections untuk field terkait. |
| 283 | Form kompleks | **Dedicated page** — create/edit Provider, Combo, Proxy Pool, dan form kompleks lain memakai halaman penuh. Drawer/modal dipakai untuk aksi pendek, konfirmasi, atau edit kecil agar form utama tetap fokus dan tidak terpotong. |
| 284 | Loading, empty, dan error state | **Contextual states** — skeleton mengikuti bentuk konten; empty state menjelaskan kondisi dan memberi aksi relevan; error tampil inline dekat area terkait, mempertahankan data lama jika tersedia, dan menyediakan retry yang jelas. |
| 285 | Aksesibilitas dashboard | **WCAG 2.2 AA** — keyboard navigation, visible focus, semantic labels, contrast, reduced motion, dan screen-reader support menjadi release gate dashboard. |
| 286 | Motion dashboard | **Minimal functional motion** — transisi singkat hanya untuk drawer, tabs, dropdown, status, dan feedback yang membantu operasi; tidak ada animasi dekoratif yang mengganggu workflow. |
| 287 | Pola halaman analytics | **Activity/OpenRouter-like** — Usage, Quota, Cost, dan Provider Health memakai metric cards compact dengan sparkline di atas, grid chart dua kolom, chart tren full-width saat relevan, legend, tooltip, dan action `Explore`. Visual tetap operational, bukan marketing dashboard. |
| 288 | Perbandingan metric | **Wajib bila datanya mendukung** — metric card menampilkan nilai saat ini, delta, arah naik/turun, dan label periode pembanding seperti `vs prev period`. Jika baseline tidak tersedia, UI menjelaskan bahwa perbandingan belum tersedia. |
| 289 | Filter halaman analytics | **Global page filters** — timezone, date range, preset, dan filter global di header berlaku ke seluruh cards, tabel, dan chart pada halaman. Breakdown lokal hanya menambah detail dan tidak diam-diam memakai rentang berbeda. |
| 290 | Responsive analytics | **Stacked operational layout** — cards beralih dari grid 2 kolom ke 1 kolom, chart grid menjadi satu kolom, filter header boleh wrap, dan chart lebar tetap bisa di-scroll horizontal bila dibutuhkan. Tidak memakai horizontal scroll untuk seluruh halaman. |
| 291 | Kepadatan tabel | **Compact but readable** — tabel mengikuti kepadatan OpenRouter dengan baris ringkas, tetapi target klik, focus state, keyboard navigation, dan touch spacing tetap aman. |
| 292 | Live data refresh | **Quiet live refresh** — polling/SSE memperbarui data tanpa mereset scroll atau filter, menampilkan indikator kecil seperti `Updated just now`, dan tidak melakukan flashing pada seluruh panel. |
| 293 | Usage capability | **Pertahankan seluruh struktur Usage original** — tab Overview/Details, preset waktu, metric utama, topology, Recent Requests, chart breakdown, Tokens/Cost toggle, usage type, request volume, token breakdown, prompt caching, serta tabel model/provider expandable tetap menjadi capability wajib. Visual mengikuti OpenRouter-like UI. |
| 294 | Usage topology | **Wajib di Overview** — graph provider/model/API key/route menjadi bagian utama Usage Overview untuk menunjukkan aliran routing dan membantu diagnosis, bukan capability opsional yang boleh hilang. |
| 295 | Usage table hierarchy | **Expandable row wajib** — baris model/provider dapat dibuka untuk melihat detail account/request/cost tanpa meninggalkan halaman, dengan Explore/detail workflow bila informasi terlalu besar. |
| 296 | Interaksi topology graph | **Zoom + pan + inspect** — graph Usage mendukung zoom, pan, reset viewport, klik node/edge untuk melihat detail, dan highlight jalur routing terkait. Kontrol tetap ringkas dan tidak menutupi graph. |
| 297 | Recent Requests | **Feed monitoring realtime saja** — panel Recent Requests dipakai untuk melihat request yang sedang/baru terjadi secara realtime. Panel ini bukan inspector atau navigasi detail; detail historis dan troubleshooting berada pada workflow Details/Explore. |
| 298 | Persistensi filter Usage | **Persist global filters** — date range, timezone, preset, model/provider/API key filter tetap dipertahankan saat berpindah antara Overview, Details, dan Explore, tanpa reset diam-diam. |
| 299 | Recent Requests buffer | **Kapasitas 50 item** — mengikuti `RING_CAP = 50` pada 9Router original untuk feed realtime in-memory. Buffer bukan histori permanen; data historis tetap berasal dari Usage Details/Request Logger. |
| 300 | Recent Requests default display | **20 item terbaru** — mempertahankan behavior original: filter token-zero, deduplicate, sort newest-first, lalu tampilkan maksimum 20 item pada card Overview. Item hingga kapasitas buffer dapat diakses melalui workflow detail/Explore. |
| 301 | Recent Requests delivery | **SSE newest-first tanpa auto-scroll bawaan** — feed menerima update realtime melalui SSE, menempatkan request terbaru di atas, memakai sticky header, dan tidak memaksakan scroll-to-bottom, pause, atau follow toggle karena behavior itu tidak ada di original. |
| 302 | Sidebar navigation | **Urutan original 9Router** — Endpoint & Key, Providers, Combos, Usage, Quota Tracker, Token Saver, CLI Tools, Media Providers, Proxy Pools, Skills, Console Log, Remote, Settings tetap menjadi urutan utama navigasi. Visual shell dan grouping ringan boleh mengikuti OpenRouter tanpa memindahkan capability secara membingungkan. |
| 303 | Recent Requests display | **20 tampil / 50 buffer** — Overview card menampilkan maksimum 20 item setelah filter/dedup/sort seperti original; buffer realtime menyimpan kapasitas 50 dan data lebih lengkap tersedia melalui Details/Explore. |
| 304 | Recent Requests UX | **Preserve original behavior** — SSE, newest-first, sticky header, tanpa forced auto-scroll, pause, atau follow toggle. Redesign hanya mengubah visual hierarchy dan komponen, bukan perilaku feed. |
| 305 | Batas rewrite dan tambahan | **Rewrite original, tambahan minimal** — gorouter adalah rewrite 9Router original, bukan produk baru. Capability, workflow, dan data semantics utama harus mengikuti original. Tambahan hanya sedikit, jelas manfaatnya, dan tidak boleh berkembang menjadi kumpulan enterprise feature/domain baru tanpa keputusan terpisah. Setiap usulan wajib dilabeli sebagai original parity, small approved improvement, atau not included. |
| 270 | Tipografi dashboard | **System sans seperti referensi** — gunakan system/modern sans dengan ukuran, weight, line-height, tracking, label, heading, tabel, dan muted text yang dekat dengan screenshot OpenRouter. Tidak memakai display font dekoratif atau monospace sebagai font UI utama. |
| 271 | Bentuk form controls | **Compact OpenRouter-like** — tombol, input, select, tab, menu, dan badge memakai radius kecil/medium, border tipis, tinggi ringkas, primary action solid sesuai aksen tema, serta vocabulary control yang konsisten. Pill hanya untuk status/tag yang memang membutuhkan bentuk tersebut. |
| 272 | Feedback dan state UI | **Inline-first** — loading, success, warning, error, validation, cooldown, dan provider status muncul dekat objek/tindakan terkait. Toast dipakai untuk konfirmasi singkat; error penting tidak hanya muncul sebagai toast yang menghilang; modal dipakai hanya ketika flow memang membutuhkan keputusan terfokus. |
| 273 | Global search dashboard | **Dilewati** — global search pada top bar tidak termasuk scope UI yang sedang ditetapkan. Fokus navigasi dan workflow berada pada sidebar serta halaman capability terkait. |
| 274 | Halaman Home/Overview | **Dilewati** — halaman Overview/Home tidak menjadi fokus desain ini dan boleh diabaikan; implementasi UI memprioritaskan halaman operasional/data seperti referensi screenshot. |
| 275 | Konfirmasi aksi berisiko | **Inline confirmation** — konfirmasi ditempatkan dekat aksi dengan dampak yang jelas. Operasi destruktif dapat meminta teks konfirmasi atau password/PAT sesuai tingkat risiko; seluruh tindakan tetap dicatat di audit trail. |

### 16. Frontend Stack
- **Keputusan:** React + Vite + shadcn/ui + Tailwind
- **Alasan:**
  - Dashboard isinya tabel CRUD, forms, chart — cocok untuk React ecosystem
  - shadcn/ui: tabel, form, dialog, chart tinggal copy-paste, looks modern
  - User sudah familiar dengan React ecosystem dari 9router-mw (Next.js)
  - Build ke static files → tinggal copy ke Go embed atau serve via nginx
- **Bundle size:** ~150KB min
- **Build time:** 2-5s

### 17. Redis Dependency
- **Keputusan:** Tidak perlu Redis. Semua state pake PostgreSQL.
- **OAuth pending state:** tabel `oauth_sessions` dengan kolom `expires_at`
- **Alasan:**
  - Upstream `decolua/9router` juga tidak pakai Redis (SQLite + in-memory closure)
  - Go goroutine concurrent aman ke 1 PG pool — tidak perlu queue coordination
  - OAuth state TTL via query `WHERE expires_at < NOW()` — berkala cleanup goroutine
- **Konsekuensi:** 1 dependency kurang untuk di-deploy, maintain, dan monitor

### 18. Config Management
- **Keputusan:** godotenv (`.env` file)
- **Alasan:** Simpel, standard, seperti 9router-mw sekarang

### 19. OAuth Pending State
- **Keputusan:** PostgreSQL via tabel `oauth_sessions`
- **Schema (draft):**
  ```sql
  CREATE TABLE oauth_sessions (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      provider VARCHAR(32) NOT NULL,         -- 'codex', 'xai', 'gemini', dll
      state VARCHAR(128) NOT NULL,
      code_verifier TEXT NOT NULL,
      redirect_uri TEXT NOT NULL,
      status VARCHAR(16) NOT NULL DEFAULT 'pending',  -- 'pending', 'done', 'error'
      error_message TEXT,
      access_token TEXT,          -- diisi setelah callback sukses
      refresh_token TEXT,
      expires_at TIMESTAMPTZ NOT NULL,        -- TTL via query
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  CREATE INDEX idx_oauth_sessions_state ON oauth_sessions(provider, state);
  CREATE INDEX idx_oauth_sessions_expires ON oauth_sessions(expires_at);
  ```
- **Cleanup:** Goroutine tiap 5 menit: `DELETE FROM oauth_sessions WHERE expires_at < NOW() - INTERVAL '1 hour'`

### 20. Architecture Design Decisions
| # | Area | Decision |
|---|------|----------|
| 306 | Backend boundary | **Modular monolith** — satu binary Go dengan module terpisah untuk HTTP/API, request orchestration, routing, provider, streaming/translation, persistence, OAuth, background jobs, dan host integration. Tidak dipecah menjadi service terpisah tanpa keputusan baru. |
| 307 | Frontend/backend boundary | **Contract-first Admin API** — dashboard React berkomunikasi melalui versioned Admin API. Request, response, error, auth, dan capability contract diuji sebelum workflow UI dibangun. |
| 308 | Model API pipeline | **Satu orchestration pipeline terarah** — format request dideteksi/diterima, model/account/route diselesaikan, lalu translator/executor dipilih. Format khusus tetap boleh bypass bagian pipeline yang loss-y; tidak dipaksa melalui representasi yang merusak semantics. |
| 309 | Provider boundary | **Dua level provider adapter** — generic OpenAI/Anthropic-compatible adapter untuk provider standar, serta specialized adapter/executor untuk Codex, Gemini, Kiro, Cursor, binary/NDJSON, OAuth, media, dan protocol khusus sesuai parity original. |
| 310 | Persistence boundary | **Repository per domain** — provider connections, models/routing, usage, OAuth, settings, keys, combos, logs, dan domain lain memiliki repository/domain service terpisah; tidak memakai satu generic repository sebagai boundary utama. |
| 311 | Orchestration stages | **Explicit testable stages** — urutan Auth → format detection → normalization → model resolution → account/proxy selection → translation/executor → stream/response → usage/finalization diekspresikan eksplisit dan dapat dites per stage. |
| 312 | Streaming contract | **Mengikuti upstream** — pertahankan direct translator, native passthrough, protocol-specific event/terminal semantics, dan internal normalization hanya pada jalur yang aman. Jangan memaksa semua provider/format melalui satu representasi stream yang lossy. |
| 313 | Error boundary | **Dua lapis error** — client menerima error terstruktur dan sanitized; diagnostic internal menyimpan cause, retry/fallback trace, provider/model, dan correlation ID tanpa secret. Error client dan diagnostic tidak disamakan secara mentah. |
| 314 | Cancellation | **Full propagation** — disconnect/cancel diteruskan ke reader, translator, executor, provider request, timer, usage finalization, dan goroutine terkait; terminal event/resource cleanup tidak boleh duplikat atau bocor. |
| 315 | PostgreSQL transaction boundary | **Domain service owns transaction** — use-case lintas repository membuka satu transaksi dan memakai transaction-scoped repositories sehingga seluruh perubahan terkait commit atau rollback bersama. Repository tidak membuat transaksi terpisah yang memecah atomicity workflow. |
| 316 | Provider persistence shape | **Core columns + JSONB** — field umum seperti id, provider, authType, status, priority, dan timestamps menjadi kolom terstruktur; credential dan metadata khusus provider disimpan dalam JSONB agar parity provider-specific tetap fleksibel. |
| 317 | Schema migration model | **Versioned forward migrations** — migration SQL berurutan tertanam di binary dan dijalankan otomatis saat startup setelah preflight/backup. Rollback binary hanya diperbolehkan bila metadata schema menyatakan kompatibel; tidak memakai schema auto-reconciliation bebas. |
| 318 | Usage write path | **Async durable DB worker** — request menghasilkan usage event yang diproses asynchronous oleh worker internal berbasis PostgreSQL, tidak menahan response, tetapi accepted event harus survive restart dan tidak boleh hilang dalam kondisi normal. |
| 319 | Request details storage | **Metadata-only default** — simpan provider/model/account tersamarkan, tokens, cost, latency, status, sanitized error, fallback/retry trace, dan tool names. Prompt/response/body hanya disimpan melalui explicit debug opt-in. |
| 320 | Backup consistency | **Point-in-time consistent** — backup PostgreSQL harus konsisten secara transaksional dan dianggap valid hanya setelah restore verification berhasil. |
| 321 | Data retention | **90 hari default** — usage, request details, dan application logs dibersihkan otomatis setelah 90 hari melalui scheduler, dengan status cleanup terlihat di dashboard. |
| 322 | Provider credential storage | **Restricted plaintext provider data** — API keys/OAuth tokens/provider credentials tetap tersedia otomatis setelah restart dan disimpan di PostgreSQL dengan DB role, filesystem, deployment, dan backup access controls. PAT serta gorouter model keys tetap hash-only. |
| 323 | Runtime database lock | **Satu proses, banyak goroutine/worker** — satu proses gorouter boleh memiliki banyak goroutine/worker dan satu PostgreSQL pool. Proses gorouter kedua yang memakai database sama ditolak saat startup dengan error diagnosis yang jelas melalui PostgreSQL advisory lock. |
| 324 | Model resolution | **Persis upstream + extension** — pertahankan provider/model, alias tanpa slash, combo, built-in alias, inferred routes, dan urutan resolution original; setelah itu dukung extension `namespace/model(variant)`. |
| 325 | Account selection | **Priority + cooldown seperti upstream** — pilih account aktif berdasarkan priority, lewati cooldown/model lock, lalu fallback ke account berikutnya untuk model yang sama. Tidak mengganti behavior dengan round-robin atau health-weighted selection. |
| 326 | Routing state | **Memory + PostgreSQL checkpoint** — cooldown, model lock, dan rotation hot state berada di memory untuk latency rendah; state penting di-checkpoint ke PostgreSQL dan dipulihkan secara deterministik setelah restart. |
| 327 | Proxy selection | **Persis upstream** — pertahankan assignment, order/rotation, proxy pool, cooldown, test status, dan fallback original; tidak menambah algoritma proxy baru tanpa keputusan. |
| 328 | Same-model account fallback | **Fallback antar-account** — saat account pertama gagal sementara dan account lain untuk model sama tersedia, tandai/cooldown sesuai upstream lalu coba account berikutnya sebelum error. |
| 329 | Single-model fallback | **Tidak berpindah model otomatis** — request single-model tetap pada model tersebut. Fallback hanya antar-account/provider route yang melayani model sama; pindah model dilakukan melalui combo yang dipilih user. |
| 330 | Sequential combo | **Persis upstream** — jalankan model/account sesuai urutan combo; lanjut ke item berikutnya hanya jika error memenuhi fallback policy, sambil mempertahankan retry, cooldown, dan status. |
| 331 | Round-robin combo | **State rotation upstream** — pertahankan state rotasi, sticky count/selection, urutan, dan behavior restart sesuai original; tidak menggantinya dengan health-weighted atau random selection. |
| 332 | Capability auto-switch | **Persis upstream** — capability request memilih model kompatibel dalam combo; fallback model combo tidak boleh dibuang dan single-model request tidak boleh diubah otomatis. |
| 333 | Fusion combo | **Persis upstream** — panel model dipanggil paralel, tools/nonstream handling mengikuti original, quorum/grace/timeout dipertahankan, lalu judge melakukan synthesis. |
| 334 | Fusion degradation | **Degrade seperti upstream** — nol panel sukses menghasilkan error; satu panel sukses menjadi direct result; beberapa panel sukses lanjut ke judge/synthesis sesuai aturan original. |
| 335 | Provider retry policy | **Provider-aware upstream policy** — klasifikasikan status/error/transient condition per provider, retry terbatas dengan backoff, lalu lakukan account/model fallback sesuai policy; tidak memakai fixed retry universal. |
| 336 | Auth error handling | **Persis upstream** — untuk 401/403, refresh credential yang relevan lalu retry terbatas; credential baru dipersist bila berhasil; bila tetap gagal, account masuk cooldown/fallback sesuai policy. |
| 337 | Definitive credential invalidation | **Disable, jangan hapus** — refresh token invalid/revoked membuat account tetap tersimpan untuk audit tetapi dikeluarkan dari routing sampai user melakukan re-auth/import credential ulang. |
| 338 | Final routing error | **Sanitized structured error** — client menerima status/type/code/retryability/provider/model/safe message dan correlation ID tanpa token, URL sensitif, atau detail credential. |
| 339 | Cooldown duration | **Upstream/provider-aware policy** — account/model cooldown dan exponential backoff mengikuti klasifikasi error serta provider behavior original; tidak memakai satu durasi global. |
| 340 | Priority tie-break | **Preserve priority/order upstream** — priority manual dan urutan original tetap dihormati. Tie-break mengikuti selection state original, bukan latency/health score baru. |
| 341 | Routing state dashboard | **Status terlihat dan refresh** — dashboard menampilkan active, cooldown, disabled, dan error sesuai workflow original; perubahan dapat di-refresh/realtime tanpa menambah observability platform besar. |
| 342 | OAuth module boundary | **Per-provider flow seperti upstream** — random loopback, fixed callback port, device code, cookie/PAT import, IDE import, dashboard relay, dan mekanisme khusus lain dimodelkan sebagai flow module terpisah; tidak dipaksa ke satu OAuth flow universal. |
| 343 | OAuth pending state | **PostgreSQL with TTL** — state, PKCE, status, dan callback metadata sementara disimpan di PostgreSQL dengan expiry/cleanup, sehingga aman untuk goroutine concurrent dan dapat dipulihkan setelah restart bila masih valid. |
| 344 | OAuth credential persistence | **Provider connection JSONB** — access token, refresh token, expiry, account/workspace/project IDs, dan metadata provider-specific disimpan pada provider connection core columns + JSONB sebagai restricted plaintext. |
| 345 | OAuth callback contract | **Preserve provider contract** — fixed/random loopback port dan redirect URI dipertahankan sesuai kebutuhan upstream/provider; listener utama hanya digunakan bila provider memang mendukungnya. |
| 346 | OAuth pending session after restart | **Selalu batalkan** — startup menandai session OAuth pending dari runtime sebelumnya sebagai gagal/cancelled dan memberi error jelas. PostgreSQL pending state dipakai untuk concurrency/status/TTL dalam satu runtime, bukan melanjutkan browser/listener flow lintas restart. |
| 347 | OAuth account dedup | **Provider-specific seperti upstream** — dedup memakai email, workspace, account ID, provider, dan auth type sesuai provider. Access-token connection yang upstream tidak dedup tetap tidak dipaksa dedup. |
| 348 | OAuth refresh coordinator | **Satu coordinator** — proactive scheduler dan reactive request-path refresh memakai coordinator yang sama, dengan per-account singleflight, global semaphore, provider-aware backoff, credential persistence, dan status transition. |
| 349 | Refresh singleflight result | **Shared per account** — semua request untuk account yang sedang refresh menunggu operasi yang sama dan menerima credential/error result yang sama; refresh duplikat tidak dijalankan. |
| 350 | Persisted refresh state | **Minimal operational state** — simpan last success/attempt, sanitized error category, next retry/cooldown, re-auth-required flag, serta provider/account IDs. Detail transient tetap di memory/log. |
| 351 | Proactive refresh failure routing | **Tetap aktif sampai expiry** — jika refresh proactive gagal sementara tetapi access token lama masih valid, account tetap dapat routing dan refresh diulang dengan backoff. Setelah expiry, request wait/fallback policy berlaku. |
| 352 | Refresh persistence transaction | **Atomic domain transaction** — token, expiry, provider metadata, dan refresh status diperbarui bersama; memory cache hanya diperbarui setelah transaction commit berhasil. |
| 353 | Refresh dashboard surface | **Status original + minimal detail** — tampilkan active/cooldown/re-auth, token expiry, last refresh, next retry, dan sanitized error, serta aksi refresh/re-auth sesuai upstream; tidak membuat full attempt timeline baru. |
| 354 | Admin API versioning | **Path version `/api/admin/v1`** — dashboard, CLI, scripts, dan PAT clients memakai contract versioned eksplisit. Compatibility endpoint original tetap tersedia melalui adapter terpisah. |
| 355 | Shared application layer | **Satu application layer** — HTTP/Admin API, dashboard workflows, dan CLI commands memanggil use-case/domain services yang sama sehingga validation, authz, transactions, dan behavior konsisten. |
| 356 | Management API compatibility | **Thin compatibility adapters** — path/request/response management original dipetakan ke application use-case baru tanpa menduplikasi business logic dan dilindungi parity contract tests. |
| 357 | Application actor context | **Unified actor context** — setiap use-case menerima actor dashboard session, PAT, local CLI, atau internal job beserta origin dan capability context agar authz dan audit konsisten. |
| 358 | Dashboard API usage | **Langsung memakai Admin API v1** — React dashboard memakai `/api/admin/v1`; cookie session diverifikasi pada HTTP boundary lalu diterjemahkan menjadi actor context. Tidak membuat business logic BFF terpisah. |
| 359 | Dashboard realtime transport | **SSE per domain** — Usage, Console, provider state, dan status background jobs memakai SSE server-to-browser sesuai pola original; mutation tetap HTTP dan WebSocket hanya jika feature benar-benar membutuhkan komunikasi dua arah. |
| 360 | Frontend module boundary | **Feature/domain modules** — Endpoint, Providers, Combos, Usage, Quota, Token Saver, CLI Tools, Media, Proxy Pools, Skills, Console, Remote, dan Settings memiliki pages, API hooks, components, serta state terkait di module domain masing-masing. |
| 361 | Frontend state management | **Query cache + local UI state** — server state memakai query/cache layer dengan invalidation jelas; form, filter, drawer, theme, dan state visual tetap lokal. Tidak memakai satu global store besar untuk seluruh aplikasi. |
| 362 | Frontend API contracts | **Generated from API schema** — OpenAPI/contract source menghasilkan TypeScript types dan client tipis; Go handlers serta frontend diuji terhadap schema yang sama. |
| 363 | CLI execution boundary | **In-process local use-cases** — command lokal yang memiliki akses runtime/config memanggil application layer langsung; remote automation memakai Admin API + PAT. CLI tidak mengakses database melewati use-case. |
| 364 | Host integration boundary | **Dedicated adapter per feature** — tray, service manager, updater, tunnels, MITM, Headroom, Pxpipe, dan MCP memiliki interface/use-case serta adapter platform/provider-specific terpisah dengan permission checks dan parity tests. |
| 365 | Platform implementation boundary | **Shared contract + build-tag adapters** — behavior umum memakai interface bersama; implementasi Windows/macOS/Linux menggunakan build-tag/files yang terpisah. Docker capabilities dinyatakan eksplisit dan feature incompatible dinonaktifkan. |
| 366 | Background scheduler boundary | **Central scheduler + domain jobs** — satu scheduler mengelola lifecycle, trigger, timeout, retry/backoff, run-now, locking, dan status; business logic job tetap berada pada domain provider, usage, backup, update, dan maintenance terkait. |
| 367 | Background job overlap | **Per-job local lock** — timer dan `Run now` tidak boleh menjalankan job sama secara bersamaan. Trigger kedua bergabung, ditolak, atau dijadwalkan setelah run aktif sesuai contract job. |
| 368 | Background job history | **Current + short history** — persist current/last status dan histori ringkas yang diperlukan dashboard/workflow original; detail mentah tetap di application logs dan tidak membuat permanent job-event store besar. |
| 369 | Repository structure | **Pertahankan struktur berlapis** — target tree tetap memakai boundary `transport → app → domain/engine → persistence/host`. Boundary yang tegas lebih penting daripada meratakan semua file per fitur. |
| 370 | Application and domain separation | **`internal/app` dan `internal/domain` tetap terpisah** — domain menyimpan aturan serta invariant murni, sedangkan application layer memiliki use-case, koordinasi, actor context, dan transaction boundary. |
| 371 | Model API contract artifacts | **Schema formal + fixtures** — Admin API dan seluruh model-compatible API utama memiliki contract/schema versioned serta golden request/response/stream fixtures. Schema formal tidak boleh memaksa protocol native/binary menjadi representasi lossy; fixture parity tetap menjadi bukti observable behavior. |
| 372 | Upstream-to-Go mapping granularity | **Per modul/workflow** — setiap request flow, provider family, OAuth flow, dashboard workflow, CLI/host workflow, dan domain original dipetakan ke target Go module. File serta symbol upstream tetap dicantumkan sebagai source evidence dan parity-test anchor, tetapi ledger utama tidak diwajibkan menjadi satu baris per file/symbol. |
| 373 | Preserve/change/remove ledger format | **Tabel per domain** — ledger dipisahkan menjadi domain HTTP contracts, providers, OAuth, routing/combos, streaming/translators, persistence, dashboard, CLI/host integration, jobs, security, packaging, dan tests. Sesuai keputusan #256, seluruh tabel domain tetap disetujui sebagai satu paket final sebelum coding. |
| 374 | Internal porting order | **Foundation lalu vertical slice** — urutan resmi: (A) bootstrap/config/PostgreSQL/migrations/auth/trusted-proxy/contracts/test harness; (B) satu model-request vertical slice end-to-end yang mencakup SSE, cancellation, terminal event, fallback, satu generic compatible provider, dan satu specialized executor berisiko tinggi; (C) seluruh format/translator/streaming/provider/routing/combo matrix; (D) Admin API, dashboard, dan CLI per domain; (E) host integrations, packaging, full provider live matrix, performance, security, dan soak gates. Urutan ini hanya implementation ordering dan tidak mengurangi super-full parity target release pertama. |
| 375 | Dashboard OIDC | **Pertahankan OIDC original** — local password tetap tersedia sebagai default, dan OIDC dashboard original juga dipertahankan sebagai opsi. OIDC harus memakai session/authz/audit application boundary yang sama dan mendapat parity/security tests. Keputusan ini memperluas keputusan #47 yang sebelumnya menyebut local password saja. |
| 376 | Custom provider URL safety | **Tetap tanpa SSRF/DNS-rebinding guard** — custom provider URL boleh mengakses loopback, LAN, internal, dan link-local; tidak ada DNS-rebinding check. Ini merupakan keputusan sadar berisiko tinggi dan wajib didokumentasikan. Security gate tetap menguji trusted-proxy/authz/redaction, tetapi tidak boleh diam-diam memblokir target custom-provider tersebut. |
| 377 | Localization parity | **Pertahankan semua locale original** — English dan Indonesian tetap locale utama serta English fallback, tetapi seluruh locale original 9Router juga menjadi target parity release pertama. Keputusan ini menggantikan pembatasan scope keputusan #56. |
| 378 | Manual configuration export/import | **Pertahankan kontrak original** — port export/import configuration original, termasuk payload yang partial, tabel/domain yang disertakan atau dikecualikan, replacement/destructive semantics, transactionality, validation, dan konfirmasi risiko. Ini membatalkan keputusan #200 yang sebelumnya menghapus config transfer. Backup/restore DR tetap kontrak terpisah. |
| 379 | Cloud/sync | **Tetap dihapus** — gorouter adalah local/self-hosted product tanpa cloud/sync original. Ini merupakan pengecualian source parity yang disengaja dan keputusan #156 tetap final. |
| 380 | Final parity-ledger approval | **`PARITY-LEDGER.md` disetujui sebagai satu paket final** — seluruh disposition Preserve/Change/Remove, pengecualian parity, decision references, observable contracts, risiko, dan acceptance evidence menjadi scope authority untuk implementation planning. |
| 381 | Final architecture approval | **`ARCHITECTURE.md` disetujui secara keseluruhan** — modular monolith berlapis, `internal/app` dan `internal/domain` terpisah, PostgreSQL, satu proses multi-goroutine, repository tree, dependency rules, data flows, formal contract artifacts, dan urutan gate #374 menjadi architecture authority. Approval ini mengizinkan implementation planning, bukan coding tanpa plan yang ditinjau. |
