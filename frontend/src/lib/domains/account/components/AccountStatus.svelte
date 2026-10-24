<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { MagicLinkForm, createSessionQuery } from '$lib/domains/auth';

  import { createAccountSummaryQuery } from '../queries/account.queries';
  import { createAccountState } from '../state/account.state.svelte';
  import AccountSummary from './AccountSummary.svelte';
  import EmptyAccount from './EmptyAccount.svelte';
  import PinCard from './PinCard.svelte';
  import TransactionHistory from './TransactionHistory.svelte';

  // The session is the only authorization for this page: there is no Phone Number
  // to type, and a lost SMS is answered with a new Magic Link (PRD point 14).
  const session = createSessionQuery();
  const state = createAccountState();
  const summary = createAccountSummaryQuery(() => session.data != null);

  const account = $derived(summary.data);
  const hasActivePeriod = $derived(
    account !== null &&
      account !== undefined &&
      account.active_period.found &&
      account.active_period.state !== 'none' &&
      account.package !== null,
  );
</script>

{#if session.isPending}
  <Card>
    <CardHeader>
      <CardTitle>Memeriksa sesi…</CardTitle>
      <p class="text-muted-foreground text-sm">Sebentar, kami memeriksa apakah Anda sudah masuk.</p>
    </CardHeader>
    <CardContent>
      <div class="space-y-3" role="status" aria-live="polite">
        <span class="sr-only">Memeriksa sesi</span>
        <div class="bg-muted h-4 w-2/3 animate-pulse rounded"></div>
        <div class="bg-muted h-4 w-1/2 animate-pulse rounded"></div>
      </div>
    </CardContent>
  </Card>
{:else if session.isError}
  <Card>
    <CardHeader>
      <CardTitle>Sesi tidak dapat diperiksa</CardTitle>
      <p class="text-muted-foreground text-sm">
        {session.error?.message ?? 'Periksa koneksi Anda lalu coba lagi.'}
      </p>
    </CardHeader>
    <CardContent>
      <Button class="h-10 w-full" type="button" variant="outline" onclick={() => session.refetch()}>
        Coba lagi
      </Button>
    </CardContent>
  </Card>
{:else if session.data === null}
  <div class="space-y-3">
    <Card>
      <CardHeader>
        <CardTitle>Sesi tidak ditemukan atau sudah berakhir</CardTitle>
        <p class="text-muted-foreground text-sm">
          Account Status hanya bisa dibuka dengan sesi aktif. Minta Magic Link baru lewat SMS; Anda
          tidak perlu PIN untuk masuk.
        </p>
      </CardHeader>
    </Card>
    <MagicLinkForm />
  </div>
{:else if summary.isPending}
  <Card>
    <CardHeader>
      <CardTitle>Memuat Account Status…</CardTitle>
    </CardHeader>
    <CardContent>
      <div class="space-y-3" role="status" aria-live="polite">
        <span class="sr-only">Memuat Account Status</span>
        <div class="bg-muted h-16 w-full animate-pulse rounded"></div>
        <div class="bg-muted h-16 w-full animate-pulse rounded"></div>
      </div>
    </CardContent>
  </Card>
{:else if summary.isError}
  <Card>
    <CardHeader>
      <CardTitle>Account Status tidak dapat dimuat</CardTitle>
      <p class="text-muted-foreground text-sm">
        {summary.error?.message ?? 'Periksa koneksi Anda lalu coba lagi.'}
      </p>
    </CardHeader>
    <CardContent>
      <Button class="h-10 w-full" type="button" variant="outline" onclick={() => summary.refetch()}>
        Coba lagi
      </Button>
    </CardContent>
  </Card>
{:else if account}
  <div class="space-y-4">
    {#if hasActivePeriod}
      <AccountSummary {account} />
      {#if account.pin}
        <PinCard pin={account.pin} visible={state.pinVisible} onToggle={() => state.togglePIN()} />
      {/if}
    {:else}
      <EmptyAccount />
    {/if}

    <TransactionHistory />
  </div>
{/if}
