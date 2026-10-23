<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import MagicLinkForm from '$lib/domains/auth/components/MagicLinkForm.svelte';

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <MagicLinkForm packageId={1} returnUrl="http://192.168.88.1/login?mac=aa-bb" />
</QueryClientProvider>
