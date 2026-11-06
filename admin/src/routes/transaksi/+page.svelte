<script lang="ts">
  import PageHeader from '$lib/components/shared/PageHeader.svelte';
  import { TransactionManagement } from '$lib/domains/transactions';
</script>

<svelte:head>
  <title>Transaksi — Panel Admin</title>
</svelte:head>

<div class="space-y-6">
  <PageHeader
    title="Transaksi"
    description="Cari Transaction berdasarkan state, Phone Number, dan tanggal. Daftar tidak memuat PIN; membuka PIN adalah tindakan terpisah yang tercatat."
  />

  <TransactionManagement />
</div>
