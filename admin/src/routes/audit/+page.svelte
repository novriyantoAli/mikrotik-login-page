<script lang="ts">
  import PageHeader from '$lib/components/shared/PageHeader.svelte';
  import { createSessionQuery } from '$lib/domains/auth';
  import { AuditLog } from '$lib/domains/audit';

  const session = createSessionQuery();
  const canRead = $derived(session.data?.role === 'owner' || session.data?.role === 'auditor');
</script>

<svelte:head>
  <title>Jejak Audit — Panel Admin</title>
</svelte:head>

<div class="space-y-6">
  <PageHeader
    title="Jejak Audit"
    description="Setiap perubahan katalog dan setiap pembukaan PIN tercatat di sini, lengkap dengan aktor dan nilainya. Baris tidak bisa diubah atau dihapus."
  />

  {#if canRead}
    <AuditLog />
  {:else}
    <p class="bg-muted rounded-md px-4 py-3 text-sm" role="alert">
      Peran Anda tidak diizinkan membaca jejak audit. Hanya owner dan auditor yang bisa membukanya.
    </p>
  {/if}
</div>
