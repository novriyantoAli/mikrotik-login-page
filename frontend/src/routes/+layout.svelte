<script lang="ts">
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
  import type { Snippet } from 'svelte';

  import '../app.css';

  let { children }: { children?: Snippet } = $props();

  // One client for the whole SPA. The app is a consumer of server state only
  // (ADR 0013): no session is held here, the backend owns the cookie.
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: 1, refetchOnWindowFocus: false },
    },
  });
</script>

<QueryClientProvider client={queryClient}>
  <div class="bg-muted/40 flex min-h-dvh flex-col">
    <header class="bg-background border-b">
      <div class="mx-auto flex w-full max-w-3xl flex-col gap-1 px-4 py-4">
        <p class="text-muted-foreground text-xs tracking-wide uppercase">Hotspot</p>
        <h1 class="text-lg font-semibold">Aplikasi Pembelian</h1>
      </div>
    </header>

    <main class="mx-auto w-full max-w-3xl flex-1 px-4 py-6">
      {@render children?.()}
    </main>

    <footer class="text-muted-foreground mx-auto w-full max-w-3xl px-4 pb-8 text-xs">
      Aplikasi dan API disajikan dari satu alamat yang sama.
    </footer>
  </div>
</QueryClientProvider>
