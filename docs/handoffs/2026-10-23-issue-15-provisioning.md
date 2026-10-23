---
judul: Handoff — Issue #15 Penyediaan akun: FreeRADIUS, PIN & Extend
tanggal: 2026-10-23
posisi_main: 0c41d25
untuk: sesi baru yang mengerjakan #15
---

# Handoff — #15 Penyediaan akun: FreeRADIUS, PIN & Extend

> **Catatan kerja, bukan dokumen produk.** Hapus file ini setelah #15 merge; isi
> yang layak hidup lama harus menjadi ADR atau PRD.

## Posisi sekarang

`main` = `0c41d25`, CI hijau. Tertutup: #11 (prefactor), #12 (katalog), #13
(Magic Link + sesi + outbox SMS), **#14 (Transaction, kanal & pembayaran
Tripay)** lewat PR #20. Terbuka: **#15** (slice ini), #16 (Account Status),
#17 (pengerasan + rekonsiliasi + smoke E2E). PRD induk: #10 —
`docs/prd/transaction-purchase-app-backend.md`.

ADR yang mengikat: 0001 (FreeRADIUS writer), 0002 (MySQL terpisah), 0003/0010
(modem + outbox, outbox ditulis dalam transaksi pemanggil), 0004 (Phone Number +
PIN per pembelian, CHAP butuh password ter-hash-able), 0008 (Tripay di balik
port), 0009/0011 (cookie opaque + Magic Link), 0012 (seed katalog), 0013 (SPA),
0014 (baca Active Period lewat port), **0015 (penjaga transisi & idempotensi
callback)**. **Nomor berikutnya: 0016.**

## Cara menjalankan

```bash
cd backend
make deps build lint format            # lint harus 0 issues
make test                              # unit + integrasi (Postgres/Redis nyata)
make test-integration                  # hanya test/integration
make swagger-gen                       # output ke ../docs/api (git-ignored)
make run-api / run-worker / run-migrate
```

Integrasi butuh `redis-server` di `PATH` (atau `TEST_REDIS_ADDR`); Postgres
di-embed dan di-cache di `~/.cache`. **`TEST_FREERADIUS_MYSQL_DSN` wajib** untuk
tes penulis FreeRADIUS; tanpa itu tesnya `t.Skip` (dan acceptance #15 tidak
terbukti). Contoh: `radius:radius@tcp(127.0.0.1:3306)/radius`.

```bash
cd frontend
npm run typecheck && npm run lint && npm test && npm run build
```

Root (`npm test`) memuat tes Login Page dan tidak boleh berubah.

## Peta domain dan seam yang sudah ada

| Butuh | Pakai ini | Jangan |
| --- | --- | --- |
| Transaksi yang sudah lunas | `transaction.Service` (`Get`), entity `StatePaid`, `repository.Transition(ctx, id, from, to, at)` | menulis `state` dengan UPDATE sendiri tanpa penjaga `state = <from>` |
| Harga/durasi/rate limit Package | `packages.Service.GetPackage(ctx, id) (*entity.Package, error)` (`PriceIDR`, `DurationMinutes`, `RateLimit`) | mem-parse string tampilan `ListPackages` |
| Tulis akun hotspot | `provisioning.Writer.Apply(ctx, Account{PhoneNumber, PIN, Expiration, RateLimit})` — **port ada, adapter MySQL-nya belum** | query `radcheck`/`radreply` dari service |
| Baca expiry untuk Extend | `provisioning.ExpirationReader` (adapter MySQL sudah ada di `internal/pkg/database/radius_expiration.go`) | query MySQL langsung |
| Kirim SMS PIN | `notification/service.Outbox` (`Enqueue(ctx, Message)`) + `sms.TaskDeliver` | menyentuh modem/`sms_outbox` dari service |
| Waktu | `clock.Clock` disuntik | `time.Now()` di service/repository |
| Uang | `int64` rupiah | float |
| Phone Number | `phone.Normalize` (canonical `628…`), `phone.Mask` untuk log | membandingkan string mentah |
| PIN acak | helper baru berbasis `crypto/rand` (6 digit) | `math/rand`, atau `securetoken` (itu base64url, bukan PIN) |

Wiring: satu domain = `internal/application/<domain>/{dto,entity,repository,service,worker,module.go}`,
`Module()` untuk API, `WorkerModule()` untuk worker, `Migrations()` untuk tabel.
Route = `RouteRegistrar` di `internal/server/api/module.go`; task = `TaskRegistrar`
di `internal/server/worker` (lihat `notification/worker/drainer.go` dan
`transaction/worker/expiry.go` sebagai pola sweep + wake-up). Tabel baru wajib
masuk `migrations()` di `internal/server/migration/module.go`.

