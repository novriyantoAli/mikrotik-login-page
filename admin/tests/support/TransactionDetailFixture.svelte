<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import { TransactionDetail } from '$lib/domains/transactions';
  import type { AdminTransaction } from '$lib/domains/transactions';

  let { transaction, canReveal = true }: { transaction: AdminTransaction; canReveal?: boolean } =
    $props();

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <TransactionDetail {transaction} {canReveal} />
</QueryClientProvider>
