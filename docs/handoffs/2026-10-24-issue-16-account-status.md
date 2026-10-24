---
judul: Handoff — Issue #16 Account Status & Extend
tanggal: 2026-10-24
posisi_main: 3ccefe1
untuk: sesi baru yang mengerjakan #16
---

# Handoff — #16 Account Status & Extend

> **Catatan kerja, bukan dokumen produk.** Hapus file ini setelah #16 merge; isi
> yang layak hidup lama harus menjadi ADR atau PRD.

## Posisi sekarang

`main` = `e3ade4b`, CI hijau. Tertutup: #11 (prefactor), #12 (katalog), #13
(Magic Link + sesi + outbox SMS), #14 (Transaction, kanal & pembayaran Tripay),
**#15 (penyediaan FreeRADIUS, PIN & Extend)** lewat PR #21 (rebase merge).
Terbuka: **#16** (slice ini), #17 (pengerasan + rekonsiliasi + smoke E2E). PRD
induk: #10 — `docs/prd/transaction-purchase-app-backend.md`.

ADR yang mengikat: 0001 (penulis FreeRADIUS), 0004 (Phone Number + PIN),
0009/0011 (cookie opaque + Magic Link), 0014 (baca Active Period lewat port),
0015 (penjaga transisi), **0016 (PIN terenkripsi & idempoten lintas DB)**.
**Nomor berikutnya: 0017.**

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
sekarang **gagal keras** kalau `CI=true` tanpa DSN — jangan mengandalkan skip.

```bash
cd frontend
npm run typecheck && npm run lint && npm test && npm run build
```

Root `npm test` memuat tes Login Page dan tidak boleh berubah.

## Peta domain dan seam yang sudah ada

| Butuh | Pakai ini | Jangan |
| --- | --- | --- |
| PIN saat ini | `transaction.Service.CurrentPIN(ctx, phoneNumber) (string, bool, error)` — PIN transaksi `provisioned` terbaru, sudah didekripsi | query `pin_ciphertext`/dekripsi sendiri; tampilkan PIN di daftar/riwayat |
| Sisa Active Period (angka) | `provisioning.ExpirationReader.Expiration(ctx, canonicalPhone)` → `{ExpiresAt, Found}` (adapter MySQL ada) | memakai `activeperiod.Service` (itu kontrak string Login Page, bukan angka) |
| Batas kecepatan Package aktif | `packages.Service.GetPackage(ctx, id)` → `RateLimit`; id dari transaksi `provisioned` terbaru | mengarang dari `package_name`; menambah kolom duplikat |
| Phone Number pemilik | `middleware.SessionFrom(c)` → `Session.PhoneNumber`; `middleware.RequireOwnPhoneNumber(c, body)` → 404 | percaya parameter Phone Number dari klien; 403 |
| Riwayat Transaction | **belum ada query daftar**: tambah `ListByPhone(ctx, canonicalPhone, AccountQueryParams)` di repo (params milik repo, bukan dto) | memakai `repository.Pending`/`GetByID` berulang; `SELECT *` di handler |
| Waktu | `clock.Clock` disuntik (FakeClock di tes) | `time.Now()` di service/repository |
| Uang | `int64` rupiah | float |
| Format durasi | `packages/service/format.go` (`FormatActivePeriod`) untuk teks Login Page; app merender sisanya | menghitung ulang di handler |

Tabel yang ada: `packages`, `transactions` (termasuk `pin_ciphertext`,
`pin_key_version`, `provision_target_expiration`, `return_url`, `paid_at`),
`sessions`, `magic_link_tokens`, `sms_outbox`. **Jangan menambah tabel
`account`** (PRD 6/7): Phone Number adalah satu-satunya identitas.

Wiring: `transaction.Module()` (API) vs `WorkerModule()` (worker),
`RouteRegistrar` di `internal/server/api/module.go`, `TaskRegistrar` di
`internal/server/worker`. Harness: `testutil.NewAPIHarness(t)` menyediakan
`Doubles.{Gateway,SMS,Radius,RadiusReader,Clock}`, `Catalog`,
`TransactionExpiry`, `TransactionProvisioning`, `TransactionService`.

