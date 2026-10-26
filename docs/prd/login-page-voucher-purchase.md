---
status: delivered
judul: Integrasi Login Page dengan Alur Pembelian Voucher
pelaku: Operator hotspot, Pengguna hotspot, Admin
---

# PRD — Integrasi Login Page dengan Alur Pembelian Voucher

> **Status triase:** `delivered` (slice #2–#9 merge)
> **Konteks:** Lihat `CONTEXT.md` (glosarium domain) dan `docs/adr/` (ADR 0001–0004).

## Problem Statement

Saat ini **Login Page** hotspot (MikroTik captive portal) menampilkan daftar **Package** yang ditulis manual di dalam konfigurasi halaman. Tombol "Beli" pada tiap Package mengarah ke URL placeholder yang tidak terhubung dengan sistem penjualan, dan alur login masih memakai skema voucher/member lama (salah satunya menyamakan kata sandi dengan nama pengguna) alih-alih **Phone Number + PIN** sebagaimana ditetapkan di ADR 0004.

Akibatnya, dari sudut pandang pengguna:

- Pengguna tidak bisa membeli **Active Period** langsung dari alur hotspot — mereka harus menghubungi reseller di luar sistem.
- Daftar **Package** cepat usang (harga/durasi/kecepatan berubah di **Admin Panel**, tetapi tidak tercermin di **Login Page**).
- Pengguna bingung harus mengetik apa saat login — apakah kode voucher, nama anggota, atau **Phone Number** dan **PIN**.
- Operator tidak bisa mengubah penawaran tanpa menyunting berkas halaman secara manual.

## Solution

**Login Page** menjadi pintu masuk alur pembelian voucher:

1. **Login Page** mengambil daftar **Package** secara dinamis dari backend (bukan lagi daftar manual), lalu menampilkan nama, harga, durasi (**Active Period**), dan batas kecepatan.
2. Setiap **Package** memiliki tombol "Beli" yang mengarahkan pengguna ke aplikasi web (Svelte) untuk membeli **Active Period**, dengan **Package** yang dipilih sudah terpilih dan konteks kembali ke **Login Page** yang sama.
3. Setelah pembelian selesai, pengguna kembali ke **Login Page** dan masuk ke hotspot menggunakan **Phone Number** + **PIN** (sesuai ADR 0004), dengan PIN di-hash CHAP sebelum dikirim.
4. Halaman status menunjukkan sisa **Active Period**, dan mengarahkan pengguna untuk **Extend** atau membeli ulang saat masa aktif habis.

Perubahan dibatasi pada sisi **Login Page**; backend, aplikasi web, dan provisioning FreeRADIUS tetap berada di luar repo ini (lihat *Out of Scope*).

## User Stories

1. Sebagai pengguna hotspot yang belum pernah membeli, saya ingin melihat daftar **Package** yang tersedia di **Login Page**, sehingga saya bisa memilih **Active Period** yang sesuai kebutuhan.
2. Sebagai calon pelanggan, saya ingin setiap **Package** menampilkan nama, harga, durasi (**Active Period**), dan batas kecepatan, sehingga saya bisa membandingkan sebelum membeli.
3. Sebagai pengguna, saya ingin daftar **Package** selalu mencerminkan penawaran terbaru yang dikelola admin, sehingga saya tidak melihat **Package** yang sudah dihapus atau kedaluwarsa.
4. Sebagai pengguna, saya ingin melihat indikator sedang memuat saat daftar **Package** diambil, sehingga saya tahu halaman sedang bekerja.
5. Sebagai pengguna, saya ingin melihat pesan kesalahan yang jelas ketika daftar **Package** gagal dimuat, sehingga saya tahu harus mencoba lagi atau menghubungi admin.
6. Sebagai pengguna, ketika tidak ada **Package** yang tersedia, saya ingin melihat keadaan kosong yang informatif, sehingga saya paham bahwa saat ini tidak ada yang bisa dibeli.
7. Sebagai pengguna, saya ingin tombol "Beli" pada setiap **Package**, sehingga saya bisa mulai membeli **Package** tersebut.
8. Sebagai pengguna, saya ingin menekan "Beli" membawa saya ke aplikasi web dengan **Package** yang saya pilih sudah terpilih, sehingga saya tidak perlu memilih ulang di sana.
9. Sebagai pengguna, saya ingin dikembalikan ke **Login Page** yang sama setelah menyelesaikan pembelian, sehingga saya bisa langsung masuk ke hotspot.
10. Sebagai pengguna, saya ingin konteks hotspot asal saya dipertahankan selama proses beli, sehingga setelah pembelian saya kembali ke hotspot yang benar.
11. Sebagai pengguna perangkat seluler, saya ingin daftar **Package** dan tombol-tombolnya responsif serta mudah disentuh, sehingga saya bisa membeli dengan nyaman dari ponsel.
12. Sebagai pengguna yang baru selesai membeli, saya ingin masuk ke hotspot menggunakan **Phone Number** dan **PIN** yang saya terima via SMS, sehingga saya bisa langsung online.
13. Sebagai pengguna, saya ingin **Login Page** dengan jelas meminta **Phone Number** dan **PIN** (bukan kode voucher generik), sehingga saya tahu persis apa yang harus diketik.
14. Sebagai pengguna, saya ingin kolom **PIN** disembunyikan secara bawaan dengan tombol tunjuk/sembunyi, sehingga saya bisa menjaga kerahasiaan **PIN** saya.
15. Sebagai pengguna, saya ingin pesan kesalahan yang jelas saat **Phone Number** atau **PIN** salah, sehingga saya bisa memperbaikinya.
16. Sebagai pengguna, saya ingin pesan kesalahan membedakan "akun kedaluwarsa" dari "PIN salah", sehingga saya tahu apakah harus membeli lagi atau cukup mengetik ulang.
17. Sebagai pengguna, saya ingin **Phone Number** saya terisi otomatis pada kunjungan berikutnya jika memungkinkan, sehingga saya tidak perlu mengetik ulang.
18. Sebagai pengguna yang sudah online, saya ingin melihat sisa **Active Period** di halaman status, sehingga saya tahu kapan harus **Extend**.
19. Sebagai pengguna yang **Active Period**-nya hampir habis, saya ingin indikasi yang jelas, sehingga saya bisa **Extend** sebelum kehilangan akses.
20. Sebagai pengguna yang **Active Period**-nya sudah habis, saya ingin diarahkan kembali ke daftar **Package** untuk membeli lagi, sehingga saya bisa segera online kembali.
21. Sebagai pengguna yang sudah memiliki **Active Period**, saya ingin **Extend** dengan membeli **Package** lain saat masa aktif masih berjalan, sehingga sisa waktu saya ditambahkan, bukan hilang.
22. Sebagai admin, saya ingin mengelola **Package** (nama, harga, durasi, batas kecepatan) di **Admin Panel**, sehingga **Login Page** selalu menampilkan penawaran terkini.
23. Sebagai admin, saya ingin perubahan **Package** langsung tercermin di **Login Page** tanpa menyunting berkas halaman, sehingga saya tidak perlu menyentuh kode.
24. Sebagai operator, saya ingin **Login Page** tetap berfungsi untuk login walaupun backend tidak dapat dijangkau (menampilkan kesalahan untuk daftar **Package**, tetapi login tetap jalan), sehingga gangguan backend tidak memblokir login hotspot sepenuhnya.
25. Sebagai pengguna, saya ingin proses masuk tetap memakai mekanisme CHAP hotspot yang sama, sehingga pengalaman masuk tidak berubah bagi perangkat yang sudah terbiasa.

## Implementation Decisions

1. **Satu seam baru — modul klien voucher.** Seluruh fitur diimplementasikan sebagai satu modul sisi-klien yang menjadi satu-satunya titik uji. Modul ini mengekspos perilaku publik: (a) mengambil daftar **Package**, (b) merender kartu **Package** beserta tombol "Beli", (c) membangun URL pengalihan pembelian, dan (d) melakukan login **Phone Number** + **PIN** (termasuk hashing CHAP). Tidak ada seam baru lain di halaman.

2. **Sumber data Package dari backend.** Daftar **Package** yang tadinya ditulis manual diganti dengan pengambilan dari endpoint publik backend (mis. `GET /packages`). **Login Page** hanya membaca dan merender; ia tidak pernah menulis. Bentuk respons minimal: larik objek `{ id, nama, harga, durasiActivePeriod, batasKecepatan }`. Kontrak ini adalah antarmuka yang menjadi batas uji (di-mock pada pengujian).

3. **Pengalihan pembelian.** Tombol "Beli" membangun URL ke aplikasi web (Svelte) dengan menyertakan identitas **Package** yang dipilih dan target kembali ke **Login Page**. Autentikasi aplikasi web memakai **Magic Link**, pembayaran, dan provisioning dijalankan di sisi aplikasi web/backend (di luar repo ini) sesuai ADR 0001–0003. **Login Page** hanya membangun tautan dan menavigasi.

4. **Kredensial login sesuai ADR 0004.** Username = **Phone Number**, password = **PIN** 6 digit. Perilaku lama yang menyamakan kata sandi dengan nama pengguna dihapus untuk mode voucher; **PIN** selalu di-hash CHAP sebelum dikirim. Mode login "member" lama digantikan oleh **Phone Number** + **PIN**.

5. **Alur trial dipertahankan.** Login trial (berbasis alamat perangkat) tetap ada sebagai opsi yang dikendalikan admin dan tidak terpengaruh oleh perubahan **Phone Number** + **PIN**.

6. **Keadaan yang ditangani.** Modul menangani: sedang memuat, sukses, kosong (tanpa **Package**), gagal (backend tidak terjangkau), dan kedaluwarsa sesi/akun. Pesan kedaluwarsa dipetakan dari variabel expiry RouterOS dan nilai kesalahan hotspot menjadi pesan yang bisa dipahami manusia, dengan membedakan "PIN salah" dan "akun kedaluwarsa".

7. **Konfigurasi terpusat.** URL basis backend dan URL aplikasi web pembelian dapat dikonfigurasi (per deployment), dan tersedia saklar untuk mengaktifkan/menonaktifkan daftar **Package** dari backend dengan fallback ke konfigurasi statis untuk degradasi yang anggun.

## Testing Decisions

- **Prinsip pengujian:** hanya perilaku eksternal yang diuji (apa yang dilihat/dilakukan pengguna), bukan detail implementasi internal. Uji antarmuka publik modul: hasil fetch → render, URL pengalihan yang dibangun, dan hashing login; jangan uji struktur DOM internal.
- **Modul yang diuji:** modul klien voucher (seam tunggal). Pengujian berfokus pada (a) fetch daftar **Package** dan render, (b) konstruksi URL "Beli" dengan **Package** dan target kembali yang benar, (c) login **Phone Number** + **PIN** dengan hashing CHAP, dan (d) keadaan kosong/gagal.
- **Cara mock:** backend di-mock pada batas fetch; tidak menguji internals RouterOS/CHAP di luar keluaran hash yang diharapkan.
- **Prior art:** tidak ada pengujian yang sudah ada di repo (halaman statis tanpa pengujian). Karena ini seam baru, diperkenalkan harness pengujian JS minimal (unit/DOM runner, mis. Vitest + jsdom) dan satu uji asap berbasis peramban untuk halaman penuh.
- **Kriteria pengujian baik:** setiap uji menegaskan satu perilaku yang terlihat; mengubah struktur render internal tanpa mengubah keluaran tidak boleh membuat uji gagal.

## Out of Scope

- Implementasi backend (Go DDD), provisioning FreeRADIUS, dan skema Postgres/MySQL (ADR 0001–0002).
- Aplikasi web (Svelte): autentikasi **Magic Link**, pembayaran, logika **Extend**, dan halaman **Account Status**/**Admin Panel**.
- Pengiriman SMS via modem Wavecom (ADR 0003).
- UI **Admin Panel** untuk CRUD **Package** — hanya efeknya (daftar **Package** via API) yang relevan di sini.
- Pengelolaan mode trial/member.
- Fitur template lain yang tidak terkait (store, biolink, FAQ, chat, media sosial, running text, slider).

## Further Notes

- **Login Page** tetap disajikan oleh RouterOS; variabel template `$(...)` yang ada tetap dipakai.
- URL basis backend dan URL aplikasi pembelian harus dapat dikonfigurasi per deployment, bukan di-hardcode.
- Daftar **Package** boleh di-cache singkat di sisi klien, tetapi harus batal saat admin mengubah **Package** (mis. dengan versi/etag pada respons).
- Glosarium domain di `CONTEXT.md` bersifat mengikat: gunakan istilah **Package**, **Active Period**, **Phone Number**, **PIN**, **Magic Link**, **Extend**, **Transaction**, **Login Page**, **Account Status**, **Admin Panel** — bukan sinonim yang dilarang.
