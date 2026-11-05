<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';

  import AdminCreateForm from '$lib/domains/admins/components/AdminCreateForm.svelte';

  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
</script>

<QueryClientProvider {client}>
  <AdminCreateForm />
</QueryClientProvider>
