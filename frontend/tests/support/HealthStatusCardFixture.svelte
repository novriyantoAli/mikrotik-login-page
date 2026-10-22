<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import HealthStatusCard from '$lib/domains/health/components/HealthStatusCard.svelte';

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, refetchInterval: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <HealthStatusCard />
</QueryClientProvider>
