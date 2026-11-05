<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
  import type { Snippet } from 'svelte';

  import '../app.css';
  import AppShell from '$lib/components/shared/AppShell.svelte';

  let { children }: { children?: Snippet } = $props();

  // One client for the whole panel. The app is a consumer of server state only
  // (ADR 0028): no session is held here, the backend owns the cookie.
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: 1, refetchOnWindowFocus: false },
    },
  });
</script>

<QueryClientProvider client={queryClient}>
  <AppShell>
    {@render children?.()}
  </AppShell>
</QueryClientProvider>
