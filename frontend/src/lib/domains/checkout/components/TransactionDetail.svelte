<script lang="ts">
  import { resolve } from '$app/paths';

  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { historyStateLabel } from '$lib/domains/account';

  import { formatDeadline, formatIDR } from '../format';
  import { createTransactionQuery } from '../queries/checkout.queries';
  import { isTerminalState } from '../schemas/checkout.schema';

  // One past Transaction, read through the same session-authorized endpoint the
  // checkout polls. It deliberately never renders `pin`: the credential belongs to
  // Account Status, which has the show/hide control (ADR 0017).
  let { id }: { id: number } = $props();

  const transaction = createTransactionQuery(() => id);
</script>

<Card>
  {#if transaction.isPending}
    <CardHeader>
      <CardTitle>Memuat Transaction…</CardTitle>
    </CardHeader>
    <CardContent>
      <div class="space-y-3" role="status" aria-live="polite">
        <span class="sr-only">Memuat Transaction</span>
        <div class="bg-muted h-16 w-full animate-pulse rounded"></div>
      </div>
    </CardContent>
  {:else if transaction.isError}
    <CardHeader>
      <CardTitle>Transaction tidak dapat dimuat</CardTitle>
      <p class="text-muted-foreground text-sm">
        {transaction.error?.message ?? 'Transaksi tidak ditemukan atau bukan milik Anda.'}
      </p>
    </CardHeader>
    <CardContent>
      <a
        class="inline-flex h-10 w-full items-center justify-center rounded-md border px-4 text-sm font-medium"
        href={resolve('/akun')}>Kembali ke Account Status</a
      >
    </CardContent>
  {:else if transaction.data}
    {@const item = transaction.data}
    <CardHeader>
      <CardTitle>{item.package_name}</CardTitle>
      <p class="text-muted-foreground text-sm">
        {historyStateLabel(item.state)} · {item.channel_name || item.channel}
      </p>
    </CardHeader>
    <CardContent class="space-y-2 text-sm">
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Jumlah</span>
        <span class="font-semibold">{formatIDR(item.amount_idr)}</span>
      </p>
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Dibuat</span>
        <span class="font-medium">{formatDeadline(item.created_at)}</span>
      </p>
      {#if item.paid_at}
        <p class="flex items-baseline justify-between">
          <span class="text-muted-foreground">Dibayar</span>
          <span class="font-medium">{formatDeadline(item.paid_at)}</span>
        </p>
      {/if}
      {#if !isTerminalState(item.state)}
        <p class="flex items-baseline justify-between">
          <span class="text-muted-foreground">Batas pembayaran</span>
          <span class="font-medium">{formatDeadline(item.expires_at)}</span>
        </p>
      {/if}
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Reference</span>
        <span class="font-mono text-xs">{item.reference}</span>
      </p>
      {#if item.instructions.length > 0 && !isTerminalState(item.state)}
        <div class="space-y-1 pt-2">
          {#each item.instructions as instruction (instruction.title)}
            <p class="font-medium">{instruction.title}</p>
            <ol class="text-muted-foreground list-decimal space-y-1 pl-5 text-xs">
              {#each instruction.steps as step, index (index)}
                <li>{step}</li>
              {/each}
            </ol>
          {/each}
        </div>
      {/if}
      <p class="text-muted-foreground pt-2 text-xs">
        PIN tidak ditampilkan di detail Transaction lama. Buka Account Status untuk melihat PIN yang
        berlaku sekarang.
      </p>
    </CardContent>
    <CardContent>
      <a
        class="inline-flex h-10 w-full items-center justify-center rounded-md border px-4 text-sm font-medium"
        href={resolve('/akun')}>Kembali ke Account Status</a
      >
    </CardContent>
  {/if}
</Card>

{#if transaction.data && !isTerminalState(transaction.data.state)}
  <p class="text-muted-foreground mt-3 text-xs" role="status" aria-live="polite">
    Status diperbarui otomatis sampai pembayaran selesai.
  </p>
{/if}

{#if transaction.data}
  <p class="text-muted-foreground mt-2 text-xs">
    Biaya kanal pembayaran ditagihkan terpisah dari harga Package.
  </p>
{/if}