## Keputusan yang harus diambil sebelum/selagi kode (ADR 0017)

Tulis ADR 0017 lebih dulu: #16 menyentuh kredensial dan satu endpoint baru yang
dibaca Login Page dan aplikasi.

**1. Bentuk permukaan bacaan.** Usulan: pisahkan tiga hal agar PIN tidak pernah
ikut di daftar dan riwayat bisa dipaginasi —

- `GET /account` (sesi) → `{ phone_number, active_period: { expires_at, found, state }, package: { id, name, rate_limit } | null, pin }`
- `GET /transactions?page=&page_size=` (sesi) → `{ data: [...], total_count, page, page_size }`, **tanpa** `pin`
- `GET /transactions/{id}` sudah ada (ADR 0016) — putuskan apakah app menampilkan `pin` untuk transaksi lama (rekomendasi: hanya ringkasan akun yang menampilkan PIN; detail transaksi lama tidak merendernya).

Alternatif yang ditolak: satu `GET /account` raksasa berisi riwayat (PIN
terbawa ke payload yang di-cache, pagination mustahil).

**2. Ambang "hampir berakhir" harus sama dengan Login Page.** Sumbernya
`KOPINETGO-2024/config/data/voucher.js` → `expiryWarningHours: 24`
(dipakai `voucher-client.js` `activePeriodStatus`: `remainingMs <= warningMs` →
`expiring`). Usulan: konfigurasi backend `account.expiry_warning_hours` (default
24) dan **backend yang menghitung `active_period.state`** (`active|expiring|expired`)
dari `clock.Clock`, app hanya merender. Dengan begitu app dan halaman status
Login Page tidak bisa berbeda pendapat; nilai 24 didokumentasikan sebagai "harus
sama dengan `expiryWarningHours` Login Page". Jangan mengirim ambang ke klien
lalu menghitung di sana.

**3. Pratinjau Extend (PRD story 49).** `GET /account/extend-preview?package_id=`
→ `{ estimated_expiration }`, atau sertakan di detail Package. **Aturannya harus
satu dengan provisioning**: `Provisioner.targetExpiration` (masih aktif → expiry
lama + durasi; kosong/lewat → sekarang + durasi). Ekstrak aturan itu menjadi satu
fungsi/port yang dipakai provisioner **dan** pratinjau — jangan menyalin rumusnya.
Dokumentasikan bahwa pratinjau bukan angka final; angka final tetap dihitung saat
penyediaan.

**4. PIN default tersembunyi** di UI (tombol tunjuk/sembunyi) dan hanya boleh
lewat sesi pemilik. `pin` tidak pernah masuk query string, log, atau payload
daftar.

## Isi #16 (dari issue)

1. Ringkasan: **Package** aktif + batas kecepatan, sisa **Active Period** mudah
   dibaca, tanggal/jam berakhir.
2. **PIN** saat ini dengan tombol tunjuk/sembunyi.
3. Riwayat **Transaction** (tanggal, Package, jumlah, status) + detail satu
   Transaction.
4. Keadaan kosong (belum pernah beli → arahkan ke katalog), kedaluwarsa
   (Active Period habis → tindakan beli), sesi kedaluwarsa/dicabut (→ Magic Link
   baru, bukan halaman kosong).
5. Indikator hampir berakhir dengan ambang jam yang sama dengan halaman status
   Login Page.
6. Alur **Extend**: pembelian saat masih aktif dikonfirmasi sebagai **Extend**
   dengan perkiraan expiration baru sebelum membayar.

**Batas slice:** rekonsiliasi `Gateway.GetStatus` + smoke sandbox/hotspot nyata
milik #17; Admin Panel di luar cakupan PRD.

## Jebakan yang sudah pernah menggigit

