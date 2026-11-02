---
status: ready
judul: Admin Panel — Katalog Package, Daftar Transaction, dan Pengguna Aktif
pelaku: Admin (owner, support, auditor), Operator hotspot
---

# PRD — Admin Panel

> **Status triase:** `ready` — belum ada issue. Dua slice (lihat § Slice).
> **Konteks:** `CONTEXT.md` (glosarium domain, mengikat) dan `docs/adr/` (ADR 0001–0023, ditambah 0024–0028 dari PRD ini).
> **Ruang lingkup:** monorepo ini. Menambah domain backend `admin` dan aplikasi `admin/`; **Login Page** dan aplikasi pembelian tidak diubah perilakunya.

## Problem Statement

Kedua PRD sebelumnya sudah delivered, dan **Admin Panel** adalah satu-satunya area yang tersisa — ia bahkan disebut sebagai pekerjaan tertunda di ADR 0012 dan ADR 0018. Hari ini belum ada satu pun kode admin di repo: tidak ada domain `admin`, tidak ada konsep peran, dan tidak ada UI. Akibatnya, dari sudut pandang operator:

- **Katalog hanya bisa diubah lewat SQL.** Satu-satunya jalur tulis adalah seed deployment (ADR 0012). Karena seed memakai `ON CONFLICT (code) DO NOTHING`, harga yang diubah dengan tangan bertahan — tetapi **Package yang dihapus dengan tangan hidup lagi pada deploy berikutnya**, dan operator tidak punya cara menjelaskannya kepada siapa pun.
- **Tidak ada cara menjawab telepon pembeli.** Operator tidak bisa melihat apakah sebuah Transaction sudah lunas, sudah diprovisioning, atau SMS-nya gagal; semua informasi itu ada di database dan log, bukan di tangan orang yang mengangkat telepon.
- **PIN hanya bisa diambil pembeli sendiri.** Jalur resminya adalah Account Status lewat Magic Link (ADR 0017). Pembeli yang kehilangan SMS **dan** tidak bisa membuka tautan tidak punya jalan lain, dan operator tidak punya alat untuk membantunya.
- **Tidak ada jejak siapa mengubah harga.** Perubahan katalog dan pembukaan kredensial akan menjadi aksi paling sensitif di sistem ini, dan hari ini tidak ada satu pun catatan yang bisa ditanyakan.
- **Tidak ada identitas admin sama sekali.** `auth` hanya mengenal Phone Number + Magic Link untuk pembeli; tidak ada aktor, peran, atau sesi yang bisa dipakai operator.

## Solution

Menambah **Admin Panel** ke monorepo sebagai aktor dan permukaan tersendiri:

1. **Akun admin terpisah dengan password** (bcrypt) dan **sesi admin terpisah** (tabel + cookie sendiri), karena operator harus bisa masuk ketika modem/SMS sedang bermasalah (ADR 0024). Tiga peran dengan matriks izin eksplisit: `owner`, `support`, `auditor`.
2. **Owner pertama di-seed `migrate`** dari `ADMIN_EMAIL`/`ADMIN_PASSWORD`, idempoten, dan tidak pernah menimpa password yang sudah ada.
3. **Tulis katalog melalui panel**: membuat Package, mengubah nama/harga/durasi/rate limit, dan **menonaktifkan** (bukan menghapus) Package. Package yang nonaktif hilang dari Login Page tetapi tetap ada di panel dan di riwayat (ADR 0025).
4. **Snapshot istilah katalog di Transaction** (`DurationMinutes`, `RateLimit`) saat checkout, dan provisioning membaca snapshot — supaya edit katalog tidak mengubah apa yang diterima pembeli yang sudah membayar (ADR 0026).
5. **Jejak audit append-only** untuk setiap tulis katalog dan setiap pembukaan PIN, ditulis dalam transaksi DB yang sama dengan perubahannya, dan bisa dibaca owner/auditor (ADR 0027).
6. **Daya lihat untuk menjawab pembeli**: daftar Transaction dengan filter, pembukaan PIN satu per satu (dibatasi + diaudit), dan daftar pengguna aktif dari agregat Postgres dengan verifikasi per-baris ke FreeRADIUS.
7. **Panel sebagai aplikasi SvelteKit terpisah** (`admin/`) dengan image dan service compose sendiri, disajikan proxy di `/admin` (ADR 0028).

