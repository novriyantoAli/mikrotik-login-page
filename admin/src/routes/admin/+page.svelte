<script lang="ts">
  import PageHeader from '$lib/components/shared/PageHeader.svelte';
  import { createSessionQuery } from '$lib/domains/auth';
  import { AdminManagement } from '$lib/domains/admins';

  const session = createSessionQuery();
  const canManage = $derived(session.data?.role === 'owner');
</script>

<svelte:head>
  <title>Kelola Admin — Panel Admin</title>
</svelte:head>

<div class="space-y-6">
  <PageHeader
    title="Kelola Admin"
    description="Menambah admin, mengganti peran, mereset kata sandi, dan menonaktifkan admin."
  />

  {#if canManage}
    <AdminManagement {canManage} />
  {:else}
    <p class="bg-muted rounded-md px-4 py-3 text-sm" role="alert">
      Peran Anda tidak diizinkan membuka halaman ini. Hanya owner yang mengelola admin.
    </p>
  {/if}
</div>
