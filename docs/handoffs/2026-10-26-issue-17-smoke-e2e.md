---
judul: Handoff — sisa #17: satu smoke E2E perjalanan penuh
tanggal: 2026-10-26
posisi_branch: feat/issue-17-hardening @ 5072ea2 (main: 9c96cb0)
untuk: sesi baru di mesin yang punya Docker
---

# Handoff — sisa #17: smoke E2E perjalanan penuh

> **Catatan kerja, bukan dokumen produk.** Hapus file ini setelah #17 merge. Isi
> yang layak hidup lama sudah ada di ADR 0018 dan `backend/README.md`.

## Posisi sekarang

Branch `feat/issue-17-hardening`, tujuh commit di atas `main` (`9c96cb0`):

| Commit | Isi |
| --- | --- |
| `047c440` | ADR 0018 — rekonsiliasi, listener operator worker, smoke tanpa modem |
| `ab7232b` | Rekonsiliasi `pending` lewat `Gateway.GetStatus` |
| `fd8825a` | Guard graph worker + migrate (`fx.ValidateApp`) |
| `a85e809` | Event `transaction.state_changed`/`provisioning.attempt` + `provision_attempts` |
| `e73c7dc` | Listener operator worker: `/healthz`, `/metrics`, heartbeat `sweeps`, compose healthcheck |
| `ec8bfd5` | Tabel isolasi Phone Number untuk setiap endpoint bersesi |
| `5072ea2` | Pemindai istilah UI + layout 360px (Playwright, API diintersepsi) |

Hijau di mesin pengembang: backend `make test` 297 tes, `make lint` 0 issues;
frontend `typecheck`/`lint` 0 issues, 93 tes, `build` sukses; root 60 tes Login
Page + 25 smoke + 6 layar 360px.

**Yang belum ada sama sekali: satu smoke E2E yang menempuh perjalanan penuh
terhadap stack sungguhan.** Itu satu-satunya sisa acceptance #17, dan sengaja
tidak dikerjakan di sesi sebelumnya karena **Docker tidak tersedia** di mesin
itu. Aturan yang berlaku: jangan menulis spec E2E lalu mengklaim hijau tanpa
pernah menjalankannya — buktinya harus dari run nyata.

Keputusan yang mengikat: **ADR 0018 §5** (opsi c: stack tanpa modem, vendor
distub lewat `TRIPAY_BASE_URL`).

## Isi pekerjaan

1. **Stub Tripay** — satu service kecil tanpa dependensi (cukup `node:22-alpine`
   + `node:http`/`node:crypto`, atau Go stdlib) dengan dua endpoint yang benar-
   benar dipanggil adapter, plus satu halaman bayar palsu:
   - `POST /transaction/create` → `{"success":true,"data":{...}}` (form-urlencoded
     masuk; yang dibaca adapter: `reference`, `pay_code`/`pay_url`/`qr_*`,
     `instructions`, `expires_at`).
   - `GET /transaction/detail?reference=…` → status domain (`paid`/`unpaid`).
   - `GET /bayar?reference=…` → halaman stub yang, saat diklik, mem-**POST**
     callback bertanda tangan ke `${TRIPAY_CALLBACK_URL}`.
2. **`docker-compose.e2e.yml`** (override, jangan ubah `docker-compose.yml`):
   - service stub + `TRIPAY_BASE_URL: http://tripay-stub:8080`,
     `TRIPAY_CALLBACK_URL: http://api:8080/api/payments/callback`,
     `TRIPAY_PRIVATE_KEY` dipakai bersama backend dan stub (stub yang menandatangani).
   - `SMS_DEVICE` diarahkan ke device yang **tidak ada**, sehingga drainer gagal
     dan baris `sms_outbox` tetap menyimpan `message` (hanya baris `sent` yang
     dikosongkan — `notification/repository` `MarkSent`).
3. **Spec Playwright** (`tests/e2e/` + config sendiri, mis.
   `playwright.e2e.config.mjs`), perjalanan: Login Page → pilih Package → isi
   Phone Number → baca Magic Link dari Postgres → tukar → pilih kanal → bayar di
   halaman stub → tunggu PIN di Account Status → kembali ke Login Page.
   Di setiap layar app, tegaskan juga `scrollWidth <= clientWidth` (sudah ada
   padanannya di `tests/app/layout.spec.mjs`).
4. **Job CI** `E2E (compose)` terpisah: `docker compose -f docker-compose.yml -f
   docker-compose.e2e.yml up -d --build`, tunggu `GET /api/health` 200 **dan**
   `127.0.0.1:8081/healthz` worker, jalankan spec, `down -v` di `if: always()`.

