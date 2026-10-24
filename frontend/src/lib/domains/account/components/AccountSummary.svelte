<script lang="ts">
  import { resolve } from '$app/paths';

  import { Badge } from '$lib/components/ui/badge';
  import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { cn } from '$lib/utils';

  import { formatDateTime, formatRemaining, formatSpeedLimit } from '../format';
  import type { Account } from '../schemas/account.schema';

  // Everything here is the backend's answer: `state` decides the badge, and the
  // remaining time is only a rendering of `expires_at` (ADR 0017).
  let { account }: { account: Account } = $props();

  const pkg = $derived(account.package);
  const active = $derived(
    account.active_period.state === 'active' || account.active_period.state === 'expiring',
  );
</script>

<Card>
  <CardHeader class="flex flex-row items-start justify-between gap-3">
    <div>
      <CardTitle>{pkg?.name ?? 'Package'}</CardTitle>
      <p class="text-muted-foreground text-sm">
        Phone Number <span class="font-medium">{account.phone_number}</span>
      </p>
    </div>
    {#if account.active_period.state === 'expiring'}
      <Badge variant="destructive">Hampir berakhir</Badge>
    {:else if account.active_period.state === 'active'}
      <Badge variant="success">Aktif</Badge>
    {:else}
      <Badge variant="destructive">Kedaluwarsa</Badge>
    {/if}
  </CardHeader>

  <CardContent class="space-y-2 text-sm">
    {#if active && account.active_period.expires_at}
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Sisa Active Period</span>
        <span class="font-medium">{formatRemaining(account.active_period.expires_at)}</span>
      </p>
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Berakhir</span>
        <span class="font-medium">{formatDateTime(account.active_period.expires_at)}</span>
      </p>
    {:else if account.active_period.expires_at}
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Berakhir</span>
        <span class="font-medium">{formatDateTime(account.active_period.expires_at)}</span>
      </p>
      <p class="text-muted-foreground">
        Active Period Anda sudah habis. Beli Package baru untuk kembali online.
      </p>
    {/if}

    {#if pkg}
      <p class="flex items-baseline justify-between">
        <span class="text-muted-foreground">Batas kecepatan</span>
        <span class="font-medium">{formatSpeedLimit(pkg.rate_limit)}</span>
      </p>
    {/if}

    {#if account.active_period.state === 'expiring'}
      <p class="text-destructive text-xs" role="status">Active Period Anda hampir berakhir.</p>
    {/if}

    {#if active}
      <p class="text-muted-foreground text-xs">
        Extend menambah durasi Package baru ke sisa Active Period Anda, bukan memulainya dari nol.
      </p>
    {/if}
  </CardContent>

  <CardFooter>
    {#if pkg}
      <a
        class={cn(
          'inline-flex h-10 w-full items-center justify-center rounded-md px-4 text-sm font-medium transition-colors',
          active
            ? 'bg-background hover:bg-muted border'
            : 'bg-primary text-primary-foreground hover:bg-primary/90',
        )}
        href={resolve(`/beli?package=${pkg.id}`)}
      >
        {active ? 'Extend Package' : 'Beli Package baru'}
      </a>
    {/if}
  </CardFooter>
</Card>
