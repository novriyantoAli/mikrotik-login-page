---
judul: Handoff — Issue #14 Transaction, kanal & pembayaran Tripay
tanggal: 2026-10-23
posisi_main: 83ba648
untuk: sesi baru yang mengerjakan #14
---

# Handoff — #14 Transaction, kanal & pembayaran Tripay

> **Catatan kerja, bukan dokumen produk.** Hapus file ini setelah #14 merge; isi
> yang layak hidup lama sudah menjadi ADR atau PRD.

## Posisi sekarang

`main` = `83ba648`, CI hijau. Selesai dan tertutup: #11 (prefactor monorepo),
#12 (katalog Package + Active Period publik), #13 (Magic Link, sesi, outbox SMS +
worker modem). Terbuka: **#14** (slice ini), lalu #15 (FreeRADIUS/PIN/Extend),
#16 (Account Status), #17 (pengerasan + smoke E2E). PRD induk: #10 —
`docs/prd/transaction-purchase-app-backend.md`.

ADR yang mengikat: 0001 (FreeRADIUS writer), 0002 (MySQL terpisah), 0004 (Phone
Number + PIN), 0005/0014 (baca Active Period lewat port), 0008 (Tripay di balik
port), 0009/0011 (cookie opaque + Magic Link/sesi), 0010 (outbox SMS + serializer
modem), 0012 (seed katalog), 0013 (SPA satu origin). **Nomor berikutnya: 0015.**

## Cara menjalankan

```bash
cd backend
make deps build lint format            # lint harus 0 issues
make test                              # unit + integrasi; Postgres/Redis nyala otomatis
make test-integration                  # hanya test/integration (embedded Postgres + redis-server)
make swagger-gen                       # output ke ../docs/api (git-ignored)
make run-api / run-worker / run-migrate
```

Tes integrasi butuh `redis-server` di `PATH` (ada di mesin ini) atau
`TEST_REDIS_ADDR`; Postgres di-embed dan di-cache di `~/.cache`.
`TEST_FREERADIUS_MYSQL_DSN` opsional — tanpa itu jalur "DB down" yang diuji.

```bash
cd frontend
npm run typecheck && npm run lint && npm test && npm run build
```

Root (`npm test`) memuat tes Login Page (unit + kontrak) dan tidak boleh berubah.

## Peta domain dan seam yang sudah ada

| Butuh | Pakai ini | Jangan |
| --- | --- | --- |
| Identitas pembeli | `auth.Service.Authenticate(ctx, cookieValue) (*dto.SessionResponse, error)`; cookie dibaca dengan `httpx.ReadSessionCookie(c, name)` | membaca cookie/`sessions` sendiri |
| Kirim SMS (PIN, nanti) | `notification/service.Outbox` (`Enqueue(ctx, Message)`) | menyentuh modem atau `sms_outbox` |
| Tulis akun hotspot | `provisioning.Writer` (`Apply(ctx, Account)`) | query `radcheck`/`radreply` di service |
| Baca expiry | `provisioning.ExpirationReader` | query MySQL langsung |
| Gateway | `payment.Gateway`: `CreateTransaction`, `GetStatus`, `VerifyCallback(raw, signature)`; `payment.Status` adalah kosakata kita | tipe/status Tripay bocor ke service atau DTO |
| Katalog | `packages.Service.ListPackages` — **hanya DTO tampilan** (lihat prasyarat 1) | mem-parse `"Rp 5.000"` / `"1 Hari"` |
| Waktu | `clock.Clock` disuntik | `time.Now()` di service/repository |
| Uang | `int64` rupiah | float |

Wiring: satu domain = `internal/application/<domain>/{dto,entity,repository,service,handler,module.go}`,
`Module()` untuk API, `WorkerModule()` untuk worker, `Migrations()` untuk tabel.
Route didaftarkan sebagai `RouteRegistrar` di `internal/server/api/module.go`; task
worker sebagai `TaskRegistrar` di `internal/server/worker` (lihat contoh drainer di
`internal/app/app.go` `WorkerWithBase`). Tabel baru wajib masuk `migrations()` di
`internal/server/migration/module.go`.

## Prasyarat #14 (kerjakan sebagai commit pertama, kecil dan terpisah)

**1. Bacaan Package bertipe.** `packages.Service` hanya mengembalikan
`dto.PackageResponse` (string tampilan). Checkout butuh `PriceIDR`, dan #15 butuh
`DurationMinutes` + `RateLimit`.
- Tambah `repository.GetByID(ctx, id) (*entity.Package, error)` (GORM, `gorm.ErrRecordNotFound`
  → sentinel `ErrPackageNotFound`).
- Tambah `Service.GetPackage(ctx, id) (*entity.Package, error)`; service boleh
  mengembalikan entity ke domain lain (bukan ke HTTP).
- Tes: repository (integrasi) + service (unit, pakai fake repo).

**2. `middleware.RequireSession`.** Sekarang pembacaan cookie hanya ada di handler
auth; #14, #15, #16 semuanya butuh. Ekspektasi (PRD 13):
- ambil cookie → `auth.Service.Authenticate`; gagal ⇒ `401 unauthorized`;
- simpan identitas di `gin.Context` (`c.Set("session", identity)`), sediakan
  helper `middleware.SessionFrom(c)` untuk handler;
- Phone Number di URL/body yang tidak sama dengan sesi dijawab **404**, bukan 403;
- jangan pasang CORS di route bersesi (aturan ADR 0014: hanya dua read publik).
- Tes: handler-level dengan fake authenticator, plus satu tes integrasi.