- **Rate limit Redis tidak ber-namespace**: tes integrasi wajib memakai Phone
  Number **dan** `RemoteAddr` unik per tes.
- **Kunci config baru wajib di 4 tempat** (`config.go` defaults+validate,
  `config.sample.yaml`, `.env.example`, `docker-compose.yml`);
  `test/integration/deployment_test.go` merah kalau salah satu terlewat.
- **Fixture kontrak diuji dua sisi**: Go `DisallowUnknownFields` + zod
  `.strict()`, dan `contracts/README.md` harus memuat fixture baru.
- **PIN**: tidak di log (`phone.Mask` untuk Phone Number), tidak plaintext di
  Postgres, tidak di daftar. Body SMS dikosongkan saat baris outbox final —
  jangan mengandalkan isi `sms_outbox.message` setelah `sent`.
- **Clock**: `FakeClock` untuk `expiring`/`expired`; jangan `time.Sleep`.
- **Frontend**: layering `route → component → query/state → api → klien axios`;
  domain baru `account` dengan `index.ts`; jangan menaruh PIN di `$state`
  global, cukup state UI di komponen/`state.account.svelte.ts`.
- Route `/` sekarang halaman status sistem dan `+page.ts` mengalihkan
  `?package=`/`?return=` ke `/masuk`; Account Status perlu route sendiri
  (mis. `/akun`) dan tautan dari layar selesai checkout.
- `GET /active-period` publik **tidak boleh berubah bentuk** (kontrak Login Page).

## Urutan commit yang disarankan

1. `docs(adr): ADR 0017` — tulis **lebih dulu** (bentuk permukaan bacaan, ambang
   hampir berakhir, satu aturan Extend), seperti #15 menulis ADR 0016 di commit
   pertama.
2. `feat(account): ringkasan akun + PIN lewat sesi` (repo query, service, handler,
   tes handler+integrasi).
3. `feat(transaction): riwayat Transaction berpaginasasi` (+) fixture kontrak
   `transactions.response.json` dua sisi.
4. `feat(account): pratinjau Extend` (+ ekstrak aturan Extend bersama provisioner,
   satu tes membuktikan keduanya memakai angka yang sama).
5. `feat(app): halaman Account Status` (ringkasan, PIN tunjuk/sembunyi, riwayat,
   keadaan kosong/kedaluwarsa/sesi habis) + tes komponen.
6. `feat(app): konfirmasi Extend + perkiraan expiration` + tes.

## Definition of Done (#16)

Ikuti §11 skill backend / §12 skill frontend, plus:

- [ ] `make build && make test && make lint && make format` hijau; app
      `typecheck/lint/test/build` hijau; root `npm test` tidak berubah.
- [ ] Sesi membuka Account Status: Package aktif, sisa waktu, expiration persis,
      PIN saat ini (tersembunyi secara bawaan, bisa ditunjuk).
- [ ] Riwayat menampilkan semua Transaction dengan status benar; detail satu
      Transaction bisa dibuka; **tidak ada `pin` di payload riwayat**.
- [ ] Tanpa sesi / sesi kedaluwarsa → penjelasan + tawaran Magic Link baru.
- [ ] Belum pernah membeli → keadaan kosong menuju katalog; Active Period habis →
      keadaan kedaluwarsa dengan tindakan beli.
- [ ] Indikator hampir berakhir memakai ambang yang sama dengan Login Page
      (buktikan dengan clock: 23 jam → expiring, 25 jam → active pada ambang 24).
- [ ] Extend: membeli saat masih aktif menampilkan konfirmasi Extend + perkiraan
      expiration; setelah settlement, expiration yang tampil sama dengan
      perkiraan itu (pratinjau dan provisioning memakai satu aturan).
- [ ] API menolak menampilkan PIN/riwayat milik Phone Number lain, dibuktikan
      lewat uji pada batas HTTP (404, bukan 403).
- [ ] `Gateway.GetStatus` tetap tanpa pemanggil sampai #17.
