<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import { PackageManagement } from '$lib/domains/packages';

  let { canWrite = true }: { canWrite?: boolean } = $props();

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <PackageManagement {canWrite} />
</QueryClientProvider>
