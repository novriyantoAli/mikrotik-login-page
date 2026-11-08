<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';

  import {
    formatDateTime,
    formatPrice,
    smsStateLabels,
    transactionStateLabels,
    type AdminTransaction,
    type SmsState,
    type TransactionState,
  } from '../schemas/transaction.schema';

  import TransactionDetail from './TransactionDetail.svelte';

  /**
   * The table plus one expanded detail row at a time. The query, the filters, the
   * pagination and the role all belong to TransactionManagement, which owns them;
   * this component renders, and decides which row is open.
   *
   * There is no PIN column: a list never carries a credential. The detail row is the
   * only place a PIN can be asked for, and asking is the audited act (ADR 0027 §4).
   */
  let {
    transactions,
    canReveal = false,
  }: { transactions: AdminTransaction[]; canReveal?: boolean } = $props();

  // One row open at a time: two PIN panels on screen is two credentials to misread.
  let expandedId = $state<number | null>(null);

  function toggle(id: number) {
    expandedId = expandedId === id ? null : id;
  }

  const stateVariants: Record<TransactionState, 'default' | 'success' | 'destructive' | 'outline'> =
    {
      pending: 'outline',
      paid: 'default',
      provisioned: 'success',
      failed: 'destructive',
      expired: 'outline',
    };

  const smsVariants: Record<SmsState, 'default' | 'success' | 'destructive' | 'outline'> = {
    pending: 'outline',
    sending: 'default',
    sent: 'success',
    failed: 'destructive',
  };
</script>

<div class="overflow-x-auto rounded-lg border">
  <table class="w-full min-w-[64rem] text-left text-sm">
    <thead class="bg-muted/60 text-muted-foreground text-xs uppercase">
      <tr>
        <th class="px-3 py-2 font-medium">Waktu (WIB)</th>
        <th class="px-3 py-2 font-medium">Phone Number</th>
        <th class="px-3 py-2 font-medium">Package</th>
        <th class="px-3 py-2 font-medium">Jumlah</th>
        <th class="px-3 py-2 font-medium">Kanal</th>
        <th class="px-3 py-2 font-medium">State</th>
        <th class="px-3 py-2 font-medium">Reference</th>
        <th class="px-3 py-2 font-medium">Percobaan</th>
        <th class="px-3 py-2 font-medium">SMS terakhir</th>
        <th class="px-3 py-2 font-medium">Detail</th>
      </tr>
    </thead>
    <tbody class="divide-y">
      {#each transactions as transaction (transaction.id)}
        <tr>
          <td class="px-3 py-2 whitespace-nowrap">
            <div>{formatDateTime(transaction.created_at)}</div>
            {#if transaction.paid_at}
              <div class="text-muted-foreground text-xs">
                Lunas {formatDateTime(transaction.paid_at)}
              </div>
            {/if}
          </td>
          <td class="px-3 py-2 font-mono text-xs whitespace-nowrap">{transaction.phone_number}</td>
          <td class="px-3 py-2">
            <div class="font-medium">{transaction.package_name}</div>
            <div class="text-muted-foreground text-xs">#{transaction.package_id}</div>
          </td>
          <td class="px-3 py-2 whitespace-nowrap">{formatPrice(transaction.amount_idr)}</td>
          <td class="px-3 py-2 whitespace-nowrap"
            >{transaction.channel_name || transaction.channel}</td
          >
          <td class="px-3 py-2">
            <Badge variant={stateVariants[transaction.state]} size="sm">
              {transactionStateLabels[transaction.state]}
            </Badge>
          </td>
          <td class="px-3 py-2">
            <div class="font-mono text-xs">{transaction.reference}</div>
            {#if transaction.gateway_reference}
              <div class="text-muted-foreground font-mono text-xs">
                {transaction.gateway_reference}
              </div>
            {/if}
          </td>
          <td class="px-3 py-2 tabular-nums">{transaction.provision_attempts}</td>
          <td class="px-3 py-2 whitespace-nowrap">
            {#if transaction.last_sms_state}
              <Badge variant={smsVariants[transaction.last_sms_state]} size="sm">
                {smsStateLabels[transaction.last_sms_state]}
              </Badge>
              <div class="text-muted-foreground text-xs">
                {formatDateTime(transaction.last_sms_at)}
              </div>
            {:else}
              <span class="text-muted-foreground text-xs">Belum ada SMS</span>
            {/if}
          </td>
          <td class="px-3 py-2">
            <Button
              variant="outline"
              aria-expanded={expandedId === transaction.id}
              onclick={() => toggle(transaction.id)}
            >
              {expandedId === transaction.id ? 'Tutup' : 'Detail'}
            </Button>
          </td>
        </tr>
        {#if expandedId === transaction.id}
          <tr>
            <td colspan="10" class="p-0">
              <TransactionDetail {transaction} {canReveal} />
            </td>
          </tr>
        {/if}
      {/each}
    </tbody>
  </table>
</div>
