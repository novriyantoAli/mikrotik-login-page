<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import AccountStatus from '$lib/domains/account/components/AccountStatus.svelte';

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <AccountStatus />
</QueryClientProvider>