## User Stories

### Masuk sebagai admin

1. Sebagai owner, saya ingin masuk ke panel dengan email dan password, sehingga saya tidak bergantung pada SMS yang justru sedang bermasalah.
2. Sebagai admin, saya ingin sesi saya berakhir sendiri setelah 12 jam, sehingga laptop yang tertinggal tidak menjadi pintu terbuka sepanjang hari.
3. Sebagai admin, saya ingin keluar seketika, sehingga sesi yang bocor bisa saya matikan dari sisi server.
4. Sebagai admin, saya ingin mengganti password saya sendiri, sehingga saya tidak perlu meminta owner mengetikkan password baru untuk saya.
5. Sebagai admin, saya ingin pesan galat login yang sama untuk email tak dikenal dan password salah, sehingga panel ini tidak bisa dipakai menebak email admin.
6. Sebagai admin, saya ingin percobaan login dibatasi per IP dan per email, sehingga tebakan password berulang berhenti dengan sendirinya.

### Mengelola katalog Package

7. Sebagai owner, saya ingin melihat seluruh Package termasuk yang nonaktif, sehingga saya tahu apa yang pernah dijual dan apa yang sedang dijual.
8. Sebagai owner, saya ingin membuat Package baru (code, nama, harga, durasi, rate limit), sehingga saya bisa menjual penawaran baru tanpa deploy.
9. Sebagai owner, saya ingin mengubah harga, nama, durasi, dan rate limit Package, sehingga penawaran mengikuti keputusan bisnis hari ini.
10. Sebagai owner, saya ingin `code` tidak bisa diubah setelah Package dibuat, sehingga seed tidak menganggap Package itu baru dan membuat baris kedua.
11. Sebagai owner, saya ingin menonaktifkan Package alih-alih menghapusnya, sehingga ia berhenti dijual tanpa menghilang dari riwayat dan tanpa dihidupkan lagi oleh deploy berikutnya.
12. Sebagai owner, saya ingin mengaktifkan kembali Package yang nonaktif, sehingga keputusan yang salah bisa dibalik.
13. Sebagai owner, saya ingin panel menolak menonaktifkan Package aktif terakhir, sehingga Login Page tidak pernah menampilkan daftar kosong.
14. Sebagai owner, saya ingin nilai yang tidak masuk akal ditolak (harga nol/negatif, durasi nol, rate limit salah format, nama kosong), sehingga FreeRADIUS dan tagihan tidak menerima nilai rusak.
15. Sebagai owner, saya ingin tahu kalau harga sudah diubah admin lain saat saya menyimpan, sehingga saya tidak menimpa perubahan tanpa melihatnya.
16. Sebagai auditor, saya ingin melihat katalog tanpa bisa mengubahnya, sehingga saya bisa memeriksa penawaran yang berjalan tanpa risiko salah tekan.

### Menjawab pembeli: Transaction dan PIN

17. Sebagai support, saya ingin mencari Transaction berdasarkan Phone Number, state, Package, dan rentang tanggal, sehingga saya bisa menjawab "pembayaran saya bagaimana?" dalam hitungan detik.
18. Sebagai support, saya ingin melihat state pembayaran, channel, reference gateway, jumlah percobaan provisioning, dan status SMS tiap Transaction, sehingga saya bisa membedakan "belum dibayar", "sudah dibayar tapi belum diprovisioning", dan "SMS gagal".
19. Sebagai support, saya ingin daftar Transaction tidak menampilkan PIN, sehingga membaca daftar tidak pernah membocorkan kredensial siapa pun.
20. Sebagai support, saya ingin membuka PIN satu Transaction atas permintaan pembeli, sehingga pembeli yang kehilangan SMS tidak kehilangan akses.
21. Sebagai owner, saya ingin setiap pembukaan PIN tercatat (siapa, kapan, Transaction mana), sehingga tindakan itu bisa ditinjau.
22. Sebagai auditor, saya ingin daftar Transaction tanpa tombol buka PIN, sehingga peran saya membaca dan tidak menyentuh kredensial.
23. Sebagai support, saya ingin melihat nama Package yang **dibeli** (snapshot) pada baris Transaction, sehingga harga dan nama yang ditampilkan tidak berubah karena katalog diedit.
24. Sebagai support, saya ingin hasil pencarian dipaginasi, sehingga daftar besar tetap bisa dipakai.

