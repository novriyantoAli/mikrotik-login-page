<script lang="ts">
  import { page } from '$app/state';

  import { MagicLinkForm } from '$lib/domains/auth';

  // The Login Page links to `?package=<id>&return=<Login Page URL>`. Both are
  // hints only: the backend re-validates the Package and the return target.
  const packageParam = page.url.searchParams.get('package');
  const packageId =
    packageParam !== null && /^\d+$/.test(packageParam) ? Number(packageParam) : undefined;
  const returnUrl = page.url.searchParams.get('return') ?? undefined;
</script>

<svelte:head>
  <title>Masuk — Aplikasi Pembelian</title>
</svelte:head>

<MagicLinkForm {packageId} {returnUrl} />
