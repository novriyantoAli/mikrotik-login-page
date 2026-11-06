<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import { TransactionManagement } from '$lib/domains/transactions';

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <TransactionManagement />
</QueryClientProvider>
