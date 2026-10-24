<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '$lib/components/ui/card';

  import { formatChannelFee, formatDeadline, formatIDR } from '../format';
  import { isTerminalState, type Channel, type Transaction } from '../schemas/checkout.schema';

  let {
    transaction,
    channel = undefined,
    onNewTransaction,
  }: {
    transaction: Transaction;
    channel?: Channel;
    onNewTransaction?: () => void;
  } = $props();

  // Everything the buyer must see is derived from the Transaction, never from a
  // client-side calculation: the amount and the deadline are the backend's.
  // `paid` is not the end of the flow: the account is still being provisioned and
  // the PIN arrives with `provisioned` (ADR 0016).
  const provisioned = $derived(transaction.state === 'provisioned');
  const preparing = $derived(transaction.state === 'paid');
  const closed = $derived(transaction.state === 'expired' || transaction.state === 'failed');
  const watching = $derived(!isTerminalState(transaction.state));
</script>

<Card>
  {#if provisioned}
    <CardHeader>
      <CardTitle>Akun hotspot siap dipakai</CardTitle>
      <p class="text-muted-foreground text-sm">
        Pembayaran diterima dan akun hotspot Anda sudah aktif. Masuk ke Login Page dengan nomor dan
        PIN berikut.
      </p>
    </CardHeader>
    <CardContent class="space-y-3 text-sm">
      <div class="bg-muted/60 rounded-lg p-3 text-center">
        <p class="text-muted-foreground text-xs">Phone Number</p>
        <p class="font-mono text-base font-semibold">{transaction.phone_number}</p>
      </div>
      <div class="bg-muted/60 rounded-lg p-3 text-center">
        <p class="text-muted-foreground text-xs">PIN</p>
        <p class="font-mono text-xl font-semibold tracking-[0.3em]">{transaction.pin ?? '—'}</p>
      </div>
      <p class="text-muted-foreground text-xs">
        Langkah berikutnya: buka Login Page hotspot, masuk dengan Phone Number di atas, lalu
        masukkan PIN. PIN juga dikirim lewat SMS dan bisa dilihat lagi di Account Status.
      </p>
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Package</span>
        <span class="font-medium">{transaction.package_name}</span>
      </p>
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Jumlah dibayar</span>
        <span class="font-semibold">{formatIDR(transaction.amount_idr)}</span>
      </p>
    </CardContent>
    {#if transaction.return_url}
      <CardFooter>
        <Button
          class="h-10 w-full"
          type="button"
          onclick={() => window.location.assign(transaction.return_url)}
        >
          Kembali ke Login Page
        </Button>
      </CardFooter>
    {/if}
  {:else if preparing}
    <CardHeader>
      <CardTitle>Pembayaran diterima</CardTitle>
      <p class="text-muted-foreground text-sm">
        Transaction {transaction.reference} sudah lunas
        {transaction.paid_at ? ` pada ${formatDeadline(transaction.paid_at)}` : ''}. Akun hotspot
        dan PIN sedang disiapkan; halaman ini diperbarui otomatis.
      </p>
    </CardHeader>
    <CardContent class="space-y-2 text-sm">
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Package</span>
        <span class="font-medium">{transaction.package_name}</span>
      </p>
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Jumlah dibayar</span>
        <span class="font-semibold">{formatIDR(transaction.amount_idr)}</span>
      </p>
      {#if watching}
        <p class="text-muted-foreground text-xs" role="status" aria-live="polite">
          Menyiapkan akun hotspot… PIN akan muncul di sini dan dikirim lewat SMS.
        </p>
      {/if}
    </CardContent>
  {:else if closed}
    <CardHeader>
      <CardTitle>
        {transaction.state === 'expired' ? 'Waktu pembayaran habis' : 'Pembayaran tidak berhasil'}
      </CardTitle>
      <p class="text-muted-foreground text-sm">
        {transaction.state === 'expired'
          ? 'Batas waktu pembayaran sudah lewat. Buat Transaction baru untuk mencoba lagi.'
          : 'Kanal pembayaran menolak Transaction ini. Buat Transaction baru dengan kanal lain.'}
      </p>
    </CardHeader>
    <CardContent class="space-y-2 text-sm">
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Package</span>
        <span class="font-medium">{transaction.package_name}</span>
      </p>
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Reference</span>
        <span class="font-mono text-xs">{transaction.reference}</span>
      </p>
    </CardContent>
    {#if onNewTransaction}
      <CardFooter>
        <Button class="h-10 w-full" type="button" onclick={onNewTransaction}>
          Buat Transaction baru
        </Button>
      </CardFooter>
    {/if}
  {:else}
    <CardHeader>
      <CardTitle>Selesaikan pembayaran</CardTitle>
      <p class="text-muted-foreground text-sm">
        {transaction.channel_name || transaction.channel} · {transaction.package_name}
      </p>
    </CardHeader>

    <CardContent class="space-y-3 text-sm">
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Jumlah</span>
        <span class="text-base font-semibold">{formatIDR(transaction.amount_idr)}</span>
      </p>
      {#if channel}
        <p class="flex items-baseline justify-between">
          <span class="text-muted-foreground">Biaya kanal</span>
          <span class="font-medium"
            >{formatChannelFee(channel.fee_flat_idr, channel.fee_percent)}</span
          >
        </p>
      {/if}

      {#if transaction.pay_code}
        <div class="bg-muted/60 rounded-lg p-3 text-center">
          <p class="text-muted-foreground text-xs">Kode bayar</p>
          <p class="font-mono text-lg font-semibold tracking-wide">{transaction.pay_code}</p>
        </div>
      {/if}

      {#if transaction.qr_url}
        <div class="flex justify-center">
          <img
            class="h-48 w-48 rounded-md border bg-white p-2"
            src={transaction.qr_url}
            alt="Kode QR pembayaran"
          />
        </div>
      {/if}

      <p class="text-muted-foreground text-xs">
        Bayar sebelum {formatDeadline(transaction.expires_at)}. Status diperbarui otomatis.
      </p>

      {#each transaction.instructions as instruction (instruction.title)}
        <div class="space-y-1">
          <p class="font-medium">{instruction.title}</p>
          <ol class="text-muted-foreground list-decimal space-y-1 pl-5 text-xs">
            {#each instruction.steps as step, index (index)}
              <li>{step}</li>
            {/each}
          </ol>
        </div>
      {/each}

      {#if watching}
        <p class="text-muted-foreground text-xs" role="status" aria-live="polite">
          Menunggu pembayaran. Halaman ini berhenti memperbarui status saat tab tidak terlihat.
        </p>
      {/if}
    </CardContent>
  {/if}
</Card>
