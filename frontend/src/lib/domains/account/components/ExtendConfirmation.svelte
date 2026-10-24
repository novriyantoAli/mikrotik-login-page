<script lang="ts">
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  import { formatDateTime } from '../format';
  import { createAccountSummaryQuery, createExtendPreviewQuery } from '../queries/account.queries';

  // Shown by the checkout before the buyer pays: buying while the Active Period is
  // still running is an Extend, and the buyer sees the estimate first (PRD point
  // 21, ADR 0017). The estimate is the backend's; provisioning recomputes it with
  // the same rule.
  let { packageId }: { packageId: number } = $props();

  const summary = createAccountSummaryQuery();
  const extending = $derived(
    summary.data?.active_period.state === 'active' ||
      summary.data?.active_period.state === 'expiring',
  );

  const preview = createExtendPreviewQuery(
    () => packageId,
    () => extending,
  );
</script>

{#if extending}
  <Card>
    <CardHeader>
      <CardTitle>Extend Active Period</CardTitle>
      <p class="text-muted-foreground text-sm">
        Active Period Anda masih berjalan. Pembelian ini menambah durasi Package ke sisa masa aktif,
        bukan memulainya dari nol.
      </p>
    </CardHeader>
    <CardContent class="space-y-2 text-sm">
      {#if preview.isPending}
        <p class="text-muted-foreground" role="status" aria-live="polite">
          Menghitung perkiraan Active Period…
        </p>
      {:else if preview.isError}
        <p class="text-muted-foreground text-sm">
          Perkiraan baru belum bisa dihitung. Angka pastinya dihitung saat pembayaran selesai.
        </p>
      {:else if preview.data}
        {#if preview.data.current_expiration}
          <p class="flex items-baseline justify-between">
            <span class="text-muted-foreground">Berakhir sekarang</span>
            <span class="font-medium">{formatDateTime(preview.data.current_expiration)}</span>
          </p>
        {/if}
        <p class="flex items-baseline justify-between">
          <span class="text-muted-foreground">Perkiraan berakhir</span>
          <span class="font-semibold" data-testid="extend-estimate">
            {formatDateTime(preview.data.estimated_expiration)}
          </span>
        </p>
        <p class="text-muted-foreground text-xs">
          Ini perkiraan. Angka final dihitung backend saat akun hotspot disiapkan.
        </p>
      {/if}
    </CardContent>
  </Card>
{/if}
