---
judul: Handoff — Issue #17 Pengerasan & smoke end-to-end
tanggal: 2026-10-24
posisi_main: f166dde
untuk: sesi baru yang mengerjakan #17
---

# Handoff — #17 Pengerasan & smoke end-to-end

> **Catatan kerja, bukan dokumen produk.** Hapus file ini setelah #17 merge; isi
> yang layak hidup lama harus menjadi ADR atau PRD.

## Posisi sekarang

`main` = `f166dde`, CI hijau untuk backend, app, dan Login Page. Tertutup: #11,
#12, #13, #14, #15, **#16 (Account Status & Extend, PR #22)**. Terbuka: **#17**
(slice ini) dan PRD induk **#10** — #17 adalah slice **terakhir**; kalau merge,
PRD #10 ditutup dan tidak ada pekerjaan lain di luar Admin Panel.

ADR yang mengikat: 0001 (penulis FreeRADIUS), 0004 (Phone Number + PIN), 0008
(Tripay di balik port), 0009/0011 (cookie opaque + Magic Link), 0010 (outbox SMS
+ satu worker pemegang modem), 0014 (baca Active Period lewat port), 0015
(penjaga transisi = UPDATE berguard), 0016 (PIN terenkripsi & idempoten), 0017
(permukaan baca Account Status). **Nomor berikutnya: 0018.**

Branch yang disarankan: `feat/issue-17-hardening`.

## Cara menjalankan

```bash
cd backend
make deps build lint format            # lint harus 0 issues
make test                              # unit + integrasi (Postgres/Redis nyata)
make test-integration                  # hanya test/integration
make run-api / run-worker / run-migrate
```

Integrasi butuh `redis-server` di `PATH`; Postgres di-embed. **`TEST_FREERADIUS_MYSQL_DSN`
opsional di lokal** (tes MySQL `t.Skip`), tetapi `test/integration/main_test.go`
**gagal keras** kalau `CI=true` tanpa DSN — jangan mengandalkan skip.

```bash
cd frontend
npm run typecheck && npm run lint && npm test && npm run build
```

Root `npm test` + `npm run test:smoke` memuat Login Page (`tests/unit`,
`tests/smoke`, `playwright.config.mjs`) dan **tidak boleh berubah perilakunya**.
Stack penuh: `make up` (compose), `make logs`, `make ps`.

## Peta domain dan seam yang sudah ada

