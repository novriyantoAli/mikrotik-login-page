<script lang="ts">
  import { resolve } from '$app/paths';
  import { onMount } from 'svelte';

  import { Button } from '$lib/components/ui/button';
  import {
    Card,
    CardContent,
    CardDescription,
    CardFooter,
    CardHeader,
    CardTitle,
  } from '$lib/components/ui/card';

  import { isRetriableAuthError } from '../api/auth.api';
  import { createExchangeMagicLinkMutation } from '../queries/auth.queries';
  import { invalidLinkMessage } from '../schemas/auth.schema';

  let { token }: { token: string } = $props();

  const mutation = createExchangeMagicLinkMutation();

  // The link is redeemed exactly once, as soon as the screen loads: the token is
  // single-use, so a second attempt would fail anyway.
  onMount(() => {
    mutation.mutate(token, {
      // The token is spent; keeping it in the address bar would leave a used
      // credential in history for no benefit.
      onSuccess: () => history.replaceState(null, '', resolve('/masuk/tautan')),
    });
  });

  // A checkout link lands on the checkout with the Package it carried; anything
  // else honours the Login Page the backend already validated into return_url.
  const continueHref = $derived.by(() => {
    const result = mutation.data;
    if (!result) {
      return '/';
    }
    if (result.purpose === 'checkout' && result.package_id) {
      const query = [`package=${encodeURIComponent(String(result.package_id))}`];
      if (result.return_url) {
        query.push(`return=${encodeURIComponent(result.return_url)}`);
      }
      return `${resolve('/beli')}?${query.join('&')}`;
    }
    return result.return_url || '/';
  });
  const canRetry = $derived(mutation.isError && isRetriableAuthError(mutation.error));
</script>

<Card>
  {#if mutation.isPending}
    <CardHeader>
      <CardTitle>Memeriksa tautan…</CardTitle>
      <CardDescription>Sebentar, kami sedang membuka sesi Anda.</CardDescription>
    </CardHeader>
    <CardContent>
      <div class="space-y-3" role="status" aria-live="polite">
        <span class="sr-only">Memeriksa tautan masuk</span>
        <div class="bg-muted h-4 w-2/3 animate-pulse rounded"></div>
        <div class="bg-muted h-4 w-1/2 animate-pulse rounded"></div>
      </div>
    </CardContent>
  {:else if mutation.isError}
    <CardHeader>
      <CardTitle>Tautan tidak bisa dipakai</CardTitle>
      <CardDescription>{mutation.error?.message ?? invalidLinkMessage}</CardDescription>
    </CardHeader>
    <CardContent class="text-muted-foreground space-y-2 text-sm">
      <p>Tautan hanya berlaku sekali dan punya batas waktu.</p>
    </CardContent>
    <CardFooter class="justify-between gap-3">
      {#if canRetry}
        <Button variant="outline" type="button" onclick={() => mutation.mutate(token)}>
          Coba lagi
        </Button>
      {:else}
        <span></span>
      {/if}
      <a class="text-sm underline-offset-4 hover:underline" href={resolve('/masuk')}
        >Minta tautan baru</a
      >
    </CardFooter>
  {:else if mutation.data}
    <CardHeader>
      <CardTitle>Berhasil masuk</CardTitle>
      <CardDescription>
        Sesi aktif untuk <span class="font-medium">{mutation.data.phone_number}</span>.
      </CardDescription>
    </CardHeader>
    <CardContent class="text-muted-foreground text-sm">
      <p>Jangan tutup halaman ini bila Anda sedang membeli Package.</p>
    </CardContent>
    <CardFooter>
      <Button
        class="h-10 w-full"
        type="button"
        onclick={() => window.location.assign(continueHref)}
      >
        Lanjut
      </Button>
    </CardFooter>
  {/if}
</Card>
