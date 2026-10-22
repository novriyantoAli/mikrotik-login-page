<script lang="ts">
  import { HealthStatusCard } from '$lib/domains/health';
</script>

<svelte:head>
  <title>Status Sistem — Aplikasi Pembelian</title>
</svelte:head>

<HealthStatusCard />

<p class="text-muted-foreground mt-4 text-sm">
  Halaman ini membaca laporan kesehatan backend dari <code>/api/health</code>, termasuk Postgres,
  MySQL FreeRADIUS, dan Redis.
</p>