## Isi #14 (dari issue)

1. Daftar kanal tertutup + biaya, **dari port** (tambah metode port bila perlu —
   mis. `Channels(ctx)` — tetap tanpa tipe vendor).
2. `POST` buat Transaction idempoten per sesi+Package: `pending` yang belum
   kedaluwarsa dikembalikan ulang beserta instruksinya.
3. Instruksi dirender di aplikasi (VA/QR, jumlah, biaya terpisah, batas waktu).
4. Polling status dari aplikasi; berhenti di state akhir atau tab tidak terlihat.
5. Callback publik: verifikasi tanda tangan atas **payload mentah**, idempoten
   terhadap pengulangan, satu-satunya jalur `pending → paid`.
6. Pekerjaan latar: `pending` yang lewat batas waktu → `expired`.
7. State eksplisit `pending|paid|provisioned|failed|expired`; transisi terlarang
   ditolak di domain; pemetaan status vendor hanya di adapter.

**Batas slice:** #14 berhenti di `paid`. `paid → provisioned` milik #15 — enum
boleh lengkap, tetapi jangan menaruh pekerjaan penyediaan di slice ini.

## Keputusan yang perlu diambil (tulis ADR 0015 sebelum/selagi implementasi)

- **Penjaga transisi** memakai `UPDATE … WHERE state = 'pending'` (pola yang sama
  dengan `used_at IS NULL` di Magic Link dan `FOR UPDATE SKIP LOCKED` di outbox),
  bukan baca-lalu-tulis. Callback ganda lalu kehilangan balapan secara alami.
- **Idempotensi callback**: kunci yang dipakai (reference gateway? reference kita?)
  dan apa yang terjadi pada callback dengan jumlah tidak cocok.
- **Keatoman outbox**: ADR 0010 sudah dipersempit — pemanggil yang punya transaksi
  sendiri wajib menulis baris outbox di transaksi itu. #15 akan butuh
  `Outbox.EnqueueTx(tx, message)`; putuskan apakah dibangun sekarang atau di #15.

## Jebakan yang sudah pernah menggigit

- **CI**: `concurrency` dengan `cancel-in-progress` membatalkan run lama saat push
  beruntun. "cancelled" bukan merah; tunggu run terakhir.
- **Rate limit Redis tidak ber-namespace**: tes integrasi wajib memakai
  `RemoteAddr` unik per tes (`harness.DoRequest` + `r.RemoteAddr`), karena counter
  per IP hidup di DB Redis bersama.
- **Kunci config baru** wajib muncul di `backend/internal/config/config.go`
  (defaults + validate), `backend/config.sample.yaml`, `.env.example`, **dan**
  `docker-compose.yml`. `test/integration/deployment_test.go` akan gagal kalau
  salah satu terlewat.
- **Fixture kontrak**: setiap payload baru masuk `contracts/` dan diuji dua sisi
  (DTO Go `DisallowUnknownFields` + zod `.strict()`).
- **`docs/api/` git-ignored**; `make swagger-gen` untuk verifikasi anotasi, jangan
  commit hasilnya.
- **Clock**: jangan pakai `time.Now()` di service/repository; tes memakai
  `FakeClock` dan memajukannya.
- **Port SMS ada di `notification/sms`**, bukan root domain, karena `module.go` di
  root akan membuat import cycle. Ikuti pola yang ada, jangan "rapikan".
- **Aplikasi**: komponen tidak boleh menyentuh `axios`; `$app/paths` perlu stub di
  `vitest.config.ts` (lihat `tests/support/app-paths.ts`).

## Urutan commit yang disarankan

1. `feat(packages): baca Package bertipe untuk checkout` (+ tes).
2. `feat(auth): middleware sesi untuk endpoint pengguna` (+ tes).
3. `feat(transaction): state machine, repository, idempoten per sesi` (+ tes domain).
4. `feat(payment): adapter Tripay + verifikasi tanda tangan` (+ fixture callback,
   tanpa jaringan).
5. `feat(transaction): callback publik, expiry worker, polling` (+ tes integrasi).
6. `feat(app): checkout & instruksi pembayaran` (domain `checkout` di frontend:
   `schemas/api/queries/components/index.ts`), plus polling yang berhenti saat tab
   tidak terlihat.
7. `docs(adr): ADR 0015 penjaga transisi & idempotensi callback`.

## Definition of Done (#14)

Ikuti §11 skill backend / §12 skill frontend, plus:

- [ ] `make build && make test && make lint && make format` hijau; app
      `typecheck/lint/test/build` hijau.
- [ ] Tes adapter gateway: tanda tangan benar & salah, pemetaan **setiap** status
      vendor → state domain, tanpa jaringan.
- [ ] Tes integrasi: checkout dua kali mengembalikan Transaction yang sama;
      callback ganda tidak menghasilkan transisi kedua; callback bertanda tangan
      salah ditolak + tercatat; `pending` kedaluwarsa → `expired`; transisi
      terlarang (mis. `paid → expired`) ditolak di batas HTTP.
- [ ] Setiap endpoint bersesi punya tes yang membuktikan Phone Number lain tidak
      bisa membaca/mengubah (PRD 13), dan `return` di luar allowlist diabaikan.
- [ ] Checkout nyaman di 360px; setiap layar punya keadaan memuat/gagal/kedaluwarsa.
- [ ] Fixture baru di `contracts/` + kedua sisi tesnya.