| Butuh | Pakai ini | Jangan |
| --- | --- | --- |
| Pekerjaan berkala | `transaction/worker/sweeper.go` — `newSweeper(every, task, logger)` + `attach(lc)` + `wakeUp()`; contoh: `Expiry` (`transaction:expire`) dan `Provisioning` (`service.TaskProvision`) | menambah scheduler Asynq periodik baru atau ticker sendiri; pola ini sudah menangani start/stop/wake-up |
| Status pembayaran di gateway | `payment.Gateway.GetStatus(ctx, reference)` → `payment.PaymentStatus{Reference, Status, Amount, PaidAt}`; status domain `payment.Status{Unpaid,Paid,Expired,Failed}` | menyentuh tipe/kode Tripay; pemetaan vendor → domain hanya di adapter |
| Jalur transisi state | `service.settle` / `service.close` di `transaction/service/callback.go`; guard `repo.Transition(ctx, id, from, to, at)`; `entity.State.CanTransitionTo` | `Updates(map[string]any{"state": ...})` langsung; membuat jalur `paid` kedua |
| "Bayar setelah kedaluwarsa" | sudah ditangani `settle`: state yang tak bisa ke `paid` → log `Warn` + `return nil` (butuh operator) | memaksa transisi agar pembeli tidak komplain |
| Retry penyediaan | `service.Provisioner.ProvisionDue` + `repo.PaidIDs(limit)` lewat sweeper yang sudah ada | menduplikasi penyediaan di rekonsiliasi |
| Waktu | `clock.Clock` disuntik (`FakeClock` di tes) | `time.Now()` di service/repository |
| Tes integrasi | `testutil.NewAPIHarness(t)`; `Doubles.Gateway` (`Statuses map[string]payment.PaymentStatus`, `StatusErr`, `CreateErr`, `CallbackErr`), `Doubles.SMS`, `Doubles.Clock`; `harness.TransactionExpiry`, `harness.TransactionProvisioning`, `harness.TransactionService` | menulis fake gateway baru — yang ada sudah punya `GetStatus` |
| Health dependency | `health/service.CheckersGroup` + `GET /api/health`; DTO `health/dto.Health{status, service, version, checked_at, checks}`; fixture `contracts/health.response.json` + `health.degraded.response.json` | mengubah bentuk `/api/health` tanpa memperbarui fixture (dua sisi merah) |
| Route HTTP baru | `RouteRegistrar` di `internal/server/api/module.go`, group `api_routes` | mendaftarkan route di `NewEngine` |
| Task worker baru | `TaskRegistrar` di `internal/server/worker` (`RegisterHandlers(mux)`), group `worker_tasks` | switch terpusat di `NewServeMux` |
| Health worker | **belum ada**; compose hanya healthcheck `api` (dan `proxy`), `worker` tidak punya | menganggap worker sehat karena container `running` |
| Stub vendor untuk E2E | `tripay.base_url` sudah configurable (`TRIPAY_BASE_URL`) → cukup service stub di compose + override env | menambah "fake mode" di kode produksi |
| Magic Link di E2E | lihat Keputusan 5: baris `sms_outbox` yang **gagal** kirim tetap menyimpan `message`; yang `sent` dikosongkan | membaca token dari hash (`sessions.token_hash` = SHA-256, tak bisa dibalik) |
| Metrik | **belum ada dependensi metrik apa pun** di `backend/go.mod` | menambah Prometheus tanpa target scrape |

Tabel yang ada: `packages`, `transactions`, `sessions`, `magic_link_tokens`,
`sms_outbox`. **Jangan menambah tabel `account`** (PRD 6/7). Konfigurasi baru
wajib di **4 tempat**: `internal/config/config.go` (default + validasi),
`backend/config.sample.yaml`, `.env.example`, `docker-compose.yml` — kalau salah
satu terlewat, `test/integration/deployment_test.go` merah.

## Keputusan yang harus diambil di ADR 0018 (tulis commit pertama)

### 1. Jendela rekonsiliasi

Sapu **`pending` saja** (bukan semua state) dalam batch kecil (config, usul
`transaction.reconcile_batch` default 50), pakai `Gateway.GetStatus`, lalu masuk
lewat `settle` — jalur yang sama dengan callback, sehingga penyediaan tidak
mungkin terjadi dua kali.

Penting: `expired` adalah state akhir (ADR 0015), jadi settlement yang datang
setelah deadline **hilang** — `settle` sekarang hanya me-log `Warn`. Karena itu
jendela rekonsiliasi harus **di dalam** `transaction.payment_ttl`, bukan
sesudahnya. Tulis konsekuensinya sebagai risiko yang diterima + jalur manual
operator; jangan diam-diam "memperbaiki" dengan transisi mundur.

Batasi juga: satu request gateway per Transaction per sweep, `StatusErr` → tidak
mengubah state, dan jangan memanggil gateway untuk `paid` (itu tugas sweeper
penyediaan, sudah ada).

### 2. Bentuk metrik

Usul: `GET /api/metrics` (JSON) → `outbox{pending,sent,failed}` +
`provisioning{paid_awaiting,attempts}`. Tanpa dependensi baru, tanpa infra scrape
yang belum ada, dan bisa berpindah ke Prometheus nanti di belakang bentuk yang
sama. Kalau menolak endpoint publik, alternatifnya menaruh angka itu di
`/api/health` — tetapi itu berarti memperbarui fixture health yang sudah
dibekukan dua sisi. Pilih satu, jangan dua-duanya.

### 3. Definisi "worker sehat"

Worker adalah satu-satunya proses yang memegang modem dan FreeRADIUS (ADR 0010),
tetapi tidak punya port HTTP. Usul: `WORKER_HEALTH_ADDR` (mis. `:8081`) →
`/healthz` yang mengecek Redis + menulis timestamp sweep terakhir, lalu
healthcheck compose untuk service `worker`. Yang tidak boleh: healthcheck yang
hanya `redis-cli ping` dari sisi host (itu menguji Redis, bukan worker).