## Fakta yang sudah diverifikasi (jangan diulang risetnya)

- **Adapter hanya tahu dua path**: `POST /transaction/create` dan
  `GET /transaction/detail?reference=` (`backend/internal/application/payment/tripay/tripay.go`).
- **Tanda tangan callback**: header `X-Callback-Signature` = hex HMAC-SHA256
  **body mentah** dengan `TRIPAY_PRIVATE_KEY`, dibandingkan constant-time. Body
  wajib memuat `merchant_ref` (= `transactions.reference` sistem ini).
- **Magic Link tidak bisa dibaca dari `sessions.token_hash`** (SHA-256, satu arah)
  maupun dari `magic_link_tokens`. Satu-satunya sumber body SMS adalah baris
  `sms_outbox` yang belum `sent`.
  Baca dengan: `docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER"
  -d "$POSTGRES_DATABASE" -tAc "select message from sms_outbox where sent_at is
  null order by id desc limit 1"'`, lalu ambil URL-nya.
- **Login Page membaca katalog dari backend** bila `packagesFromBackend: 1` dan
  `backendBaseUrl` diisi (`KOPINETGO-2024/assets/voucher-client.js`); CTA-nya
  menyusun `${appBaseUrl}?package=…&return=…`. Aset KOPINETGO **dibekukan** —
  konfigurasikan lewat `page.route` pada `**/config/data/voucher.js` atau lewat
  server statis tes, jangan mengedit `KOPINETGO-2024/`.
- Read publik Login Page mengirim `Access-Control-Allow-Origin: *` (ADR 0014),
  jadi `backendBaseUrl: http://localhost:8080` boleh lintas origin.
- **Compose butuh variabel wajib** (`:?` di `docker-compose.yml`):
  `POSTGRES_USER/PASSWORD/DATABASE`, `FREERADIUS_MYSQL_USER/PASSWORD/DATABASE`,
  `REDIS_PASSWORD`, `PIN_ENCRYPTION_KEYS`. CI harus menyiapkan `.env` lebih dulu
  (`.env.example` sudah berisi nilai contoh, termasuk kunci PIN pengembangan).
- Satu origin: app di `http://localhost:8080/`, API di `/api` (proxy nginx).
  Worker hanya loopback `127.0.0.1:8081` (`/healthz`, `/metrics`).
- Rekonsiliasi berjalan tiap 1 menit; kalau callback stub gagal, transaksi tetap
  sampai `paid` lewat `transaction:reconcile` — jangan mengandalkannya, tapi
  ketahuilah bahwa satu kegagalan bukan jalan buntu.

## Jebakan yang sudah pernah menggigit

- **Jangan menambah "fake mode" ke kode produksi.** Stub cukup lewat
  `TRIPAY_BASE_URL`; `deployment_test.go` mengunci empat tempat config.
- **Rate limit Magic Link** memakai Redis bersama tanpa namespace tes: pakai
  Phone Number dan `RemoteAddr` unik per tes.
- **PIN**: tidak pernah di log, tidak plaintext di Postgres, dan body SMS
  dikosongkan saat baris outbox final — jangan mengubah `MarkSent` untuk E2E.
- **Jangan seed sesi langsung** (opsi b di ADR 0018): itu melewati langkah yang
  paling mungkin rusak, yaitu Magic Link.
- `/healthz` worker gagal bila **belum ada** sweep yang selesai; compose memakai
  `start_period: 60s`. Tunggu dengan sabar, jangan menyimpulkan worker rusak.
- `make up` menjalankan `migrate` sekali di awal; stack E2E sebaiknya juga
  (`service_completed_successfully`) agar katalog tidak kosong.

## Setelah 5b

1. `/code-review` dua sumbu atas `main...HEAD` (Standards + Spec), perbaiki
   temuan, lalu push.
2. PR dengan catatan: 5a sudah diverifikasi lokal, 5b diverifikasi lewat job CI.
   Merge menutup **#17** dan PRD induk **#10**.
3. Berikutnya **Admin Panel** — belum ada PRD-nya, jadi mulai dari `/to-prd`,
   bukan dari kode.

## Perintah verifikasi

```bash
# backend
cd backend && make deps build lint format && make test

# frontend + Login Page seam
cd frontend && npm run typecheck && npm run lint && npm test && npm run build
cd .. && npm test && npm run test:smoke && npm run test:app

# 5b (butuh Docker)
docker compose -f docker-compose.yml -f docker-compose.e2e.yml up -d --build
npx playwright test -c playwright.e2e.config.mjs
docker compose -f docker-compose.yml -f docker-compose.e2e.yml down -v
```