### Pengguna aktif

25. Sebagai support, saya ingin melihat daftar pengguna aktif (Phone Number, Package terakhir, expiry, jumlah pembelian), sehingga saya bisa memastikan seorang pelanggan memang masih punya masa aktif.
26. Sebagai support, saya ingin daftar itu menyaring berdasarkan jendela kedaluwarsa (mis. habis dalam 3 hari), sehingga saya bisa menjawab "kapan masa aktif saya habis?".
27. Sebagai support, saya ingin halaman detail satu pengguna memverifikasi expiry-nya ke FreeRADIUS, sehingga saya tahu apakah catatan pembelian aplikasi masih cocok dengan yang dipegang hotspot.
28. Sebagai support, saya ingin daftar itu menyebut dirinya "menurut catatan pembelian aplikasi", sehingga saya tidak salah menyimpulkan untuk akun yang dibuat di luar aplikasi.
29. Sebagai auditor, saya ingin melihat daftar pengguna aktif, sehingga saya bisa memeriksa klaim jumlah pelanggan.
30. Sebagai admin, saya ingin filter dan paginasi yang sama bentuknya dengan daftar Transaction, sehingga saya tidak perlu mempelajari dua pola.

### Jejak dan tata kelola

31. Sebagai owner, saya ingin audit log tempat setiap perubahan katalog tercatat dengan nilai sebelum dan sesudah, sehingga pertanyaan "siapa menaikkan harga ini?" punya jawaban.
32. Sebagai owner, saya ingin audit log tidak bisa diubah atau dihapus dari panel, sehingga jejaknya bisa dipercaya.
33. Sebagai owner, saya ingin audit log bisa disaring per admin, per aksi, dan per tanggal, sehingga peninjauan tidak berarti membaca semuanya.
34. Sebagai auditor, saya ingin membaca audit log, sehingga pemeriksaan tidak bergantung pada orang yang diperiksa.
35. Sebagai support, saya ingin tidak bisa membaca audit log, sehingga saya tidak bisa menyesuaikan diri dengan jejak rekannya.
36. Sebagai owner, saya ingin menambah admin, mengganti peran, mereset password, dan menonaktifkan admin, sehingga keanggotaan tim bisa berubah tanpa deploy.

### Operator deployment

37. Sebagai operator, saya ingin owner pertama dibuat oleh `migrate` dari environment, sehingga panel bisa dipakai tanpa menyentuh database dengan tangan.
38. Sebagai operator, saya ingin `migrate` gagal keras bila belum ada admin dan kredensial bootstrap tidak diisi, sehingga saya tidak menemukan panel tanpa pintu masuk setelah deploy.
39. Sebagai operator, saya ingin bootstrap tidak pernah menimpa password yang sudah ada, sehingga deploy tidak diam-diam mengembalikan password lama.
40. Sebagai operator, saya ingin panel disajikan sebagai image dan service sendiri di `/admin`, sehingga ia bisa dinaikkan versinya terpisah dari aplikasi pembelian.
41. Sebagai operator, saya ingin `GET /api/packages` tetap berbentuk sama (hanya menyaring Package nonaktif), sehingga Login Page yang dibekukan tidak perlu diubah.
42. Sebagai operator, saya ingin panel punya CSP dan header sendiri, sehingga kebijakan aplikasi pembelian dan panel tidak saling mengunci.

## Implementation Decisions

### Identitas, sesi, dan peran admin (ADR 0024)

