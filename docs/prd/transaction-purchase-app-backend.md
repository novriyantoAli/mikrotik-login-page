---
status: delivered
judul: Transaksi Package — Backend & Aplikasi Pembelian
pelaku: Pengguna hotspot, Pembeli Extend, Operator hotspot, Admin
---

# PRD — Transaksi Package: Backend & Aplikasi Pembelian

> **Status triase:** `delivered` (#17 merge menutup slice terakhir)
> **Konteks:** `CONTEXT.md` (glosarium domain, mengikat) dan `docs/adr/` (ADR 0001–0009).
> **Ruang lingkup:** monorepo baru (`backend/` + `frontend/`) sesuai ADR 0007. **Login Page** sudah selesai (PRD sebelumnya) dan tidak diubah.
> **Cakupan:** backend + aplikasi pembelian, termasuk **Account Status**. **Admin Panel** di luar cakupan.

## Problem Statement

**Login Page** hotspot sudah menampilkan daftar **Package** dari backend dan tombol "Beli" sudah mengarah ke aplikasi pembelian dengan **Package** yang dipilih. Tetapi aplikasi pembelian dan backend yang dituju belum ada. Dari sudut pandang pengguna:

- Menekan "Beli" berakhir di halaman kosong: tidak ada tempat untuk memasukkan **Phone Number**, tidak ada cara membayar, dan tidak ada yang mengirim **PIN**.
- Pengguna yang membayar tidak punya bukti apa pun bahwa pembeliannya berhasil: tidak ada **Account Status**, tidak ada riwayat **Transaction**, dan **PIN** hanya akan ada di SMS yang mudah hilang.
- Pengguna yang **Active Period**-nya masih berjalan tidak bisa **Extend**: sisa waktunya harus hangus atau menunggu kedaluwarsa dulu untuk bisa membeli lagi.
- Pengguna yang kembali setelah sesi lama tidak punya jalan masuk selain menunggu SMS baru, sehingga **PIN** yang sudah dibayar bisa tidak terjangkau.
- Operator harus menyediakan akun hotspot secara manual: tidak ada yang menulis kredensial ke FreeRADIUS, sehingga **Phone Number** + **PIN** di **Login Page** tidak pernah valid.

## Solution

Membangun backend Go DDD dan aplikasi pembelian SvelteKit di monorepo ini:

1. **Aplikasi pembelian** menerima `package` dan `return` dari **Login Page**, meminta **Phone Number**, lalu mengirim **Magic Link** via SMS. Setelah **Magic Link** ditukar, pengguna punya sesi (cookie opaque, ADR 0009) tanpa kata sandi.
2. Pengguna memilih kanal pembayaran tertutup (VA/QRIS) dan membayar di dalam aplikasi, dengan instruksi pembayaran dan status yang dipoll sampai lunas. Settlement hanya diakui dari callback gateway (ADR 0008).
3. Setelah **Transaction** lunas, worker menyediakan akun hotspot: menulis kredensial ke FreeRADIUS (username = **Phone Number**, password = **PIN** baru, `Expiration`, `Mikrotik-Rate-Limit`) sesuai ADR 0001/0004, lalu mengirim **PIN** via SMS.
4. **Account Status** menampilkan **Package** aktif, sisa **Active Period**, **PIN** saat ini, dan riwayat **Transaction** — sehingga SMS yang hilang tidak membuat pembeli kehilangan akses.
5. Pembelian saat **Active Period** masih berjalan menjadi **Extend**: durasi baru ditambahkan ke expiration yang ada, bukan menimpanya.
6. Backend juga melayani dua endpoint baca yang sudah dipakai **Login Page** (`GET /packages`, `GET /active-period`) dengan bentuk respons yang sudah dibekukan oleh uji yang ada.

## User Stories

### Memulai dan masuk

1. Sebagai pembeli baru, saya ingin membuka aplikasi pembelian dari tombol "Beli" di **Login Page**, sehingga saya bisa mulai membeli tanpa mencari alamat lain.
2. Sebagai pembeli baru, saya ingin **Package** yang saya tekan di **Login Page** sudah terpilih di aplikasi, sehingga saya tidak perlu memilih dua kali.
3. Sebagai pembeli, saya ingin melihat nama, harga, durasi (**Active Period**), dan batas kecepatan **Package** yang saya pilih sebelum membayar, sehingga saya yakin membeli yang benar.
4. Sebagai pembeli, saya ingin memasukkan **Phone Number** saja untuk memulai, sehingga saya tidak perlu membuat akun atau mengingat kata sandi.
5. Sebagai pembeli, saya ingin menerima **Magic Link** lewat SMS, sehingga tidak ada kata sandi yang harus saya simpan.
6. Sebagai pembeli, saya ingin aplikasi memberi tahu bahwa SMS sudah dikirim, sehingga saya tahu harus menunggu di ponsel saya.
7. Sebagai pembeli yang salah mengetik **Phone Number**, saya ingin bisa memperbaikinya dan meminta SMS ulang, sehingga saya tidak terjebak menunggu SMS yang tidak akan pernah datang.
8. Sebagai pembeli, saya ingin **Magic Link** yang saya tekan langsung membuka aplikasi dalam keadaan sudah masuk, sehingga saya tidak perlu mengetik apa pun lagi.
9. Sebagai pembeli, saya ingin **Magic Link** hanya bisa dipakai sekali dan kedaluwarsa cepat, sehingga tautan yang diteruskan orang lain tidak bisa membuka akun saya.
10. Sebagai pengguna, saya ingin sesi saya bertahan lama, sehingga saya bisa kembali ke **Account Status** beberapa minggu kemudian tanpa SMS baru.
11. Sebagai pengguna, saya ingin sesi yang bocor bisa dicabut oleh operator, sehingga akses saya tidak bergantung pada token yang tidak bisa ditarik.
12. Sebagai pengguna, saya ingin diminta **Magic Link** baru ketika sesi saya kedaluwarsa atau dicabut, dengan penjelasan yang jelas, sehingga saya tidak bingung mengapa halaman kosong.

### Membayar

13. Sebagai pembeli, saya ingin melihat kanal pembayaran yang tersedia beserta biayanya, sehingga saya bisa memilih yang paling nyaman.
14. Sebagai pembeli, saya ingin memilih VA atau QRIS, sehingga pembayaran selesai tanpa meninggalkan aplikasi.
15. Sebagai pembeli, saya ingin melihat nomor VA atau kode QR beserta instruksi pembayaran di dalam aplikasi, sehingga saya tidak perlu membuka aplikasi bank untuk mencari caranya.
16. Sebagai pembeli, saya ingin melihat batas waktu pembayaran yang berlaku, sehingga saya tahu kapan harus menyelesaikan pembayaran.
17. Sebagai pembeli, saya ingin status pembayaran berubah sendiri saat saya sudah membayar, sehingga saya tidak perlu menekan tombol apa pun untuk memastikan.
18. Sebagai pembeli, saya ingin diberi tahu segera setelah **Transaction** lunas, sehingga saya bisa langsung masuk ke hotspot.
19. Sebagai pembeli yang menutup aplikasi sebelum membayar, saya ingin bisa kembali ke **Transaction** yang sama dan melihat instruksinya lagi, sehingga saya tidak membayar dua kali.
20. Sebagai pembeli yang menekan "Bayar" dua kali, saya ingin hanya satu **Transaction** yang dibuat, sehingga saya tidak ditagih dua kali.
21. Sebagai pembeli, saya ingin **Transaction** yang tidak dibayar sampai batas waktu berakhir menjadi kedaluwarsa dengan pesan yang jelas, sehingga saya tahu harus memulai pembelian baru.
22. Sebagai pembeli, saya ingin bisa mengganti kanal pembayaran dengan membuat **Transaction** baru saat yang lama kedaluwarsa, sehingga saya tidak terhalang.
23. Sebagai pembeli, saya ingin harga yang saya lihat sama dengan jumlah yang diminta gateway, sehingga saya tidak dikenai biaya kejutan.
24. Sebagai pembeli, saya ingin melihat bahwa pembayaran sedang diverifikasi ketika gateway belum memberi kabar, sehingga saya tidak panik dan membayar ulang.

### Mendapat kredensial hotspot

25. Sebagai pembeli, saya ingin menerima **PIN** 6 digit via SMS setelah pembayaran lunas, sehingga saya bisa masuk ke hotspot.
26. Sebagai pembeli, saya ingin akun hotspot saya memakai **Phone Number** sebagai username, sehingga saya hanya perlu mengingat satu hal.
27. Sebagai pembeli, saya ingin **Active Period** saya mulai berjalan sesuai durasi **Package** yang saya beli, sehingga saya mendapat waktu yang saya bayar.
28. Sebagai pembeli, saya ingin batas kecepatan **Package** saya berlaku di hotspot, sehingga saya mendapat layanan sesuai harga.
29. Sebagai pembeli, saya ingin **PIN** saya diganti pada setiap pembelian, sehingga **PIN** lama yang pernah bocor tidak bisa dipakai lagi.
30. Sebagai pembeli, saya ingin pembelian yang sudah lunas tetap tersedia walau modem SMS sedang bermasalah, sehingga saya tidak kehilangan hak akses hanya karena SMS gagal terkirim.
31. Sebagai pembeli, saya ingin SMS **PIN** dikirim ulang secara otomatis ketika modem pulih, sehingga saya tetap menerimanya tanpa menghubungi siapa pun.
32. Sebagai pembeli, saya ingin **PIN** saya tetap bisa dilihat di **Account Status**, sehingga SMS yang hilang atau terhapus tidak membuat saya terkunci.
33. Sebagai operator, saya ingin akun ditulis langsung ke FreeRADIUS, sehingga tidak ada layanan per-login yang bisa gagal saat pengguna masuk.
34. Sebagai operator, saya ingin penulisan akun dan pengiriman SMS dijalankan sebagai pekerjaan latar, sehingga permintaan pembayaran tidak ikut menunggu modem atau database RADIUS.
35. Sebagai operator, saya ingin pekerjaan penyediaan akun aman dijalankan ulang, sehingga percobaan ulang tidak menggandakan atau menimpa **Active Period** yang sudah benar.
36. Sebagai pengguna hotspot, saya ingin login dengan **Phone Number** dan **PIN** di **Login Page** berhasil segera setelah pembayaran, sehingga saya tidak perlu menunggu operator.

### Account Status

37. Sebagai pengguna, saya ingin membuka **Account Status** dengan **Magic Link**/sesi saya, sehingga hanya saya yang bisa melihat **PIN** saya.
38. Sebagai pengguna, saya ingin melihat sisa **Active Period** dalam bentuk yang mudah dibaca, sehingga saya tahu kapan koneksi saya berakhir.
39. Sebagai pengguna, saya ingin melihat tanggal dan jam berakhirnya **Active Period**, sehingga saya bisa merencanakannya.
40. Sebagai pengguna, saya ingin melihat indikator ketika **Active Period** hampir berakhir, sehingga saya bisa **Extend** sebelum terputus.
41. Sebagai pengguna, saya ingin melihat **Package** yang sedang aktif beserta batas kecepatannya, sehingga saya tahu apa yang sedang saya pakai.
42. Sebagai pengguna, saya ingin melihat **PIN** saat ini dengan tombol tunjuk/sembunyi, sehingga **PIN** tidak terlihat orang di sebelah saya.
43. Sebagai pengguna, saya ingin melihat riwayat **Transaction** beserta tanggal, **Package**, jumlah, dan statusnya, sehingga saya punya bukti pembelian.
44. Sebagai pengguna, saya ingin membuka detail satu **Transaction** dari riwayat, sehingga saya bisa memeriksa apakah pembayaran tertentu sudah lunas.
45. Sebagai pengguna yang belum pernah membeli, saya ingin **Account Status** menjelaskan bahwa belum ada **Active Period**, sehingga saya diarahkan ke daftar **Package**.
46. Sebagai pengguna yang **Active Period**-nya sudah habis, saya ingin melihat keadaan kedaluwarsa dan tombol beli, sehingga saya bisa segera online kembali.

### Extend

47. Sebagai pengguna dengan **Active Period** berjalan, saya ingin membeli **Package** lagi dan mendapat tambahan durasi di atas sisa waktu saya, sehingga sisa waktu tidak hangus.
48. Sebagai pengguna, saya ingin diminta konfirmasi yang menjelaskan bahwa pembelian ini adalah **Extend**, sehingga saya tidak mengira akan mulai dari nol.
49. Sebagai pengguna, saya ingin melihat perkiraan expiration baru sebelum membayar, sehingga saya tahu hasil **Extend** saya.
50. Sebagai pengguna yang **Active Period**-nya sudah habis, saya ingin pembelian berikutnya mulai dari sekarang, sehingga saya tidak mendapat waktu yang sudah lewat.
51. Sebagai pengguna, saya ingin **Extend** dengan **Package** berbeda durasi dan kecepatan tetap dihitung benar, sehingga durasi bertambah dan batas kecepatan mengikuti **Package** terbaru.
52. Sebagai pengguna, saya ingin **Extend** tidak mengubah **Phone Number** saya, sehingga saya tetap masuk dengan kredensial yang sama.

### Kembali ke hotspot

53. Sebagai pembeli, saya ingin setelah pembayaran selesai diarahkan kembali ke **Login Page** hotspot tempat saya mulai, sehingga saya bisa langsung masuk.
54. Sebagai pembeli yang berpindah jaringan, saya ingin aplikasi tidak mengarahkan saya ke alamat **Login Page** yang tidak dikenal, sehingga saya tidak dikirim ke situs lain.
55. Sebagai pengguna, saya ingin instruksi singkat setelah pembayaran (masuk dengan **Phone Number** dan **PIN**), sehingga saya tahu langkah berikutnya tanpa membaca dokumentasi.

### Operator, konfigurasi, dan ketahanan

56. Sebagai operator, saya ingin mengatur kredensial gateway, kode merchant, dan kanal yang aktif lewat konfigurasi deployment, sehingga saya bisa memakai sandbox atau produksi tanpa mengubah kode.
57. Sebagai operator, saya ingin mengatur masa hidup sesi dan masa berlaku **Magic Link** lewat konfigurasi, sehingga saya bisa menyesuaikan kebijakan keamanan.
58. Sebagai operator, saya ingin aplikasi dan API disajikan pada satu origin, sehingga tidak ada pengaturan CORS dan cookie tidak pernah tertolak.
59. Sebagai operator, saya ingin backend berjalan sebagai API dan worker terpisah dari image yang sama, sehingga pekerjaan latar tidak memperlambat permintaan pengguna.
60. Sebagai operator, saya ingin hanya satu proses worker yang memegang modem, sehingga perintah AT tidak saling tabrakan.
61. Sebagai operator, saya ingin log terstruktur untuk setiap perubahan status **Transaction** dan setiap percobaan penyediaan akun, sehingga saya bisa menelusuri keluhan pembeli.
62. Sebagai operator, saya ingin health check pada API dan worker, sehingga gangguan terdeteksi tanpa menunggu laporan pengguna.
63. Sebagai operator, saya ingin **Login Page** tetap bisa login walaupun API tidak terjangkau, sehingga gangguan backend tidak memblokir pengguna lama.
64. Sebagai operator, saya ingin daftar **Package** dapat diisi lewat seed saat deployment, sehingga sistem bisa dipakai sebelum **Admin Panel** ada.

### Keamanan dan anti-penyalahgunaan

65. Sebagai operator, saya ingin permintaan **Magic Link** dibatasi laju per **Phone Number** dan per alamat IP, sehingga SMS tidak bisa dipakai untuk membanjiri orang lain.
66. Sebagai operator, saya ingin respons permintaan **Magic Link** selalu sama, sehingga penyerang tidak bisa menebak **Phone Number** mana yang terdaftar.
67. Sebagai operator, saya ingin callback gateway diverifikasi tanda tangannya sebelum dipercaya, sehingga pihak lain tidak bisa menandai **Transaction** lunas.
68. Sebagai operator, saya ingin callback yang diulang tidak memicu penyediaan akun kedua kali, sehingga **Active Period** tidak bertambah karena pengulangan.
69. Sebagai pengguna, saya ingin **PIN** dan token **Magic Link** tidak pernah tersimpan dalam bentuk terbuka di database aplikasi, sehingga kebocoran database tidak langsung memberi akses.
70. Sebagai pengguna, saya ingin halaman **Account Status** hanya menampilkan data **Phone Number** milik sesi saya, sehingga saya tidak bisa melihat **PIN** orang lain dengan mengubah parameter.
71. Sebagai pengguna, saya ingin parameter `return` hanya menerima alamat **Login Page** yang sah, sehingga aplikasi tidak bisa dipakai untuk pengalihan terbuka.

### Pengalaman pemakaian

72. Sebagai pengguna ponsel, saya ingin seluruh alur (pilih **Package**, masukkan **Phone Number**, bayar, lihat **PIN**) nyaman disentuh dan tidak perlu diperbesar, sehingga saya bisa membeli sambil berdiri di depan hotspot.
73. Sebagai pengguna, saya ingin setiap langkah menunjukkan keadaan memuat, berhasil, atau gagal dengan pesan yang bisa dimengerti, sehingga saya tidak pernah melihat halaman diam tanpa penjelasan.
74. Sebagai pengguna, saya ingin mencoba lagi setelah gangguan jaringan tanpa kehilangan langkah saya, sehingga pembayaran yang sudah dibuat tidak terbuang.
75. Sebagai pengguna berbahasa Indonesia, saya ingin semua teks aplikasi berbahasa Indonesia dengan istilah domain yang konsisten (**Package**, **Active Period**, **PIN**, **Extend**, **Transaction**), sehingga saya tidak melihat campuran istilah asing yang membingungkan.

## Implementation Decisions

### Monorepo dan teknologi

1. **Monorepo baru mengikuti ADR 0007.** Backend dan aplikasi pembelian dibangun dari nol di repo ini; `mikrotik-login` hanya dipakai sebagai prior art pola (integrasi gateway, worker queue, auth) dan kosakatanya tidak diadopsi. Repo ini tetap memegang satu `CONTEXT.md`, satu urutan ADR (lanjut di 0010), dan satu API contract antara backend dan aplikasi.

2. **Backend mengikuti arsitektur DDD Go proyek ini.** Fx untuk DI, Gin untuk HTTP, GORM + Postgres untuk data backend, Asynq (Redis) untuk pekerjaan latar, viper untuk config, zap untuk log. Satu domain = satu vertical slice lengkap, layering satu arah `handler → service → repository → entity`, interface di samping implementasi, DTO sebagai satu-satunya hal yang menyeberang batas HTTP.

3. **Aplikasi pembelian mengikuti arsitektur DDD SvelteKit proyek ini.** SvelteKit + Svelte 5 runes + Tailwind v4 + shadcn-svelte, axios untuk HTTP, TanStack Query untuk server state. Layering satu arah `route → component → query/state → api → klien axios bersama`; zod schema sebagai satu-satunya hal yang menyeberang batas HTTP; komponen tidak pernah memanggil axios langsung.

4. **Aplikasi disajikan sebagai SPA di origin yang sama dengan API** (ADR 0009). Tidak ada SSR yang memegang sesi: sesi hanya hidup di cookie opaque yang dibaca backend, sehingga aplikasi tidak pernah memerlukan akses ke isi cookie. Tidak ada CORS, tidak ada token di JavaScript.

### Domain backend

5. **Enam domain backend:** `package` (katalog **Package**), `transaction` (**Transaction** beserta state-nya), `payment` (port gateway + adapter Tripay), `provisioning` (penulis akun FreeRADIUS), `notification` (**PIN**/SMS via modem), dan `auth` (**Magic Link** + sesi). Domain `package` hanya membaca katalog pada slice ini; penulisan katalog menunggu **Admin Panel**.

6. **`account` bukan domain sendiri** pada slice ini: **Account Status** adalah komposisi bacaan dari `transaction` (riwayat), `provisioning` (**PIN** dan expiration saat ini), dan `package` (**Package** aktif), karena **Phone Number** adalah satu-satunya identitas klien dan tidak ada record akun (CONTEXT.md). Ini mencegah lahirnya tabel "account" yang akan menduplikasi kebenaran FreeRADIUS.

7. **Tidak ada record pelanggan.** Tabel Postgres yang ditambahkan: `packages`, `transactions`, `sessions`, `magic_link_tokens`, `sms_outbox`. Kebenaran kredensial dan **Active Period** tetap di `radcheck`/`radreply` (ADR 0001) — backend adalah penulis, FreeRADIUS adalah sumber kebenaran saat login. **PIN** terbaru disimpan terenkripsi di Postgres dengan kunci deployment yang berversi, sehingga **Account Status** bisa menampilkannya kembali walau SMS hilang, sementara `radcheck` menyimpan `Cleartext-Password` sebagaimana dibutuhkan CHAP (ADR 0004). Token **Magic Link** dan token sesi hanya disimpan sebagai hash.

8. **Kontrak Login Page dibekukan.** Backend wajib memenuhi dua endpoint baca yang sudah dipakai **Login Page** dan sudah diuji di repo ini, tanpa perubahan bentuk:
   - `GET /packages` → `{ data: [{ id, nama, harga, durasiActivePeriod, batasKecepatan }] }`
   - `GET /active-period?phone=<Phone Number>` → `{ data: { expiry } }`, `expiry` berupa ISO-8601 atau epoch seconds (ADR 0005).
   Keduanya publik, read-only, tanpa sesi. Perubahan bentuk respons adalah perubahan yang merusak dan harus ditolak saat review.

9. **Normalisasi Phone Number.** Semua **Phone Number** dinormalisasi ke bentuk kanonik Indonesia (`08xx` → `628xx`, hanya digit, tanpa spasi/tanda hubung) sebelum dipakai sebagai username FreeRADIUS, kunci **Transaction**, kunci sesi, dan tujuan SMS. Normalisasi terjadi sekali di satu fungsi domain bersama; tidak ada penyimpanan **Phone Number** dalam bentuk tidak kanonik. Ini yang membuat login hotspot dengan **Phone Number** yang diketik berbeda-beda tetap menemukan akun yang sama.

### Auth, Magic Link, dan sesi

10. **Permintaan Magic Link** menerima **Phone Number** (dan opsional `package` serta `return`), selalu dibalas dengan respons sukses yang identik, dan tidak pernah mengungkap apakah **Phone Number** itu pernah bertransaksi. Pembatasan laju per **Phone Number** dan per IP; permintaan berulang dalam jendela singkat tidak mengirim SMS tambahan.

11. **Magic Link** adalah token acak sekali-pakai berumur pendek (default 15 menit, dapat dikonfigurasi) yang dikirim sebagai tautan ke aplikasi. Di database hanya disimpan hash token beserta tujuan (checkout **Package** tertentu atau **Account Status**), waktu kedaluwarsa, dan penanda sudah dipakai. Menukar token yang sudah dipakai, kedaluwarsa, atau tidak dikenal menghasilkan pesan "tautan tidak berlaku" yang seragam.

12. **Penukaran Magic Link** membuat sesi: token sesi acak dikirim ke browser sebagai cookie `HttpOnly`, `SameSite=Lax`, `Secure` (dapat dimatikan untuk deployment lokal), berlaku 30 hari (dapat dikonfigurasi). Di Postgres hanya disimpan hash token sesi, **Phone Number**, waktu dibuat/kedaluwarsa, dan penanda dicabut. Tidak ada JWT, tidak ada pasangan access/refresh, tidak ada CORS (ADR 0009).

13. **Penjaga akses** untuk semua endpoint pengguna adalah sesi yang valid. Endpoint yang menyebut **Phone Number** di URL atau body wajib mencocokkannya dengan **Phone Number** sesi; ketidakcocokan dijawab sebagai tidak ditemukan, bukan sebagai terlarang, sehingga tidak membocorkan keberadaan data.

14. **Otorisasi Account Status adalah sesi, bukan SMS.** Karena **PIN** yang hilang di SMS tidak boleh mengunci pembeli (CONTEXT.md), endpoint **Account Status** mengembalikan **PIN** saat ini untuk **Phone Number** milik sesi.

### Transaction dan pembayaran

15. **State Transaction** yang eksplisit dan saling eksklusif: `pending` (menunggu pembayaran), `paid` (settlement diterima, penyediaan belum selesai), `provisioned` (akun FreeRADIUS tertulis dan SMS **PIN** masuk antrean), `failed` (gateway menolak/membatalkan), `expired` (batas waktu pembayaran lewat). Transisi hanya boleh: `pending → paid`, `pending → failed`, `pending → expired`, `paid → provisioned`, dengan `failed`/`expired` sebagai state akhir. Tidak ada transisi mundur; penyediaan yang gagal tetap berada di `paid` dan dicoba ulang.

16. **Settlement hanya dari gateway.** Callback gateway adalah sinyal otoritatif (ADR 0008). Polling status per **Transaction** bersifat baca-saja untuk pengguna dan, untuk kasus callback hilang, sebuah pekerjaan rekonsiliasi berkala menanyakan status ke gateway lalu memakai jalur transisi yang sama. Tidak ada jalur lain yang boleh menandai `paid`.

17. **Pembuatan Transaction idempoten per sesi.** Bila sesi yang sama meminta checkout untuk **Package** yang sama sementara masih ada `pending` yang belum kedaluwarsa, **Transaction** itu yang dikembalikan (beserta instruksi pembayarannya) alih-alih membuat yang baru. Perubahan kanal pembayaran dilakukan dengan menunggu `pending` kedaluwarsa atau membatalkannya secara eksplisit, bukan dengan membuat tagihan ganda.

18. **Port `PaymentGateway`** adalah satu-satunya batas ke vendor: membuat tagihan (jumlah, nama produk, kanal, waktu kedaluwarsa, callback URL), memverifikasi callback (payload mentah + tanda tangan), menanyakan status, dan mengambil daftar kanal. Adapter Tripay adalah satu-satunya kode yang mengetahui kode merchant, tanda tangan HMAC, dan nama kanal; tidak ada tipe Tripay yang menyeberang ke service **Transaction** atau ke DTO HTTP. Pemetaan status vendor → state **Transaction** dilakukan di adapter, sehingga state domain tidak bergantung pada kosakata vendor.

19. **Checkout memakai kanal tertutup** (VA/QRIS) yang dirender di dalam aplikasi beserta nomor VA/kode QR dan instruksi pembayaran, bukan pengalihan ke halaman hosted (ADR 0008). Jumlah yang ditagih adalah harga **Package**; biaya kanal ditampilkan terpisah dari respons daftar kanal dan tidak pernah dihitung ulang di klien.

20. **Batas waktu pembayaran** mengikuti konfigurasi (default 1 jam, minimum yang diizinkan gateway). **Transaction** `pending` yang melewati batas waktu dipindahkan ke `expired` oleh pekerjaan latar, bukan oleh klien. Halaman pembayaran yang masih terbuka menampilkan keadaan kedaluwarsa dan tombol membuat **Transaction** baru.

21. **Extend dihitung di backend, bukan di klien.** Saat penyediaan akun: bila expiration FreeRADIUS yang ada masih di masa depan, expiration baru = expiration lama + durasi **Package**; bila sudah lewat atau belum ada, expiration baru = waktu settlement + durasi **Package**. Aplikasi hanya menampilkan perkiraan expiration baru untuk konfirmasi; angka final selalu berasal dari backend pada saat penyediaan.

### Penyediaan akun dan SMS

22. **Penyediaan akun berjalan sebagai pekerjaan latar** yang dipicu transisi `paid → provisioned`, sehingga tidak ada permintaan pengguna yang menunggu modem atau MySQL FreeRADIUS. Pekerjaan ini idempoten terhadap `Transaction` (kunci = identitas **Transaction**): menjalankannya ulang tidak menambah durasi dua kali dan tidak mengganti **PIN** yang sudah tertulis.

23. **Penulisan FreeRADIUS** menulis, untuk username = **Phone Number**: atribut password (**PIN** 6 digit baru per pembelian, ADR 0004), `Expiration` = expiration hasil poin 21, dan `Mikrotik-Rate-Limit` dari batas kecepatan **Package**. Penulisan bersifat upsert sehingga pembelian pertama dan **Extend** memakai jalur yang sama. Konsistensi antar-tabel adalah tanggung jawab backend (ADR 0001); penulisan dilakukan dalam satu transaksi database radcheck/radreply.

24. **Notifikasi SMS adalah outbox, bukan panggilan langsung.** Setiap **PIN** yang harus dikirim ditulis ke `sms_outbox` dalam transaksi yang sama dengan penyediaan akun, lalu pekerja mengirimkannya lewat modem. Bila modem mati atau gagal, entri tetap tertunda dan dicoba ulang dengan backoff sampai berhasil, sehingga pembeli yang sudah bayar tidak kehilangan **PIN** (yang juga bisa dilihat di **Account Status**). Hanya satu proses worker yang boleh memegang modem, dan pengiriman diserialisasi.

25. **Adapter modem** membungkus perintah AT (ADR 0003) di balik port pengirim SMS dengan operasi tunggal "kirim SMS ke nomor". Tidak ada fallback ke gateway pihak ketiga; kegagalan modem adalah kondisi yang harus terlihat di log dan metrik, bukan disembunyikan.

### Aplikasi pembelian

26. **Empat domain di aplikasi:** `packages` (katalog), `auth` (**Magic Link** + sesi), `checkout` (**Transaction**, kanal, instruksi, polling status), `account` (**Account Status**, **PIN**, riwayat, pintu masuk **Extend**). Auth diperlakukan sebagai domain bisnis biasa, bukan pengecualian; tidak ada state sesi di store lintas-domain.

27. **Alur masuk dari Login Page.** Aplikasi membaca `package` dan `return` dari URL. Bila belum ada sesi, aplikasi meminta **Phone Number** lalu meminta **Magic Link** dengan tujuan (checkout **Package** atau **Account Status**) yang ikut tersimpan bersama token; setelah ditukar, pengguna langsung berada di tujuan itu dengan **Package** terpilih. Parameter `return` hanya diterima bila cocok dengan daftar alamat **Login Page** yang diizinkan pada konfigurasi deployment; selain itu diabaikan.

28. **Polling status** dilakukan pada **Transaction** yang sedang dilihat dengan interval mundur yang wajar, berhenti saat state akhir tercapai atau saat tab tidak terlihat, dan tidak pernah menjadi satu-satunya sumber kebenaran (callback tetap otoritatif).

29. **Kegagalan ditampilkan sebagai keadaan, bukan sebagai halaman error.** Setiap layar punya keadaan memuat, kosong, gagal-dengan-coba-lagi, dan kedaluwarsa-sesi. Kegagalan mengambil katalog atau status tidak boleh menghapus langkah yang sudah dicapai pengguna.

30. **Semua teks antarmuka berbahasa Indonesia** dan memakai istilah domain yang mengikat: **Package**, **Active Period**, **Phone Number**, **PIN**, **Magic Link**, **Extend**, **Transaction**, **Account Status**. Istilah terlarang di CONTEXT.md (Voucher, Order, Renew, top-up, dan seterusnya) tidak boleh muncul di UI maupun di nama tipe.

### Konfigurasi, kontrak HTTP, dan deployment

31. **Kontrak HTTP** memakai amplop konsisten `{ data, ... }` untuk sukses dan `{ error: { code, message } }` untuk gagal, dengan `code` yang stabil dan dapat dipakai klien untuk bercabang. Endpoint pengguna: permintaan **Magic Link**, penukaran **Magic Link**, keluar (mencabut sesi), katalog **Package**, daftar kanal, buat **Transaction**, status **Transaction**, riwayat **Transaction**, ringkasan **Account Status**. Endpoint publik: dua endpoint baca milik **Login Page** (poin 8), callback gateway, dan health check.

32. **Konfigurasi** diisi viper (`config.yaml` + override env): koneksi Postgres, koneksi MySQL FreeRADIUS, Redis, kredensial Tripay (API key, private key, merchant code, base URL sandbox/produksi, callback base URL, masa berlaku pembayaran), perangkat modem dan parameter serial, kunci enkripsi **PIN**, masa berlaku **Magic Link**, masa hidup sesi, daftar alamat **Login Page** yang diizinkan untuk `return`, dan `Secure` cookie. Tidak ada nilai deployment yang di-hardcode, dan tidak ada rahasia yang masuk repo.

33. **Deployment** berupa satu docker-compose: Postgres, MySQL FreeRADIUS, Redis, API, worker, aplikasi pembelian, dan reverse proxy yang menyajikan aplikasi dan mem-proxy `/api` ke backend (ADR 0009). API dan worker memakai image yang sama dengan perintah berbeda. Worker adalah satu-satunya proses dengan akses ke perangkat modem.

# Testing Decisions

- **Prinsip:** uji perilaku eksternal saja — apa yang dikirim klien dan apa yang dikembalikan sistem, atau apa yang berubah pada data yang memang milik sistem. Uji tidak boleh menegaskan nama fungsi internal, struktur tabel perantara, urutan panggilan, atau jumlah query. Mengubah cara render atau memecah service internal tanpa mengubah kontrak tidak boleh membuat uji gagal.

- **Satu seam utama: kontrak HTTP API backend**, dipakai dari tiga arah pada batas yang sama:
  - **Backend dari dalam** — uji integrasi menembak router HTTP penuh dengan Postgres asli, sementara port vendor diganti palsu: gateway pembayaran, pengirim SMS, penulis FreeRADIUS, dan clock. Assertion berupa request/response HTTP dan efek yang terlihat lewat endpoint lain (mis. expiration yang naik setelah **Extend**, status yang berpindah setelah callback, **PIN** yang tampil di **Account Status**).
  - **Aplikasi dari luar** — uji Vitest + `@testing-library/svelte` dengan adapter axios diganti stub yang mengimplementasikan kontrak JSON yang sama; query dan komponen diuji lewat perilaku yang terlihat pengguna (instruksi VA tampil, keadaan kedaluwarsa, tombol **Extend** mengirim permintaan yang benar).
  - **Satu uji asap Playwright** — aplikasi sungguhan melawan backend sungguhan (vendor tetap palsu), menempuh jalur **Magic Link → pilih kanal → bayar (settlement dipicu lewat endpoint uji/callback palsu) → PIN tampil di Account Status → kembali ke Login Page**.

- **Fixture kontrak dipakai bersama** oleh kedua sisi. Satu himpunan contoh respons dan permintaan menjadi sumber tunggal, sehingga backend dan aplikasi tidak bisa menyimpang tanpa ada uji yang gagal. Perubahan kontrak yang disengaja menuntut perubahan fixture, dan itu terlihat saat review.

- **Seam lama dipertahankan apa adanya:** dua endpoint baca yang dipakai **Login Page** sudah diuji di repo ini (`tests/unit` untuk seam klien voucher, `tests/smoke` untuk halaman). Uji integrasi backend baru wajib menegaskan bentuk respons keduanya persis seperti yang diharapkan uji tersebut, dan uji **Login Page** tidak diubah.

- **Dua seam sempit di tepi vendor**, karena fake tidak bisa membuktikan keduanya:
  - **Adapter gateway** diuji dengan fixture payload callback yang bentuknya diambil dari dokumentasi/kontrak vendor, termasuk perhitungan tanda tangan yang benar dan yang salah, serta pemetaan setiap status vendor ke state **Transaction**. Uji ini tidak menyentuh jaringan.
  - **Penulis FreeRADIUS** diuji dengan menulis sungguhan ke MySQL lalu membaca kembali `radcheck`/`radreply` untuk menegaskan username, password, `Expiration`, dan `Mikrotik-Rate-Limit`, termasuk kasus **Extend** (menambah) dan kasus expiration yang sudah lewat (mulai dari sekarang). Ini satu-satunya uji yang menyentuh dua database.

- **Yang diuji per area:** katalog **Package** (bentuk respons dan degradasi), **Magic Link** (sekali pakai, kedaluwarsa, respons seragam, pembatasan laju), sesi (cookie, kedaluwarsa, pencabutan, isolasi antar-**Phone Number**), **Transaction** (state dan transisi terlarang, idempotensi checkout ganda, kedaluwarsa, callback berulang, callback bertanda tangan salah, rekonsiliasi), penyediaan akun (**Extend** bertambah, idempoten terhadap pengulangan, **PIN** baru setiap pembelian), notifikasi (SMS tertunda saat modem gagal lalu terkirim saat pulih, **PIN** tetap bisa dilihat), **Account Status** (ringkasan, **PIN**, riwayat, akses lintas-**Phone Number** ditolak), dan aplikasi (alur masuk, checkout, polling, keadaan gagal/kedaluwarsa, **Extend**, `return` yang tidak diizinkan diabaikan).

- **Clock dan waktu adalah dependensi, bukan `time.Now()` tersebar**, supaya kasus **Extend**, kedaluwarsa pembayaran, dan masa berlaku **Magic Link** bisa diuji tanpa menunggu.

- **Prior art:** uji integrasi backend di `mikrotik-login/backend/test/integration` (router HTTP penuh + Postgres asli + gateway palsu sebagai port, dengan helper penyiapan bersama) dan uji klien di repo ini (`tests/unit` dengan fetch disuntikkan, `tests/smoke` dengan backend distub pada batas jaringan). Keduanya menunjukkan pola yang sama: palsukan di tepi, tegaskan di kontrak.

# Out of Scope

- **Login Page** dan asetnya: sudah selesai dan tidak diubah pada PRD ini. Termasuk mode **Access Code** dan **User** di sisi provisioning — keduanya perilaku **Login Page** (ADR 0006), bukan alur pembelian.
- **Admin Panel** beserta API pengelolaan **Package**, daftar **Transaction** untuk admin, dan daftar pengguna aktif. Pada slice ini katalog **Package** diisi lewat seed/migrasi.
- Stok **Access Code**, pencetakan kode, dan penjualan reseller.
- Manajemen router/NAS, sinkronisasi multi-router, antrean perintah MikroTik API, dan trial berbasis alamat perangkat — semuanya milik desain `mikrotik-login` yang ditolak ADR 0007.
- Konfigurasi server FreeRADIUS sendiri (modul `sql`, NAS client, `radgroupreply`), instalasi modem, dan pengaturan operator GSM.
- Pengembalian dana (refund), pembatalan pembayaran berbayar, dan sengketa pembayaran.
- Kanal pembayaran di luar kanal tertutup Tripay (gerai retail, kartu kredit), gateway pembayaran kedua, dan kanal WhatsApp.
- Aplikasi mobile native, notifikasi push, dan email transaksional (identitas tidak menyimpan email).
- Migrasi data dari `mikrotik-login` dan kompatibilitas API dengannya.
- Fitur template **Login Page** lain (store, biolink, FAQ, chat, slider, running text).
- Analitik, laporan pendapatan, dan ekspor data.

# Further Notes

- **Urutan pembangunan yang disarankan (vertical slice):**
  1. Prefactor: kerangka monorepo, wiring Fx, config, docker-compose, harness uji (Postgres + Redis + MySQL), fixture kontrak bersama.
  2. Endpoint baca katalog dan **Active Period** yang memenuhi kontrak **Login Page** (membuka jalan bagi pemakaian nyata lebih awal).
  3. **Magic Link** + sesi + halaman masuk aplikasi.
  4. **Transaction** + port gateway + adapter Tripay + checkout/polling di aplikasi.
  5. Penyediaan FreeRADIUS + **PIN** + outbox SMS + modem.
  6. **Account Status** (ringkasan, **PIN**, riwayat) dan **Extend**.
  7. Pengerasan: pembatasan laju, rekonsiliasi, observability, uji asap end-to-end.

- **ADR yang perlu ditulis sebelum implementasi**, karena keputusan di atas melampaui ADR yang ada: (a) outbox SMS dan serialisasi modem, (b) bentuk sesi dan **Magic Link** sekali pakai beserta masa berlakunya, (c) pengisian katalog **Package** lewat seed selama **Admin Panel** belum ada, (d) aplikasi sebagai SPA tanpa SSR yang memegang sesi.

- **Risiko yang perlu dipantau:** modem sebagai titik tunggal kegagalan (ADR 0003) — mitigasinya adalah outbox dan **PIN** yang selalu bisa dilihat di **Account Status**; MySQL FreeRADIUS ditulis langsung (ADR 0001) — mitigasinya adalah penulisan transaksional dan uji tulis nyata; callback yang hilang — mitigasinya adalah pekerjaan rekonsiliasi berkala.

- **Glosarium domain bersifat mengikat.** Istilah yang dipakai di kode, DTO, nama tabel, dan UI: **Package**, **Active Period**, **Phone Number**, **PIN**, **Access Code**, **User**, **Magic Link**, **Extend**, **Transaction**, **Login Page**, **Account Status**, **Admin Panel**. Istilah "Voucher", "Order", "Renew", "top-up", "Dashboard", "Backoffice" tidak dipakai.