### 4. Urutan log terstruktur

Setiap perubahan state: `transaction.state_changed` (`transaction_id`, `from`,
`to`, `reference`, `phone` lewat `phone.Mask`, `attempt`). Setiap percobaan
penyediaan: `provisioning.attempt` (hasil, `transaction_id`, `attempt`). Jangan
log PIN, jangan log Phone Number mentah, jangan log body callback (attacker
controlled — sudah jadi konvensi di `callback.go`).

### 5. Bagaimana smoke E2E melewati Magic Link

Tiga pilihan, putuskan di ADR:

- **(a) Modem palsu** — pty + emulator AT di compose, ambil body SMS sebelum
  kirim. Paling setia, paling mahal (AT/PDU, timing), dan tidak ada adapter modem
  yang bisa dipilih lewat config saat ini.
- **(b) Seed sesi langsung di Postgres** — `sessions(token_hash = sha256(token),
  phone_number, expires_at)` lalu set cookie di Playwright. Murah, tetapi
  melewati langkah Magic Link.
- **(c) Rekomendasi: stack smoke tanpa modem.** Arahkan `SMS_DEVICE` ke device
  yang tidak ada; drainer gagal, dan baris `sms_outbox` **tetap menyimpan
  `message`** sampai `sent` (terbukti di `outbox.service_test.go`: baris final
  mengosongkan body, baris yang akan retry menyimpannya). Tes membaca tautan
  Magic Link dari Postgres, menekannya, lalu melanjutkan bayar → PIN.

  (c) tidak menambah kode produksi, sekaligus membuktikan cerita PRD 30/31
  (modem mati tidak menghilangkan hak akses) dan mengisi metrik outbox dengan
  data nyata. Batasnya: kalau reviewer menuntut langkah SMS literal, (a) harus
  dianggarkan sebagai pekerjaan terpisah, bukan diselipkan.

## Isi #17 (dari issue)

1. Rekonsiliasi berkala: Transaction yang dibayar di gateway tanpa callback
   mencapai `paid` → `provisioned` lewat jalur transisi yang sama, tanpa
   penyediaan ganda.
2. Uji isolasi data antar-Phone Number di seluruh endpoint pengguna + daftar izin
   `return`.
3. Observability: log perubahan state & percobaan penyediaan, metrik outbox dan
   penyediaan, health check API + worker terdokumentasi.
4. Satu smoke Playwright perjalanan penuh: Login Page → Package → Magic Link →
   bayar → PIN di Account Status, hijau di CI.
5. Pass pengalaman: semua teks Indonesia dengan istilah CONTEXT.md, tiap layar
   nyaman di 360px tanpa scroll horizontal.

## Jebakan yang sudah pernah menggigit

- **Rate limit Redis tanpa namespace tes**: setiap tes yang menyentuh batas laju
  wajib memakai Phone Number **dan** `RemoteAddr` unik (catatannya sudah ada di
  `backend/README.md` § Test harness).
- **Kunci config baru wajib di 4 tempat** (lihat di atas) atau
  `deployment_test.go` merah.
- **Fixture kontrak diuji dua sisi**: Go `DisallowUnknownFields` + zod
  `.strict()`, dan `contracts/README.md` harus memuat fixture baru. Fixture yang
  tumbuh field tak dimodelkan gagal di kedua sisi.
- **PIN**: tidak di log (`phone.Mask`), tidak plaintext di Postgres, tidak di
  payload daftar; body SMS dikosongkan saat baris outbox final.
- **Clock**: `FakeClock` untuk `expiring`/`expired`/deadline; jangan
  `time.Sleep` di tes.
- **`GET /packages` dan `GET /active-period` publik tidak boleh berubah bentuk**
  (kontrak Login Page, dibekukan `tests/unit/login-page-contract.test.js`).
