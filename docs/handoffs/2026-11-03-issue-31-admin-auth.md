---
judul: Handoff — Issue #31 Auth admin, sesi, cookie, dan matriks peran
tanggal: 2026-11-03
posisi_main: f533281
untuk: sesi baru yang mengerjakan #31
---

# Handoff — #31 Auth admin, sesi, cookie, dan matriks peran

> **Catatan kerja, bukan dokumen produk.** Hapus file ini setelah #31 merge; isi
> yang layak hidup lama harus menjadi ADR atau PRD.

## Posisi sekarang

`main` = `f533281` (seed owner + `is_active`/`version`, #29). PRD Admin Panel
terbagi dua slice; issue terbuka: **#31–#38**.

**#30 sudah selesai sebagai kode, belum di `main`:** branch
`feat/30-audit-trail-same-transaction`, commit `a68d264`, PR **#41** masih OPEN.
Ia membawa port audit + jalur tulis `CreateAdmin` (lihat bagian berikut).

Branch untuk #31:

- kalau PR #41 sudah merge → cabang dari `main`;
- kalau belum → cabang dari `feat/30-audit-trail-same-transaction`, lalu rebase
  setelah #41 merge. **Jangan** menyalin file dari branch itu dengan tangan.

## Yang sudah ada dari #30 — perluas, jangan tulis ulang

| Sudah ada | Lokasi |
| --- | --- |
| `CreateAdmin(ctx, actor, *dto.CreateAdminRequest) (*dto.AdminResponse, error)` | `admin/service/admin.service.go` |
| Validasi peran, email kanonik, password ≥ 12 & ≠ email, bcrypt cost 12 (`PasswordCost`) | idem; sentinel `ErrRoleInvalid`, `ErrEmailInvalid`, `ErrPasswordWeak`, `ErrActorRequired`, `ErrEmailTaken` |
| `CreateAdmin` repository: insert + baris audit dalam satu transaksi | `admin/repository/admin.repo.go` |
| Adaptor audit (`Record(ctx, database.Tx, audit.Entry)`, menolak handle non-transaksi) | `admin/repository/audit.go` |
| Kosakata jejak: `Action` (termasuk `admin.update`/`password_reset`/`deactivate`), `TargetType`, `Actor`, `Entry` | `internal/pkg/audit` |
| Port audit di domain pemakai + binding di composition root | `packages/repository/audit.go`, `transaction/repository/audit.go`, `internal/app/audit.go` |

Dua port di tabel terakhir **belum punya pemanggil produksi** dan itu keputusan
sadar (AC #30; pemakainya #32/#36). Jangan menghapusnya saat menyentuh
`internal/app/audit.go`.

## Yang harus dibangun #31

1. **Repository sesi:** `CreateSession`, `Session(tokenHash, now)` (hanya yang
   belum kedaluwarsa & belum dicabut), `RevokeSession`, `RevokeAllForAdmin`.
2. **Service:** `Login`, `Logout`, `Session`, `ChangeOwnPassword`, `ListAdmins`,
   `CreateAdmin` (sudah), `ResetPassword`, `SetActive`.
3. **Cookie:** `HttpOnly`, `Secure` dari `ADMIN_COOKIE_SECURE`, `SameSite=Strict`,
   `Path=/api/admin`, nama `ADMIN_COOKIE_NAME`, TTL absolut `ADMIN_SESSION_TTL`
   tanpa perpanjangan.
4. **Middleware sesi + peran** untuk seluruh `/api/admin/**`, ditegakkan per route.
5. **Handler + route + anotasi swagger** untuk delapan endpoint auth/admin.
6. **Rate limit login per IP dan per email** (dua kunci terpisah, `429`).
7. **Uji HTTP:** satu per sel matriks ADR 0024 §3, isolasi cookie dua arah,
   respons login seragam, TTL kedaluwarsa, pencabutan sesi langsung `401`.

## Seam yang dipakai — jangan bikin yang baru

| Butuh | Pakai ini |
| --- | --- |
| Token sesi | `internal/pkg/securetoken` (`New()` → raw + hash SHA-256, `Hash()`) |
| Envelope + kode galat | `internal/pkg/httpx` (`Data`, `Error`, `CodeUnauthorized`, `CodeForbidden`, `CodeRateLimited`, `Internal`) |
| Rate limit | `ratelimit.Limiter.Allow(ctx, key, limit, window)` + `AdminRateLimitConfig` |
| Config | `cfg.Admin.*` — **sembilan kunci sudah ada sejak #28**, tidak perlu kunci baru |
| Waktu | `clock.Clock` yang disuntik (bandingkan `expires_at` dengannya, bukan `time.Now()`) |
| Route | `RouteRegistrar` + group `api_routes` di `internal/server/api` |
| Tes integrasi | `testutil.NewAPIHarness(t)`; skema tes sudah punya owner `owner@example.test` / `kata-sandi-yang-cukup-panjang` (`testutil/admin.go`) |
| Pola sesi pembeli | `auth/service` (`ExchangeMagicLink`, `Authenticate`) dan `middleware/session.go` sebagai **contoh bentuk**, bukan untuk dipakai ulang |

## Jebakan yang sudah kelihatan dari sini

1. **Cookie admin tidak boleh memakai helper pembeli apa adanya.**
   `httpx.SetSessionCookie`/`ClearSessionCookie` mengunci `Path=/` dan
   `SameSite=Lax`. Admin butuh `Path=/api/admin` + `SameSite=Strict`. Tambahkan
   helper khusus admin (atau perluas dengan settings), dan pastikan `Clear`
   memakai atribut yang sama — kalau tidak, cookie "hilang" tapi tetap dikirim.
2. **Guard admin jangan memakai kunci konteks pembeli.** `middleware.StashSession`
   menyimpan di kunci `"session"`; memakai kunci yang sama membuat satu bug kecil
   bisa membuat sesi pembeli terbaca sebagai sesi admin. Pakai kunci dan tipe
   sendiri.
3. **Respons login seragam.** Email tak dikenal dan password salah harus
   menghasilkan status + body identik, termasuk saat rate limit tidak aktif.
4. **Rate limit dua dimensi.** Kunci IP dan kunci email terpisah; error Redis
   tidak boleh dianggap "lolos" (limiter memang mengembalikan error, jangan
   ditelan).
5. **Pencabutan sesi saat reset password dan penonaktifan harus satu transaksi
   dengan baris audit** (`admin.password_reset`, `admin.deactivate`). Kalau
   terpisah, jejaknya berhenti bisa dipercaya.
6. **Aktor datang dari sesi, bukan body.** `CreateAdmin` sudah menolak aktor
   kosong (`ErrActorRequired`); pertahankan pola itu di setiap aksi baru.
7. **`admin_sessions` terpisah dari `sessions`** (ADR 0024 §2). Jangan menambah
   kolom `kind` pada tabel pembeli, sekecil apa pun terlihat lebih murah.
8. **Swagger ikut CI.** `make -C backend swagger-gen` dijalankan CI; anotasi yang
   menamai alias DTO pernah membuatnya merah (commit `2eb6d72`). Jalankan
   targetnya dan commit perubahan `docs/api`.
9. **Tidak perlu kunci config baru.** Kalau ternyata perlu, ia wajib lahir di
   empat tempat sekaligus (`config.go`, `config.sample.yaml`, `.env.example`,
   compose) atau `test/integration/deployment_test.go` merah.

## Urutan commit yang disarankan (satu slice TDD per commit)

1. Repository sesi + tes integrasi (simpan hash, lookup kedaluwarsa/dicabut,
   revoke-all).
2. Service login/logout/session/ubah-password + unit test dengan repo palsu
   (respons seragam, validasi password, aktor).
3. Middleware sesi + matriks peran (unit, plus satu tes HTTP per sel).
4. Helper cookie admin + handler auth + swagger.
5. Endpoint kelola admin (`GET/POST /admins`, reset password, aktif/nonaktif) —
   `CreateAdmin` sudah ada, yang baru adalah daftar, reset, dan nonaktif, dengan
   pencabutan sesi di transaksi yang sama dengan audit.
6. Isolasi cookie dua arah + `429` + TTL.

## Definition of Done (#31 + §11)

- [ ] Delapan sel matriks punya tes HTTP; `403` untuk peran yang tidak berhak,
      `401` hanya untuk sesi tidak ada/kedaluwarsa.
- [ ] Cookie admin tidak diterima di route pembeli, cookie pembeli tidak diterima
      di `/api/admin/**` — keduanya diuji di batas HTTP.
- [ ] Reset password & penonaktifan mencabut sesi (token lama langsung `401`).
- [ ] Password < 12 karakter atau sama dengan email ditolak.
- [ ] Login: email tak dikenal vs password salah identik; rate limit per IP dan
      per email menghasilkan `429`.
- [ ] `make -C backend build && make -C backend test && make -C backend lint &&
      make -C backend format-strict` hijau; `swagger-gen` bersih.
- [ ] **`/code-review` dua sumbu dijalankan sebelum commit**, bukan sesudah —
      urutan itu terbalik di #30 dan harus diperbaiki di sini.

## Jangan

- **Jangan triage #31–#38.** Itu issue hasil `/to-issues`, sudah agent-ready.
- Jangan menulis ulang `CreateAdmin`/`dto`/sentinel yang sudah ada (#30).
- Jangan mencampur #31 dan #32 dalam satu worktree. Keduanya boleh paralel, satu
  writer per direktori.
- Jangan menyentuh `Login Page`, aplikasi pembelian, atau kontrak `GET /packages`
  — ketiganya beku sampai #32.

## Setelah merge

Tutup #31, hapus file ini, perbarui paragraf status di `README.md`, lalu lanjut
#32 (tulis katalog) atau #33 (panel) sesuai urutan di PRD.