- Entity `Admin`: `email` (unik, kanonik lowercase), `password_hash` (bcrypt cost 12), `role` (`owner`|`support`|`auditor`), `is_active`, `created_at`, `updated_at`.
- Entity `AdminSession`: `token_hash` (SHA-256, unik), `admin_id`, `expires_at`, `revoked_at` — bentuknya mengikuti sesi pembeli (ADR 0009/0011), tabelnya terpisah.
- Cookie: `HttpOnly; Secure` (mengikuti `AUTH_COOKIE_SECURE`), `SameSite=Strict`, `Path=/api/admin`, nama sendiri (mis. `hotspot_admin`), TTL absolut `ADMIN_SESSION_TTL` (default 12 jam) tanpa perpanjangan.
- Matriks izin (ditegakkan middleware per route, bukan hanya UI):

  | Aksi | owner | support | auditor |
  | --- | --- | --- | --- |
  | Lihat katalog (termasuk nonaktif) | ✓ | ✓ | ✓ |
  | Tulis katalog (buat/ubah/nonaktif) | ✓ | ✗ | ✗ |
  | Lihat daftar Transaction | ✓ | ✓ | ✓ |
  | Buka PIN (dengan audit) | ✓ | ✓ | ✗ |
  | Lihat pengguna aktif + verifikasi | ✓ | ✓ | ✓ |
  | Kelola admin | ✓ | ✗ | ✗ |
  | Lihat audit log | ✓ | ✗ | ✓ |
  | Ubah password sendiri | ✓ | ✓ | ✓ |

- Password: minimum 12 karakter, tanpa aturan komposisi, ditolak bila sama dengan email. Rate limit login per IP **dan** per email memakai pola konfigurasi yang sudah ada (`ADMIN_RATE_LIMIT_*`). Tanpa lockout. Respons galat seragam (tidak ada enumerasi akun).
- Reset password oleh owner dan penonaktifan admin **mencabut semua sesi** admin itu seketika.
- MFA/TOTP: di luar v1.

### Bootstrap owner

- `migrate` men-seed satu Admin `owner` dari `ADMIN_EMAIL` + `ADMIN_PASSWORD`.
- Bila tabel admin **kosong** dan env tidak diisi → perintah gagal dengan pesan jelas.
- Bila admin sudah ada → env diabaikan sepenuhnya; password tidak pernah di-reset oleh deploy.

### Tulis katalog (ADR 0025)

- `packages` mendapat `is_active` (default true) dan `version` (integer, naik setiap tulis).
- `GET /api/packages` (publik) menyaring `is_active = true`; bentuk respons tidak berubah.
- Tidak ada hard delete. Menonaktifkan Package **tidak** membatalkan Transaction pending: uang yang sudah masuk tetap diprovisioning (ADR 0015/0016).
- `code` imutabel setelah dibuat; format `^[a-z0-9]+(-[a-z0-9]+)*$`; unique.
- Validasi di **service**, bukan hanya form: nama 1–80 karakter setelah trim tanpa karakter kontrol; `price_idr` bulat `> 0` dan `≤ 100.000.000`; `duration_minutes > 0` dan `≤ 527.040` (366 hari); `rate_limit` cocok `^\d+(\.\d+)?[kKmMgG]?/\d+(\.\d+)?[kKmMgG]?$`.
- Menonaktifkan Package aktif terakhir ditolak (409) — Login Page tidak boleh kosong.
- Ubah Package wajib menyertakan `version`; versi tidak cocok → `409` beserta nilai terkini.

### Snapshot istilah katalog di Transaction (ADR 0026)

- `transactions` mendapat `duration_minutes` dan `rate_limit` yang diisi dari katalog **saat checkout**, sejajar dengan `package_name`/`amount_idr` yang sudah ada.
- `provisioning` membaca durasi dan rate limit dari Transaction, bukan dari katalog. `ProvisionTargetExpiration` tetap disimpan sekali (ADR 0016), sehingga retry tidak bisa berubah pikiran.
- Baris `pending` yang lahir sebelum kolom ini ada di-backfill sekali di `migrate`; fallback baca katalog hanya untuk baris yang snapshot-nya kosong.

### Audit (ADR 0027)

