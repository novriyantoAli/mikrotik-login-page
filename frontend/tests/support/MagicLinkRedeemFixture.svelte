<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import MagicLinkRedeem from '$lib/domains/auth/components/MagicLinkRedeem.svelte';

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <MagicLinkRedeem token="token-from-sms" />
</QueryClientProvider>
