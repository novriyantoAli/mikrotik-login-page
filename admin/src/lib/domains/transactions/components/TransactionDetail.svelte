<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';

  import PINReveal from './PINReveal.svelte';
  import { formatDateTimeWIB } from '$lib/utils';

  import {
    formatPrice,
    smsStateLabels,
    transactionStateLabels,
    type AdminTransaction,
  } from '../schemas/transaction.schema';

  /**
   * The expanded row: the fields an operator reads while answering a call, plus the
   * audited PIN control. The list itself stays credential-free; this component is the
   * only place a PIN can appear, and it appears only when support asks for it.
   */
  let {
    transaction,
    canReveal,
  }: {
    transaction: AdminTransaction;
    canReveal: boolean;
  } = $props();
</script>

<div class="bg-muted/30 space-y-4 px-4 py-4" data-testid="transaction-detail">
  <div class="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
    <div>
      <p class="text-muted-foreground text-xs uppercase">Reference</p>
      <p class="font-mono text-xs">{transaction.reference}</p>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">Reference gateway</p>
      <p class="font-mono text-xs">{transaction.gateway_reference || '—'}</p>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">Kanal</p>
      <p>{transaction.channel_name || transaction.channel}</p>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">Snapshot katalog</p>
      <p>{transaction.package_name} — {formatPrice(transaction.amount_idr)}</p>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">Dibuat</p>
      <p>{formatDateTimeWIB(transaction.created_at)}</p>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">Lunas</p>
      <p>{formatDateTimeWIB(transaction.paid_at)}</p>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">Kedaluwarsa</p>
      <p>{formatDateTimeWIB(transaction.expires_at)}</p>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">Percobaan provisioning</p>
      <p class="tabular-nums">{transaction.provision_attempts}</p>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">State</p>
      <Badge variant="outline" size="sm">{transactionStateLabels[transaction.state]}</Badge>
    </div>
    <div>
      <p class="text-muted-foreground text-xs uppercase">SMS PIN terakhir</p>
      {#if transaction.last_sms_state}
        <p>
          {smsStateLabels[transaction.last_sms_state]} —
          {formatDateTimeWIB(transaction.last_sms_at)}
        </p>
      {:else}
        <p class="text-muted-foreground">Belum ada SMS</p>
      {/if}
    </div>
  </div>

  <div class="border-t pt-3">
    {#if canReveal}
      {#if transaction.state === 'provisioned'}
        <PINReveal transactionId={transaction.id} reference={transaction.reference} />
      {:else}
        <p class="text-muted-foreground text-sm">
          PIN baru ada setelah Transaction selesai provisioning (state sekarang:
          {transactionStateLabels[transaction.state]}).
        </p>
      {/if}
    {:else}
      <p class="text-muted-foreground text-sm">
        Peran Anda tidak boleh membuka PIN. Minta owner atau support untuk membukanya.
      </p>
    {/if}
  </div>
</div>