- Tabel `admin_audit_log` append-only: `id`, `created_at`, `admin_id` + `admin_email` (disalin, supaya baris tetap terbaca bila admin dihapus), `action` (`package.create`|`package.update`|`package.activate`|`package.deactivate`|`pin.reveal`|`admin.create`|`admin.update`|`admin.password_reset`|`admin.deactivate`), `target_type`, `target_id`, `payload` (JSONB kecil: nilai sebelum/sesudah untuk field katalog), `ip`.
- Ditulis lewat port `AuditRecorder` yang didefinisikan domain pemilik aksi (`packages`, `transaction`) dan diimplementasikan adaptor di domain `admin` — bentuk yang sama dengan preseden `eventbus.Publisher` (skill backend §8.3). Insert berjalan **di dalam transaksi DB yang sama** dengan perubahan.
- Tidak ada endpoint ubah/hapus. Retensi/rotasi di luar v1.

### Daftar Transaction dan buka PIN

- `transaction.Service` mendapat `ListTransactions(ctx, params)` dan `RevealPIN(ctx, id, actor)`; params milik repository, bukan `dto.*`.
- Daftar: waktu, Phone Number, Package (snapshot), amount, channel, state, reference, gateway reference, `provision_attempts`, status SMS terakhir dari `sms_outbox`. **Tanpa** `pin`.
- Filter: `state`, `phone_number` (exact), `package_id`, `from`/`to`; paginasi `{data, total_count, page, page_size}`.
- Buka PIN: `POST /api/admin/transactions/{id}/pin` → dekripsi AES-GCM (ADR 0016), hanya owner/support, dibatasi laju, dan **selalu** menghasilkan baris audit. Kegagalan dekripsi dijawab galat generik, tanpa membocorkan detail.

### Pengguna aktif

- `transaction.Service.ListSubscribers(ctx, params)`: agregat per Phone Number dari Transaction `provisioned` — `MAX(provision_target_expiration)`, Package terakhir dari snapshot, jumlah pembelian, status turunan (`active`/`expiring`/`expired`) memakai ambang yang sama dengan ADR 0017.
- Daftar diberi label eksplisit **"menurut catatan pembelian aplikasi"**: akun yang dibuat Mikhmon/dengan tangan tidak muncul, dan itu dinyatakan di UI, bukan hanya di dokumen.
- Detail per pengguna memverifikasi satu nomor ke FreeRADIUS memakai `ExpirationReader` yang sudah ada dan menampilkan cocok/tidak cocok.

### Domain backend `admin`

- `internal/application/admin/{dto,entity,repository,service,handler,module.go}`: akun, sesi, audit, middleware peran, seluruh route `/api/admin/**`.
- Method tulis tinggal di domain pemilik: `packages.Service` (`CreatePackage`, `UpdatePackage`, `SetActive`), `transaction.Service` (`ListTransactions`, `RevealPIN`, `ListSubscribers`).
- Cross-domain lewat interface Service (golden rule 5); handler admin tidak menyentuh repository domain lain, dan tidak ada domain yang menyentuh `*gorm.DB` domain lain.

### Kontrak HTTP admin

| Endpoint | Izin | Body |
| --- | --- | --- |
| `POST /api/admin/auth/login` | publik, rate-limited | `{email, password}` → `{data:{email, role, expires_at}}` + cookie |
| `GET /api/admin/auth/session` | sesi | `{data:{email, role, expires_at}}` / `401` |
| `POST /api/admin/auth/logout` | sesi | `204`, sesi dicabut |
| `POST /api/admin/auth/password` | sesi | `{current_password, new_password}` → `204` |
| `GET /api/admin/packages` | semua peran | daftar termasuk nonaktif, dengan `version` |
| `POST /api/admin/packages` | owner | `{code, name, price_idr, duration_minutes, rate_limit}` → `201` |
| `PUT /api/admin/packages/{id}` | owner | `{name, price_idr, duration_minutes, rate_limit, version}` → `200` / `409` |
| `POST /api/admin/packages/{id}/active` | owner | `{active: bool}` → `200` / `409` (Package aktif terakhir) |
| `GET /api/admin/transactions` | semua peran | filter + paginasi, tanpa PIN |
| `POST /api/admin/transactions/{id}/pin` | owner, support | `{data:{pin}}`, diaudit |
| `GET /api/admin/subscribers` | semua peran | filter + paginasi |
| `GET /api/admin/subscribers/{phone_number}/verify` | semua peran | `{data:{app_expiry, freeradius_expiry, match}}` |
| `GET /api/admin/audit` | owner, auditor | filter + paginasi |
| `GET /api/admin/admins` | owner | daftar admin |
| `POST /api/admin/admins` | owner | `{email, role, password}` → `201` |
| `POST /api/admin/admins/{id}/password` | owner | `{password}` → `204`, sesi admin itu dicabut |
| `POST /api/admin/admins/{id}/active` | owner | `{active: bool}` → `200`, sesi dicabut bila nonaktif |