Harness: `testutil.NewAPIHarness(t)` menyediakan `FakeGateway`, `FakeSMSSender`,
`FakeRadiusWriter` (mencatat `Account`), `FakeRadiusReader`, `FakeClock`,
`harness.TransactionExpiry`. Karena port `provisioning.Writer` belum punya
adapter produksi, fake-nya saat ini di-`Provide`; setelah adapter ada, ubah
menjadi `fx.Decorate` seperti `payment.Gateway` (lihat `fakes.go`).

## Keputusan yang harus diambil sebelum/selagi kode (ADR 0016)

Tulis ADR 0016 lebih dulu; #15 menyentuh uang, kredensial, dan dua database.

**1. PIN di Postgres: enkripsi + kunci berversi.** PRD 7/32 meminta PIN terbaru
disimpan terenkripsi dengan kunci deployment berversi, agar Account Status (#16)
bisa menampilkannya walau SMS hilang. Usulan: AES-256-GCM, simpan
`pin_ciphertext` + `pin_key_version` **pada `transactions`** (PIN saat ini =
PIN transaksi `provisioned` terbaru untuk Phone Number itu). Ini menjaga PRD 7
yang hanya menyebut lima tabel (`packages`, `transactions`, `sessions`,
`magic_link_tokens`, `sms_outbox`) — jangan menambah tabel "account". Kunci
config baru (contoh `pin.encryption_key`, `pin.encryption_key_version`) **wajib**
muncul di `backend/internal/config/config.go` (defaults + validate),
`backend/config.sample.yaml`, `.env.example`, dan `docker-compose.yml` —
`test/integration/deployment_test.go` merah kalau salah satu terlewat. Putuskan
juga rotasi: baca berdasarkan versi yang tersimpan, tulis selalu dengan versi
aktif.

**2. Idempotensi provisioning lintas Postgres↔MySQL.** Retry tidak boleh
menambah durasi dua kali atau merotasi PIN. Usulan urutan:
1. hitung target expiration **sekali** (Extend: `max(now, expiry lama) + durasi`;
   expiry dari `ExpirationReader`), simpan di kolom transaksi dengan UPDATE
   terjaga `WHERE provision_target_expiration IS NULL`;
2. tulis PIN (buat + enkripsi + simpan `pin_ciphertext`) **sebelum** menulis
   RADIUS, sehingga retry memakai PIN yang sama;
3. upsert FreeRADIUS ke target yang tersimpan (bukan menambah lagi);
4. `paid → provisioned` lewat `repository.Transition` yang sudah terjaga, lalu
   baris outbox SMS (langkah 5).
Kalau langkah 3 sukses tapi langkah 4 gagal, retry menulis target yang sama →
aman. Dokumenkan bahwa konsistensi antar-DB tidak atomik dan target tersimpan
adalah kompensasinya.

**3. `Outbox.EnqueueTx`.** ADR 0010 mewajibkan baris outbox ditulis dalam
transaksi Postgres pemanggil. Tapi service domain tidak boleh memegang
`*gorm.DB`. Pilih satu bentuk dan tulis di ADR: (a) `notification.Outbox`
mendapat method yang menerima handle transaksi abstrak, dengan repository
transaksi menyediakan `WithTx(ctx, func(tx T) error)`; atau (b) repository
transaksi yang menulis kolom transaksi + baris `sms_outbox` dalam satu transaksi
GORM (melanggar batas domain, tapi sederhana). Rekomendasi: (a), dengan tipe
`Tx` kecil didefinisikan di paket notification dan diimplementasi adapter GORM.

**4. Batas PIN API #15 vs Account Status #16.** Acceptance #15 minta "PIN tetap
bisa dibaca lewat API", sedangkan ringkasan Account Status milik #16. Putuskan:
`GET /transactions/{id}` mengembalikan `pin` hanya saat `provisioned`, atau
endpoint terpisah `GET /account/pin` yang dipakai ulang #16. Jangan
mengembalikan PIN di endpoint publik atau di daftar mana pun.

**5. Frontend.** `PaymentInstructions` saat `paid` sekarang hanya berkata "PIN
dikirim lewat SMS / lihat Account Status". #15 harus menampilkan **Phone
Number, PIN, langkah berikutnya, dan tautan kembali** (hanya bila
`transaction.return_url` tidak kosong — nilainya sudah divalidasi backend).
Polling sekarang berhenti di `paid` (`checkout.schema.ts` `terminalStates`);
setelah #15, biarkan berhenti di `provisioned` (atau `failed`/`expired`) supaya
PIN muncul tanpa reload. Kontrak `transaction.*.json` akan tumbuh field PIN →
ubah fixture, DTO Go (`DisallowUnknownFields`), dan zod strict bersamaan.

## Isi #15 (dari issue)

1. Pekerjaan latar penyediaan dipicu `paid → provisioned`, idempoten terhadap
   Transaction (retry tidak menambah durasi, tidak merotasi PIN).
2. Tulis FreeRADIUS (ADR 0001) untuk username = Phone Number: `Cleartext-Password`
   = PIN 6 digit (CHAP, ADR 0004), `Expiration` (epoch detik), dan
   `Mikrotik-Rate-Limit` dari Package — upsert dalam **satu transaksi MySQL**
   `radcheck`/`radreply` (satu baris per atribut per username).
3. Aturan Extend dihitung backend (PRD 21): expiry lama masih depan → tambah
   durasi; belum ada/lewat → `settlement time + durasi`.
4. PIN terbaru terenkripsi (kunci berversi) agar bisa ditampilkan lagi.
5. PIN dikirim lewat outbox SMS yang sudah ada.
6. Layar selesai: Phone Number, PIN, langkah berikutnya, tautan kembali ke Login
   Page asal bila `return` ada di allowlist.

**Batas slice:** Account Status lengkap (riwayat, sisa waktu, sembunyikan PIN)
milik #16; rekonsiliasi `Gateway.GetStatus` + smoke sandbox/hotspot nyata milik
#17.

## Jebakan yang sudah pernah menggigit

- **Rate limit Redis tidak ber-namespace**: tes integrasi wajib memakai Phone
  Number **dan** `RemoteAddr` unik per tes. Nomor yang sudah dipakai tes lain
  membuat permintaan Magic Link tidak mengirim SMS (gejala: "no SMS queued").
- **Kunci config baru wajib di 4 tempat** (lihat di atas); `deployment_test`
  memeriksa `.env.example` dan `docker-compose.yml` untuk setiap key.
- **Fixture kontrak** diuji dua sisi: Go `DisallowUnknownFields` + zod
  `.strict()`. Menambah field PIN tanpa memperbarui keduanya = merah.
- **Clock**: jangan `time.Now()` di service/repo; tes memakai `FakeClock` dan
  memajukannya.
- **Tes MySQL** membuat sendiri tabel yang disentuh (`radcheck`, `radreply`)
  karena schema FreeRADIUS bukan milik backend; jangan mengandalkan init
  container. Bersihkan baris di `t.Cleanup`.
- **PIN tidak boleh masuk log** (pakai `phone.Mask`, jangan `zap.String("pin")`),
  dan tidak boleh tersimpan plaintext di Postgres.
- **`radcheck` Expiration = epoch seconds**, `op` `:=`; `radreply` `op` `=`.
  Upsert per username+attribute, jangan `INSERT` berulang.
- **Worker** adalah satu-satunya proses yang boleh memegang modem; provisioning
  boleh berjalan di worker, tapi jangan menambah proses kedua yang memegang
  perangkat.
- **CI**: `concurrency` + `cancel-in-progress` membatalkan run lama saat push
  beruntun. "cancelled" bukan merah; tunggu run terakhir.

## Urutan commit yang disarankan

1. `feat(pin): PIN 6 digit + enkripsi AES-GCM berkunci versi` (+ tes, config 4
   tempat).
2. `feat(provisioning): penulis FreeRADIUS upsert radcheck/radreply` (+ tes MySQL
   nyata untuk pembelian pertama, Extend, dan setelah expiry lewat).
3. `feat(transaction): target provisioning & PIN tersimpan pada Transaction`
   (migrasi, repo, set-once, enkripsi).
4. `feat(notification): Outbox.EnqueueTx` (+ tes transaksi bersama).
5. `feat(transaction): worker provisioning paid → provisioned + SMS PIN`
   (+ integrasi: retry idempoten, modem mati tetap provisioned).
6. `feat(app): layar selesai dengan PIN + polling sampai provisioned`
   (+ fixture kontrak dua sisi).
7. `docs(adr): ADR 0016 PIN-at-rest & idempotensi provisioning lintas DB`.

## Definition of Done (#15)

Ikuti §11 skill backend / §12 skill frontend, plus:

- [ ] `make build && make test && make lint && make format` hijau; app
      `typecheck/lint/test/build` hijau.
- [ ] Penulis FreeRADIUS diuji menulis sungguhan ke MySQL dan membaca kembali
      `radcheck`/`radreply`: pembelian pertama, Extend (durasi bertambah), dan
      pembelian setelah masa aktif habis (mulai dari settlement).
- [ ] Menjalankan ulang pekerjaan penyediaan tidak menambah durasi dan tidak
      merotasi PIN.
- [ ] PIN selalu 6 digit, berganti setiap pembelian, tidak pernah plaintext di
      Postgres, dan tidak pernah di log.
- [ ] Modem mati: Transaction tetap `provisioned`, SMS menunggu di outbox, dan
      PIN tetap bisa dibaca lewat API.
- [ ] Layar selesai menampilkan Phone Number, PIN, langkah berikutnya, dan
      kembali ke Login Page hanya bila `return` ada di allowlist.
- [ ] Fixture kontrak baru (PIN) diuji dua sisi.
- [ ] `Gateway.GetStatus` tetap tanpa pemanggil sampai #17 (jangan dibiarkan
      dipakai diam-diam tanpa tes rekonsiliasi).