- **Frontend**: layering `route → component → query/state → api → klien axios`;
  jangan menaruh PIN di `$state` global; domain baru butuh `index.ts`.
- **`return` allowlist sudah diuji dua tempat** (Magic Link dan checkout) —
  jangan menduplikasi tesnya, cukup merujuk nama tesnya sebagai bukti.
- **Jangan menambah `GATEWAY` mode fake**: stub vendor cukup lewat
  `TRIPAY_BASE_URL` menunjuk service compose.

## Urutan commit yang disarankan

1. `docs(adr): ADR 0018` — jendela rekonsiliasi, bentuk metrik, definisi health
   worker, konvensi log, dan cara smoke melewati Magic Link. Perbarui
   `docs/adr/README.md`.
2. `feat(transaction): rekonsiliasi pending lewat Gateway.GetStatus` — sweeper
   ketiga + `transaction.reconcile_*` di 4 tempat config. Tes: callback hilang →
   `paid` → `provisioned` **tepat sekali** (hitung `radreply`/expiration), status
   `unpaid` tidak mengubah apa pun, `StatusErr` tidak mengubah apa pun,
   `expired` tetap `expired` + log operator.
3. `feat(observability): log state + /api/metrics + worker /healthz` — plus
   healthcheck compose `worker`, dokumentasi `backend/README.md`, dan fixture
   kontrak kalau `/api/metrics` masuk `contracts/`.
4. `test(security): tabel uji isolasi lintas endpoint` — satu tes tabel
   `{method, path, body}` untuk **setiap** endpoint bersesi, membuktikan Phone
   Number asing di parameter/body dijawab 404. Sebagian sudah ada
   (`account_test.go`, `transaction_test.go`); commit ini menyatukannya supaya
   penambahan endpoint berikutnya tidak lolos tanpa uji. Daftar izin `return`
   **sudah** dibuktikan `auth_test.go:314` dan `transaction_test.go:374` — jangan
   menulis ulang, cukup merujuknya di PR.
5. `feat(app): pass 360px + audit istilah` — jadikan kriteria ini **bisa
   dieksekusi**: satu tes Vitest yang memindai `frontend/src/**/*.svelte`
   terhadap istilah terlarang (Voucher, Order, Renew, top-up, Dashboard,
   Backoffice), plus verifikasi 360px tanpa scroll horizontal.
6. `test(e2e): smoke Playwright perjalanan penuh` — `fake-tripay` + stack tanpa
   modem di compose, config Playwright terpisah dari `playwright.config.mjs`
   (jangan mencampur dengan smoke Login Page), job CI baru.

## Definition of Done (#17)

Ikuti §11 skill backend / §12 skill frontend, plus:

- [ ] `make build && make test && make lint && make format` hijau; app
      `typecheck/lint/test/build` hijau; root `npm test` + `npm run test:smoke`
      tidak berubah.
- [ ] Transaction yang dibayar di gateway tanpa callback mencapai `paid` lalu
      `provisioned` lewat rekonsiliasi; expiration/PIN tidak bertambah dua kali.
- [ ] Setiap endpoint pengguna punya uji yang membuktikan data Phone Number lain
      tidak terbaca/tertulis (404, bukan 403).
- [ ] `return` di luar allowlist diabaikan, dengan uji yang menegaskannya.
- [ ] Log memuat perubahan state + percobaan penyediaan; metrik outbox
      (tertunda/terkirim/gagal) tersedia; kesehatan API **dan worker** terdokumentasi
      dan ditegakkan healthcheck.
- [ ] Satu smoke Playwright menempuh perjalanan penuh terhadap backend sungguhan
      dengan vendor dipalsukan, hijau di CI.
- [ ] Tidak ada istilah terlarang di UI; semua teks Indonesia; setiap layar
      terverifikasi pada 360px.
- [ ] `/code-review` dua sumbu atas diff sebelum PR; temuan diperbaiki atau
      dicatat sebagai judgement call.

## Setelah merge

Tutup issue #17 → tutup PRD #10 → hapus file ini → pastikan `README.md` menyebut
status akhir (termasuk: tidak ada lagi slice terjadwal di luar Admin Panel).