- Semua galat memakai envelope yang sudah ada: `{"error":{"code","message"}}` dengan `401`/`403`/`404`/`409`/`422`/`429`.
- `403` dipakai untuk peran yang tidak berhak; `401` hanya untuk sesi tidak ada/kedaluwarsa.

### Aplikasi `admin/` (ADR 0028)

- SvelteKit terpisah (`admin/`), stack sama dengan aplikasi pembelian dan mengikuti skill `frontend-ddd-sveltekit`: `lib/domains/{auth,packages,transactions,subscribers,audit,admins}/` dengan `index.ts` sebagai satu-satunya permukaan keluar.
- SvelteKit menyajikan panel di bawah base path `/admin` (`kit.paths.base`), dan proxy meneruskan `/admin` apa adanya dengan fallback ke `admin/index.html`. Aset panel harus memakai `base` itu, atau shell-nya akan meminta `/@vite/...` di root dan diambil alih aplikasi pembelian.
- Route: `/admin` (shell + login), `/admin/paket`, `/admin/transaksi`, `/admin/pengguna`, `/admin/audit`, `/admin/admin`. Desktop-first; standar 360px tetap milik aplikasi pembelian.
- Tidak ada SSR (`ssr = false`), sesi hanya di cookie; UI menyembunyikan aksi yang tidak diizinkan peran, tetapi penegakannya di backend.
- Sesi habis (401) → kembali ke form login dengan pesan, bukan halaman kosong.
- Image + service compose `admin`, proxy merutekan `/admin` dengan fallback SPA dan `Cache-Control: no-store` untuk shell; CSP sendiri.

### Kontrak dan fixture

- Fixture baru untuk endpoint admin masuk `contracts/` dan dipakai bersama oleh tes integrasi Go dan skema zod panel, seperti fixture yang sudah ada.

### Konfigurasi

- Kunci baru (empat tempat, atau `test/integration/deployment_test.go` gagal): `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_SESSION_TTL`, `ADMIN_COOKIE_NAME`, `ADMIN_COOKIE_SECURE`, `ADMIN_RATE_LIMIT_IP_WINDOW/LIMIT`, `ADMIN_RATE_LIMIT_EMAIL_WINDOW/LIMIT`.

## Slice

### Slice 1 — Panel bisa dipakai, katalog bisa diubah tanpa SQL

1. Domain `admin`: entity/repo/service akun, sesi, audit; port `AuditRecorder`; middleware sesi + peran.
2. `migrate` men-seed owner pertama (gagal keras bila kosong & env kosong) + kolom `is_active`/`version` di `packages`, kolom snapshot di `transactions`, backfill baris pending.
3. Endpoint auth admin: login, logout, session, ubah password; rate limit; cookie terpisah.
4. `packages.Service` tulis (`CreatePackage`, `UpdatePackage`, `SetActive`) + validasi + `version` + audit; katalog publik menyaring `is_active`.
5. `provisioning` membaca snapshot Transaction, bukan katalog.
6. Panel `admin/`: login, daftar + form Package (termasuk nonaktifkan/aktifkan), kelola admin.
7. Compose: service + image `admin`, route `/admin` di proxy, CI job, target Makefile, fixture `contracts/` untuk endpoint slice ini.

### Slice 2 — Daya lihat untuk menjawab pembeli

1. `transaction.Service.ListTransactions` + halaman daftar Transaction dengan filter.
2. `POST /api/admin/transactions/{id}/pin` + tombol buka PIN di halaman detail (owner/support) + audit.
3. `transaction.Service.ListSubscribers` + halaman pengguna aktif + verifikasi per-baris ke FreeRADIUS.
4. Halaman audit log (owner/auditor) dengan filter.
5. Fixture `contracts/` untuk endpoint slice ini.

