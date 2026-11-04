<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import AppShell from '$lib/components/shared/AppShell.svelte';

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <AppShell>
    <p>Konten halaman</p>
  </AppShell>
</QueryClientProvider>
