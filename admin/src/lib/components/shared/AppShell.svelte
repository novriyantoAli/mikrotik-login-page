<script lang="ts">
  import { resolve } from '$app/paths';
  import { useQueryClient } from '@tanstack/svelte-query';
  import { onDestroy, type Snippet } from 'svelte';

  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';
  import {
    LoginForm,
    authKeys,
    createLogoutMutation,
    createSessionQuery,
    roleLabels,
    sessionExpiredMessage,
    signedOutMessage,
    watchSessionExpiry,
  } from '$lib/domains/auth';

  /**
   * The panel's shell: one session read decides whether the operator sees the
   * app or the login form. A 401 from any other screen brings them back here
   * with a message instead of leaving a dead page behind (ADR 0028).
   */
  let { children }: { children?: Snippet } = $props();

  const queryClient = useQueryClient();
  const sessionQuery = createSessionQuery();
  const logoutMutation = createLogoutMutation();

  let notice = $state('');

  // A notice belongs to the state that produced it: once a session is live
  // again (a successful login), the old "session expired" line is not true.
  $effect(() => {
    if (sessionQuery.data) {
      notice = '';
    }
  });

  // A request that needed the session answered 401: forget it and show the form.
  const stopWatching = watchSessionExpiry(() => {
    notice = sessionExpiredMessage;
    queryClient.setQueryData(authKeys.session(), null);
  });
  onDestroy(stopWatching);

  function signOut() {
    notice = '';
    logoutMutation.mutate(undefined, {
      onSuccess: () => {
        notice = signedOutMessage;
      },
    });
  }
</script>

{#if sessionQuery.isPending}
  <div class="mx-auto w-full max-w-md space-y-3 px-4 py-16" role="status" aria-live="polite">
    <span class="sr-only">Memeriksa sesi admin</span>
    <div class="bg-muted h-10 animate-pulse rounded-md"></div>
    <div class="bg-muted h-32 animate-pulse rounded-md"></div>
  </div>
{:else if sessionQuery.isError}
  <div class="mx-auto w-full max-w-md space-y-3 px-4 py-16" role="alert">
    <p class="text-sm">{sessionQuery.error?.message ?? 'Sesi admin tidak dapat diperiksa.'}</p>
    <Button variant="outline" onclick={() => sessionQuery.refetch()}>Coba lagi</Button>
  </div>
{:else if !sessionQuery.data}
  <div class="mx-auto flex min-h-dvh w-full max-w-3xl flex-col justify-center gap-6 px-4 py-10">
    <div class="space-y-1 text-center">
      <p class="text-muted-foreground text-xs tracking-wide uppercase">Hotspot</p>
      <h1 class="text-2xl font-semibold">Panel Admin</h1>
    </div>
    <LoginForm {notice} />
  </div>
{:else}
  {@const session = sessionQuery.data}
  <div class="bg-muted/40 flex min-h-dvh flex-col">
    <header class="bg-background border-b">
      <div class="mx-auto flex w-full max-w-6xl flex-wrap items-center gap-4 px-4 py-3">
        <div class="mr-auto flex items-center gap-3">
          <span class="text-sm font-semibold">Panel Admin</span>
          <nav class="text-muted-foreground flex items-center gap-2 text-sm">
            <a class="hover:text-foreground underline-offset-4 hover:underline" href={resolve('/')}>
              Beranda
            </a>
            <a
              class="hover:text-foreground underline-offset-4 hover:underline"
              href={resolve('/paket')}
            >
              Package
            </a>
            {#if session.role === 'owner'}
              <a
                class="hover:text-foreground underline-offset-4 hover:underline"
                href={resolve('/admin')}
              >
                Admin
              </a>
            {/if}
          </nav>
        </div>

        <div class="flex items-center gap-3 text-sm">
          <span class="text-muted-foreground hidden sm:inline">{session.email}</span>
          <Badge variant="outline" size="sm">{roleLabels[session.role]}</Badge>
          <Button variant="outline" disabled={logoutMutation.isPending} onclick={signOut}>
            {logoutMutation.isPending ? 'Keluar…' : 'Keluar'}
          </Button>
        </div>
      </div>
    </header>

    <main class="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
      {@render children?.()}
    </main>

    <footer class="text-muted-foreground mx-auto w-full max-w-6xl px-4 pb-8 text-xs">
      Panel Admin mengubah katalog dan membuka PIN. Setiap tindakan tercatat atas nama Anda.
    </footer>
  </div>
{/if}