Alasan urutan: slice 1 menutup cacat yang ada hari ini (SQL manual, Package yang dihidupkan kembali seed) **dan** celah yang ia ciptakan sendiri (drift durasi/rate limit), sehingga tidak pernah ada rilis yang membuka jalur edit katalog tanpa snapshot. Slice 2 hanya menambah daya lihat dan tidak mengubah kontrak yang sudah dibekukan.

## Testing Decisions

- **Auth admin:** login benar/salah, email tak dikenal vs password salah menghasilkan respons identik, rate limit per IP dan per email, sesi kedaluwarsa, logout mencabut sesi, reset password & penonaktifan mencabut sesi, cookie tidak diterima di route pembeli dan sebaliknya.
- **Peran:** satu tes per sel matriks izin, dijalankan pada batas HTTP (bukan unit), supaya `403` benar-benar berasal dari server.
- **Katalog:** validasi tiap field (batas atas dan bawah), `code` imutabel, `version` mismatch → `409`, Package aktif terakhir tak bisa dinonaktifkan, Package nonaktif hilang dari `GET /api/packages` tetapi ada di daftar admin, seed tidak menghidupkan/menimpa apa pun.
- **Snapshot:** checkout → edit katalog → provisioning memakai nilai yang dibeli (durasi, rate limit), dibuktikan dengan tes yang mengubah katalog di antara dua langkah; backfill baris pending diuji dengan baris tanpa kolom.
- **Audit:** setiap aksi tulis menghasilkan tepat satu baris; perubahan yang gagal tidak menghasilkan baris; tidak ada endpoint yang bisa mengubah/menghapus baris.
- **PIN:** hanya owner/support, selalu diaudit, PIN tidak pernah muncul di daftar, kegagalan dekripsi tidak membocorkan detail, dan PIN tidak masuk ke log.
- **Pengguna aktif:** agregat `MAX(provision_target_expiration)` per Phone Number dengan Extend berturut-turut, klasifikasi `active/expiring/expired` memakai ambang ADR 0017, verifikasi cocok/tidak cocok terhadap FreeRADIUS asli.
- **Frontend panel:** tes skema zod per fixture `contracts/`, tes komponen untuk form Package (galat validasi per field, konflik `409`), dan satu alur Playwright: login → ubah harga → harga baru tampil di `GET /api/packages`.

## Out of Scope

1. **Hard delete Package** — panel hanya menonaktifkan.
2. **Refund, pembatalan Transaction, atau provisioning manual dari panel.**
3. **Kirim ulang PIN/SMS dari panel.**
4. **Mengedit data pembeli** (Phone Number, PIN, expiry, rate limit akun) langsung dari panel.
5. **Multi-hotspot / multi-tenant** dalam satu deployment.
6. **MFA/SSO/API key** untuk admin.
7. **Laporan/ekspor** (CSV, grafik pendapatan, analitik) dan **retensi/rotasi audit log**.
8. **Panel dioptimalkan untuk ponsel** — panel desktop-first.
9. **Mengubah kontrak publik `GET /packages`** selain menyaring `is_active`.
10. **Admin masuk lewat halaman RouterOS/Login Page.**

## Further Notes

- **ADR yang lahir dari PRD ini:** 0024 (identitas, sesi, peran admin + bootstrap), 0025 (katalog: pensiunkan, jangan hapus), 0026 (Transaction menyimpan istilah katalog), 0027 (audit append-only dan transaksional), 0028 (panel sebagai aplikasi terpisah).
- **Risiko yang perlu dipantau:** panel adalah permukaan baru yang bisa mengubah harga dan membuka PIN — mitigasinya peran + audit + rate limit, bukan kepercayaan pada UI. Verifikasi FreeRADIUS per-baris bisa lambat bila MySQL sedang sakit; kegagalannya harus tampil sebagai "tidak dapat diverifikasi", bukan sebagai "tidak aktif".
- **Glosarium mengikat:** **Admin Panel** (bukan Backoffice/Dashboard), **Package**, **Active Period**, **Phone Number**, **PIN**, **Transaction**, **Extend**. Peran ditulis `owner`/`support`/`auditor`.
