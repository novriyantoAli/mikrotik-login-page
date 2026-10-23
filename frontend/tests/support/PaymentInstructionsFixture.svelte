<script lang="ts">
  import PaymentInstructions from '$lib/domains/checkout/components/PaymentInstructions.svelte';
  import type { Channel, Transaction } from '$lib/domains/checkout';

  let {
    transaction,
    channel = undefined,
    onNewTransaction = undefined,
  }: {
    transaction: Transaction;
    channel?: Channel;
    onNewTransaction?: () => void;
  } = $props();
</script>

<PaymentInstructions {transaction} {channel} {onNewTransaction} />
