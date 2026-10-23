<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import CheckoutFlow from '$lib/domains/checkout/components/CheckoutFlow.svelte';

  let { packageId = 1, returnUrl = '' }: { packageId?: number; returnUrl?: string } = $props();

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <CheckoutFlow {packageId} {returnUrl} />
</QueryClientProvider>
