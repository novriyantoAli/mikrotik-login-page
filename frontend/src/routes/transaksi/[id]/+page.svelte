<script lang="ts">
  import { resolve } from '$app/paths';
  import { page } from '$app/state';

  import { TransactionDetail } from '$lib/domains/checkout';

  const idParam = $derived(page.params.id ?? '');
  const id = $derived(/^\d+$/.test(idParam) ? Number(idParam) : null);
</script>

<svelte:head>
  <title>Detail Transaction — Aplikasi Pembelian</title>
</svelte:head>

{#if id === null}
  <div class="bg-background rounded-lg border p-6" role="alert">
    <h2 class="text-lg font-semibold">Transaction tidak ditemukan</h2>
    <p class="text-muted-foreground mt-2 text-sm">
      Nomor Transaction tidak valid. Buka riwayat di Account Status untuk memilih salah satu.
    </p>
    <a class="mt-4 inline-block text-sm underline-offset-4 hover:underline" href={resolve('/akun')}>
      Kembali ke Account Status
    </a>
  </div>
{:else}
  <TransactionDetail {id} />
{/if}
