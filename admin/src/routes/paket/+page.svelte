<script lang="ts">
  import PageHeader from '$lib/components/shared/PageHeader.svelte';
  import { createSessionQuery } from '$lib/domains/auth';
  import { PackageManagement } from '$lib/domains/packages';

  const session = createSessionQuery();
  const canWrite = $derived(session.data?.role === 'owner');
</script>

<svelte:head>
  <title>Katalog Package — Panel Admin</title>
</svelte:head>

<div class="space-y-6">
  <PageHeader
    title="Katalog Package"
    description="Seluruh Package, termasuk yang nonaktif. Login Page hanya menampilkan yang aktif."
  />

  <PackageManagement {canWrite} />
</div>
