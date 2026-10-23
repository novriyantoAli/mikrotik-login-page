<script lang="ts">
  import { resolve } from '$app/paths';
  import { page } from '$app/state';

  import { MagicLinkRedeem, invalidLinkMessage } from '$lib/domains/auth';

  const token = page.url.searchParams.get('token') ?? '';
</script>

<svelte:head>
  <title>Masuk — Aplikasi Pembelian</title>
</svelte:head>

{#if token === ''}
  <div class="bg-background rounded-lg border p-6" role="alert">
    <h2 class="text-lg font-semibold">Tautan tidak lengkap</h2>
    <p class="text-muted-foreground mt-2 text-sm">{invalidLinkMessage}</p>
    <a
      class="mt-4 inline-block text-sm underline-offset-4 hover:underline"
      href={resolve('/masuk')}
    >
      Minta tautan masuk baru
    </a>
  </div>
{:else}
  <MagicLinkRedeem {token} />
{/if}
