<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import PackageForm from '$lib/domains/packages/components/PackageForm.svelte';
  import type { AdminPackage } from '$lib/domains/packages/schemas/package.schema';

  let { pkg = null, canWrite = true }: { pkg?: AdminPackage | null; canWrite?: boolean } = $props();

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });

  let saved = $state('');
</script>

<QueryClientProvider {client}>
  <PackageForm
    {pkg}
    {canWrite}
    onSaved={(created) => {
      saved = created.name;
    }}
  />
  {#if saved}
    <p role="status">Tersimpan: {saved}</p>
  {/if}
</QueryClientProvider>
