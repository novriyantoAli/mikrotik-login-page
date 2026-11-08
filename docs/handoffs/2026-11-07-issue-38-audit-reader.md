---
judul: Handoff — Issue #38 Pembaca audit log untuk owner dan auditor
tanggal: 2026-11-07
posisi_main: ae2e573
untuk: sesi baru yang mengerjakan #38
---

# Handoff — #38 Pembaca audit log untuk owner dan auditor

> **Catatan kerja, bukan dokumen produk.** Hapus file ini setelah #38 merge; isi
> yang layak hidup lama harus menjadi ADR atau PRD.

## Posisi sekarang

`main` = `ae2e573` (#35, daftar Transaction). Sisa slice 2 PRD Admin Panel: **#37
(pengguna aktif)** dan **#38 (audit reader)**.

**#36 sudah selesai sebagai kode, belum di `main`:** branch
`feat/36-pin-reveal`, commit `33a38a6`, PR **#49** masih OPEN (CI pending). Ia
menulis baris `pin.reveal` yang justru menjadi bahan uji #38 — lihat bagian
"Yang #36 tinggalkan".

Branch untuk #38:

- kalau PR #49 sudah merge → cabang dari `main`;
- kalau belum → cabang dari `feat/36-pin-reveal`, lalu rebase setelah #49 merge.
  **Jangan** menyalin file dari branch itu dengan tangan.

`#38` dulu diblokir #32 (katalog ber-audit), dan #32 sudah di `main`. Jadi #38
tidak punya blocker lain: kerjakan di sesi baru, lewat `/implement` dengan
`docs/prd/admin-panel.md` + issue #38. **Jangan** triage — issue ini lahir dari
`/to-issues`, sudah agent-ready.

## Yang sudah ada — pakai, jangan tulis ulang

| Sudah ada | Lokasi |
| --- | --- |
| Tabel `admin_audit_log` + `entity.AuditEntry` (`payload` JSONB, `admin_id`+`admin_email` disalin, `ip`) | `backend/internal/application/admin/entity/admin.entity.go` |
| `entity.Payload` (`map[string]any`) dengan `Value`/`Scan` — bentuk yang harus dibaca | idem (`Payload`, baris 82) |
| Kosakata aksi/target + `Entry`/`Actor` | `backend/internal/pkg/audit` |
| Adaptor tulis (`Record(ctx, tx, entry)`) | `backend/internal/application/admin/repository/audit.go` |
| Matriks peran baris "read the audit log" (owner+auditor) | `backend/internal/middleware/admin_test.go` |
| `AdminGuard.Require(roles...)` + `middleware.AdminActorFrom(c)` | `backend/internal/middleware/admin.go` |
| Precedent route read-only per peran di panel | `admin/handler/transaction.handler.go` |
| Precedent filter + params milik repository + `FilterError` → 422 | `transaction/service/panel.go`, `transaction/repository/transaction.repo.go` (`PanelListParams`) |
| Precedent budget per admin (kalau nanti perlu) | `backend/internal/middleware/admin_rate_limit.go` (#36) |
| Precedent domain panel (`schemas/api/queries/state/components/index.ts`) | `admin/src/lib/domains/{auth,packages,transactions,admins}` |
| Pola fixture dua sisi | `contracts/` + `test/integration/*_contract_test.go` + `admin/tests/unit/*.contract.test.ts` |

## Yang harus dibangun #38

1. **`admin/repository`**: `ListAudit(ctx, params AuditListParams)` — params milik
   repository (jangan `dto.*`), filter admin (email atau id), `action`, rentang
   tanggal setengah-terbuka (pola `panelWindowOf`: hari WIB, `To` = awal hari
   berikutnya), `page`/`page_size` (default 20, maks 100). Urut
   `created_at DESC, id DESC` — id sebagai tie-break supaya paginasi tidak
   menggeser baris yang `created_at`-nya sama.
2. **`admin/service`**: `ListAudit(ctx, filter *dto.AdminAuditFilter)` →
   `*dto.AdminAuditPage`. Validasi `action` terhadap kosakata `audit.Action`
   (satu daftar, jangan menyalin string); nilai tak dikenal → sentinel + `*FilterError`
   bergaya `transaction/service/panel.go`, dijawab 422 oleh handler.
3. **DTO**: `AdminAuditFilter{AdminEmail, Action, From, To, Page, PageSize}`,
   `AdminAuditEntryResponse{id, created_at, admin_email, action, target_type,
   target_id, payload (objek bebas), ip}`, `AdminAuditPage{Items, TotalCount,
   Page, PageSize}`. Handler merender lewat `httpx.Paged`.
4. **Handler + route**: `GET /api/admin/audit` di
   `admin/handler/audit.handler.go`, `guard.Require(RoleOwner, RoleAuditor)`,
   anotasi swagger lengkap (`@Tags admin-audit`, `@Param` per filter).
5. **Panel**: `admin/src/lib/domains/audit/` lengkap dengan `index.ts`; route
   `/audit` di `admin/src/routes/audit/+page.svelte` (tipis); link nav di
   `AppShell` hanya untuk owner/auditor (pola yang sudah ada untuk `/admin`);
   tabel + filter + baris payload yang bisa dibuka; loading/error/empty.
6. **Fixture** `contracts/admin-audit.response.json` + paragraf di
   `contracts/README.md`, dipakai tes integrasi Go (`DisallowUnknownFields` +
   round-trip bentuk) dan zod strict di panel.
7. **`test/integration/isolation_test.go`**: tambahkan `"GET /api/admin/audit"`
   ke `adminRoutes`. Guard tabel itu gagal kalau route admin baru lupa didaftar
   (pelajaran dari #36).

## Jebakan yang sudah kelihatan

1. **`target_id` boleh menunjuk baris yang tidak ada.** #36 mengaudit percobaan
   membuka PIN Transaction yang tidak eksis sebagai `pin.reveal` dengan
   `payload.result = "not_found"` (tanpa `reference`). Reader harus menampilkan
   baris itu apa adanya — jangan `JOIN` yang membuangnya, dan jangan menganggap
   payload selalu punya `reference`.
2. **Payload heterogen, satu kolom.** Bentuk yang sudah ada di produksi:
   - `package.create` → `{"after": {code,name,price_idr,duration_minutes,rate_limit,is_active,version}}`
   - `package.update` → field yang berubah saja, `{"price_idr": {"before","after"}, "version": {...}}`
   - `package.activate|deactivate` → `{"is_active": {"before","after"}, "version": {...}}`
   - `admin.create|update` → datar: `{"email","role","is_active"}`
   - `admin.password_reset` → `{"email"}`; `admin.deactivate` → `{"email","is_active"}`
   - `pin.reveal` → `{"result","reference"}`

   Render generik (key → nilai, `before → after` kalau bentuknya begitu) plus
   satu keistimewaan: **tampilkan before/after untuk aksi katalog** (AC #38).
   Jangan bikin schema zod per aksi yang strict — yang strict cukup envelope dan
   kolomnya; payload pakai `z.record(z.string(), z.unknown())`.
3. **`admin_email` disalin, jangan di-join.** Baris tetap terbaca setelah admin
   dinonaktifkan atau dihapus; filter admin cukup `ILIKE`/exact pada kolom
   `admin_email` itu, bukan lewat tabel `admins`.
4. **Tidak ada jalur ubah/hapus**, termasuk di UI: jangan menambah tombol
   "bersihkan", "hapus", atau endpoint `DELETE`/`PUT`. AC #38 meminta uji bahwa
   jalur itu tidak ada.
5. **Baris `pin.reveal` tidak boleh berubah menjadi tempat PIN.** Payload-nya
   memang tidak memuat digit; uji tetap harus memeriksa respons tidak memuat
   plaintext (bandingkan dengan PIN yang disegel di uji, pola
   `admin_pin_test.go`).
6. **Tanggal = hari WIB**, sama dengan daftar Transaction. Pakai
   `panelDayLocation`/`time.ParseInLocation(time.DateOnly, …)`; jangan `time.Parse`
   UTC, atau filter dan kolom waktu akan berbeda tujuh jam.
7. **Jangan menambah kunci config** untuk #38. Role + paginasi sudah cukup;
   `deployment_test.go` menuntut setiap kunci baru muncul di empat tempat
   (config+default, `config.sample.yaml`, `.env.example`, `docker-compose.yml`).
8. **Rate limit tidak perlu**: route ini read-only dan role-gated. Kalau nanti
   terbukti perlu, pola `PINRevealLimiter` (#36) sudah ada — tapi jangan
   menambahkannya tanpa alasan yang tertulis di PR.

## Uji yang harus ada

- Batas HTTP: `support` → 403; `owner` dan `auditor` → 200; anonim → 401.
- Filter admin, aksi, dan rentang tanggal benar-benar menyaring; nilai tak
  dikenal → 422 (bukan halaman kosong).
- Baris `pin.reveal` tampil dengan target, dan tidak ada plaintext PIN di
  seluruh respons.
- Perubahan katalog menampilkan nilai sebelum/sesudah; `package.create` tidak
  punya "before" dan reader tidak mengarang satu.
- Paginasi stabil di `created_at` yang sama (dua baris satu transaksi).
- Panel: loading, error, empty; filter; baris payload terbuka; tes fixture.

## Definition of Done

```bash
make -C backend test && make -C backend lint && make -C backend build && make -C backend swagger-gen
cd admin && npm run check && npm run lint && npm test && npm run build
```

Status `README.md` (root) harus diperbarui: setelah #38, sisa slice 2 hanya #37.
`admin/README.md` mendapat baris route `/admin/audit` dan domain `audit`.

## Alur PR

Ikuti #48/#49: branch `feat/38-audit-reader`, satu commit dengan subjek
`feat(admin): … (#38)`, body PR bergaya repo (`Closes #38 — …`, `## Yang
berubah`, `## Acceptance criteria`, `## Verifikasi lokal`, `## Catatan`).
Jalankan review dua sumbu (Standards + Spec) **sebelum** commit, bukan sesudah —
itu urutan yang pernah terbalik di #30.

**Catatan lingkungan:** tidak ada Docker di mesin pengembang, jadi
`docker-compose` dan job `e2e-compose` tidak bisa dijalankan lokal; katakan itu
apa adanya di PR, jangan mengklaim lebih.
