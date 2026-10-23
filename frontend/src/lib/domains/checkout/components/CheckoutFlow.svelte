<script lang="ts">
  import { resolve } from '$app/paths';

  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { MagicLinkForm, createSessionQuery } from '$lib/domains/auth';
  import { PackageSummary, createPackageListQuery, findPackage } from '$lib/domains/packages';

  import {
    createChannelsQuery,
    createCreateTransactionMutation,
    createTransactionQuery,
  } from '../queries/checkout.queries';
  import { createCheckoutState } from '../state/checkout.state.svelte';
  import ChannelList from './ChannelList.svelte';
  import PaymentInstructions from './PaymentInstructions.svelte';

  let { packageId, returnUrl = '' }: { packageId: number; returnUrl?: string } = $props();

  // UI-only choices live in this screen's own state; everything from the API is a
  // TanStack query (frontend skill §3).
  const state = createCheckoutState();

  const session = createSessionQuery();
  const packagesQuery = createPackageListQuery();
  const channelsQuery = createChannelsQuery();
  const createTransaction = createCreateTransactionMutation();
  const transactionQuery = createTransactionQuery(() => state.transactionId);

  const pkg = $derived(packagesQuery.data ? findPackage(packagesQuery.data, packageId) : undefined);
  const activeChannel = $derived(
    channelsQuery.data?.find((channel) => channel.code === state.channel),
  );
  const transaction = $derived(transactionQuery.data ?? createTransaction.data);

  function pay() {
    if (!state.channel) {
      return;
    }

    createTransaction.mutate(
      {
        package_id: packageId,
        channel: state.channel,
        return_url: returnUrl === '' ? undefined : returnUrl,
      },
      {
        onSuccess: (created) => {
          state.watch(created.id);
        },
      },
    );
  }

  function startNewTransaction() {
    state.clearTransaction();
    createTransaction.reset();
  }
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
{:else if session.data === null}
  <div class="space-y-3">
    <p class="text-muted-foreground text-sm">
      Masuk dulu dengan Phone Number supaya pembelian ini tercatat atas nama Anda.
    </p>
    <MagicLinkForm {packageId} {returnUrl} />
  </div>
{:else}
  <div class="space-y-4">
    {#if packagesQuery.isPending}
      <div class="bg-background space-y-3 rounded-lg border p-4" role="status" aria-live="polite">
        <span class="sr-only">Memuat Package</span>
        <div class="bg-muted h-5 w-1/2 animate-pulse rounded"></div>
        <div class="bg-muted h-4 w-2/3 animate-pulse rounded"></div>
      </div>
    {:else if packagesQuery.isError}
      <Card>
        <CardHeader>
          <CardTitle>Package tidak dapat dimuat</CardTitle>
          <p class="text-muted-foreground text-sm">
            {packagesQuery.error?.message ?? 'Periksa koneksi Anda lalu coba lagi.'}
          </p>
        </CardHeader>
        <CardFooter>
          <Button
            class="h-10 w-full"
            type="button"
            variant="outline"
            onclick={() => packagesQuery.refetch()}
          >
            Coba lagi
          </Button>
        </CardFooter>
      </Card>
    {:else if !pkg}
      <Card>
        <CardHeader>
          <CardTitle>Package tidak ditemukan</CardTitle>
          <p class="text-muted-foreground text-sm">
            Package yang Anda pilih tidak ada di katalog. Kembali ke Login Page lalu pilih ulang.
          </p>
        </CardHeader>
        <CardFooter>
          <a class="text-sm underline-offset-4 hover:underline" href={resolve('/')}>Kembali</a>
        </CardFooter>
      </Card>
    {:else}
      <PackageSummary {pkg} />

      {#if transaction}
        <PaymentInstructions
          {transaction}
          channel={activeChannel}
          onNewTransaction={startNewTransaction}
        />
      {:else if channelsQuery.isPending}
        <div class="bg-background space-y-3 rounded-lg border p-4" role="status" aria-live="polite">
          <span class="sr-only">Memuat kanal pembayaran</span>
          <div class="bg-muted h-10 w-full animate-pulse rounded"></div>
          <div class="bg-muted h-10 w-full animate-pulse rounded"></div>
        </div>
      {:else if channelsQuery.isError}
        <Card>
          <CardHeader>
            <CardTitle>Kanal pembayaran tidak dapat dimuat</CardTitle>
            <p class="text-muted-foreground text-sm">
              {channelsQuery.error?.message ?? 'Periksa koneksi Anda lalu coba lagi.'}
            </p>
          </CardHeader>
          <CardFooter>
            <Button
              class="h-10 w-full"
              type="button"
              variant="outline"
              onclick={() => channelsQuery.refetch()}
            >
              Coba lagi
            </Button>
          </CardFooter>
        </Card>
      {:else}
        <Card>
          <CardContent class="space-y-4 pt-6">
            <ChannelList
              channels={channelsQuery.data ?? []}
              selected={state.channel}
              disabled={createTransaction.isPending}
              onSelect={(code) => state.selectChannel(code)}
            />

            {#if createTransaction.isError}
              <p class="text-destructive text-sm" role="alert">
                {createTransaction.error?.message ?? 'Pembayaran tidak dapat dimulai. Coba lagi.'}
              </p>
            {/if}
          </CardContent>
          <CardFooter>
            <Button
              class="h-10 w-full"
              type="button"
              disabled={state.channel === null || createTransaction.isPending}
              onclick={pay}
            >
              {createTransaction.isPending ? 'Menyiapkan pembayaran…' : 'Lanjut ke pembayaran'}
            </Button>
          </CardFooter>
        </Card>
      {/if}
    {/if}
  </div>
{/if}
