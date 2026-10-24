<script lang="ts">
  import { resolve } from '$app/paths';

  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  import { formatDateTime, formatIDR, historyStateLabel } from '../format';
  import { createTransactionHistoryQuery } from '../queries/account.queries';

  // The history has its own component so its query only runs once there is a
  // session to authorize it (AccountStatus renders it behind the session branch).
  // The open page is this component's own UI state, not the screen's.
  const pageSize = 5;
  let page = $state(1);
  const history = createTransactionHistoryQuery(() => page, pageSize);

  const totalCount = $derived(history.data?.total_count ?? 0);
  const hasPrevious = $derived(page > 1);
  const hasNext = $derived(page * pageSize < totalCount);
</script>

<Card>
  <CardHeader>
    <CardTitle>Riwayat Transaction</CardTitle>
    <p class="text-muted-foreground text-sm">
      Pembelian Package Anda, terbaru lebih dulu. Buka satu baris untuk melihat detailnya.
    </p>
  </CardHeader>

  <CardContent class="space-y-3">
    {#if history.isPending}
      <div class="space-y-3" role="status" aria-live="polite">
        <span class="sr-only">Memuat riwayat Transaction</span>
        <div class="bg-muted h-12 w-full animate-pulse rounded"></div>
        <div class="bg-muted h-12 w-full animate-pulse rounded"></div>
      </div>
    {:else if history.isError}
      <p class="text-destructive text-sm" role="alert">
        {history.error?.message ?? 'Riwayat tidak dapat dimuat.'}
      </p>
      <Button class="h-10 w-full" type="button" variant="outline" onclick={() => history.refetch()}>
        Coba lagi
      </Button>
    {:else if (history.data?.data ?? []).length === 0}
      <p class="text-muted-foreground text-sm">Belum ada Transaction.</p>
    {:else}
      <ul class="space-y-2">
        {#each history.data?.data ?? [] as item (item.id)}
          <li>
            <a
              class="hover:bg-muted/40 flex flex-col gap-1 rounded-lg border p-3 transition-colors"
              href={resolve('/transaksi/[id]', { id: String(item.id) })}
            >
              <span class="flex items-baseline justify-between gap-3">
                <span class="font-medium">{item.package_name}</span>
                <span class="font-semibold">{formatIDR(item.amount_idr)}</span>
              </span>
              <span class="flex items-center justify-between gap-3 text-xs">
                <span class="text-muted-foreground">{formatDateTime(item.created_at)}</span>
                <Badge variant={item.state === 'provisioned' ? 'success' : 'outline'}>
                  {historyStateLabel(item.state)}
                </Badge>
              </span>
            </a>
          </li>
        {/each}
      </ul>

      {#if hasPrevious || hasNext}
        <div class="flex items-center justify-between gap-3 pt-1">
          <Button
            variant="outline"
            type="button"
            disabled={!hasPrevious}
            onclick={() => (page = Math.max(1, page - 1))}
          >
            Sebelumnya
          </Button>
          <span class="text-muted-foreground text-xs">
            Halaman {page} dari {Math.max(1, Math.ceil(totalCount / pageSize))}
          </span>
          <Button
            variant="outline"
            type="button"
            disabled={!hasNext}
            onclick={() => {
              if (page * pageSize < totalCount) {
                page += 1;
              }
            }}
          >
            Berikutnya
          </Button>
        </div>
      {/if}
    {/if}
  </CardContent>
</Card>
