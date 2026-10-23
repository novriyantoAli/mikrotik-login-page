<script lang="ts">
  import { resolve } from '$app/paths';
  import { page } from '$app/state';

  import { CheckoutFlow } from '$lib/domains/checkout';

  // The Login Page links here as `?package=<id>&return=<Login Page URL>`; the
  // backend re-validates both the Package and the return target.
  const packageParam = page.url.searchParams.get('package');
  const packageId =
    packageParam !== null && /^\d+$/.test(packageParam) ? Number(packageParam) : null;
  const returnUrl = page.url.searchParams.get('return') ?? '';
</script>

<svelte:head>
  <title>Pembelian — Aplikasi Pembelian</title>
</svelte:head>

{#if packageId === null}
  <div class="bg-background rounded-lg border p-6" role="alert">
    <h2 class="text-lg font-semibold">Package belum dipilih</h2>
    <p class="text-muted-foreground mt-2 text-sm">
      Buka halaman ini dari tombol Beli di Login Page supaya Package yang Anda pilih ikut terbawa.
    </p>
    <a class="mt-4 inline-block text-sm underline-offset-4 hover:underline" href={resolve('/')}>
      Kembali
    </a>
  </div>
{:else}
  <CheckoutFlow {packageId} {returnUrl} />
{/if}
