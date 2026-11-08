<script lang="ts">
  import { Button } from '$lib/components/ui/button';

  import { createRevealPINMutation } from '../queries/transaction.queries';

  /**
   * The audited way a buyer's PIN is read back to support (ADR 0027 §4). It is a
   * mutation, not a query: the digits live in this component's own state and nowhere
   * else — not in the query cache, not in a URL, not in the list (skill §14). Collapsing
   * the row destroys this component, so the value goes with it.
   */
  let { transactionId, reference }: { transactionId: number; reference: string } = $props();

  const reveal = createRevealPINMutation();

  let pin = $state('');
  let visible = $state(false);

  function open() {
    reveal.mutate(transactionId, {
      onSuccess: (value) => {
        pin = value;
        visible = true;
      },
    });
  }

  function forget() {
    pin = '';
    visible = false;
    reveal.reset();
  }
</script>

<div class="space-y-3">
  <div class="space-y-1">
    <p class="text-sm font-medium">PIN pembeli</p>
    <p class="text-muted-foreground text-xs">
      Membuka PIN tercatat di audit log: waktu, admin yang membukanya, dan Transaction
      <span class="font-mono">{reference}</span>. Tidak ada PIN di daftar, dan setiap pembukaan
      dihitung terhadap kuota Anda.
    </p>
  </div>

  {#if reveal.isPending}
    <Button variant="outline" disabled>Membuka PIN…</Button>
  {:else if reveal.isError}
    <div class="space-y-2" role="alert">
      <p class="text-sm">{reveal.error?.message ?? 'PIN tidak dapat dibuka.'}</p>
      <Button variant="outline" onclick={open}>Coba lagi</Button>
    </div>
  {:else if pin}
    <div class="flex flex-wrap items-center gap-3">
      <span
        class="bg-muted rounded-md px-3 py-1 font-mono text-lg tracking-[0.3em] tabular-nums"
        data-testid="revealed-pin"
      >
        {visible ? pin : '••••••'}
      </span>
      <Button variant="outline" onclick={() => (visible = !visible)}>
        {visible ? 'Sembunyikan' : 'Tampilkan'}
      </Button>
      <Button variant="outline" onclick={forget}>Tutup</Button>
      <Button variant="outline" onclick={open}>Buka ulang</Button>
    </div>
    <p class="text-muted-foreground text-xs">
      Sampaikan PIN ini hanya kepada pemilik Phone Number.
    </p>
  {:else}
    <Button onclick={open} disabled={reveal.isPending}>Buka PIN</Button>
  {/if}
</div>
