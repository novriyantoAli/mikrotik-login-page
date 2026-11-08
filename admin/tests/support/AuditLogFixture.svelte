<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import { AuditLog } from '$lib/domains/audit';

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  });
</script>

<QueryClientProvider {client}>
  <AuditLog />
</QueryClientProvider>
