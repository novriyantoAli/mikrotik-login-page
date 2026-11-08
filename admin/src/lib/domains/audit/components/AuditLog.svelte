<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import EmptyState from '$lib/components/shared/EmptyState.svelte';

  import AuditFilters from './AuditFilters.svelte';
  import AuditList from './AuditList.svelte';
  import { createAuditListQuery } from '../queries/audit.queries';
  import { createAuditFilterState, type AuditSearch } from '../state/audit.state.svelte';

  /**
   * The audit reader screen: one query for the page and the three states a list must
   * handle (loading, error, empty). It owns all of that so the route stays a thin
   * adapter (skill §7/§11).
   *
   * The role check lives in the route, and the backend refuses the request anyway:
   * hiding a screen is not authorisation (ADR 0024 §3).
   */
  const filters = createAuditFilterState();
  const query = createAuditListQuery(() => filters.filter);

  const page = $derived(query.data?.page ?? filters.page);
  const pageSize = $derived(query.data?.page_size ?? filters.pageSize);
  const total = $derived(query.data?.total_count ?? 0);
  const rowCount = $derived(query.data?.data.length ?? 0);
  const firstRow = $derived(rowCount === 0 ? 0 : (page - 1) * pageSize + 1);
  const lastRow = $derived(firstRow + rowCount - 1);
  const hasMore = $derived(rowCount > 0 && lastRow < total);
  const filtered = $derived(
    Boolean(filters.adminEmail || filters.action || filters.from || filters.to),
  );

  function applySearch(search: AuditSearch) {
    // Pressing "Cari" on the search already on screen must still refresh: Svelte skips
    // an equal assignment, so the query key would not change and the cached page
    // would be served without a request.
    const unchanged = filters.page === 1 && filters.matches(search);
    filters.apply(search);
    if (unchanged) {
      void query.refetch();
    }
  }
</script>

<div class="space-y-4">
  <AuditFilters
    adminEmail={filters.adminEmail}
    action={filters.action}
    from={filters.from}
    to={filters.to}
    busy={query.isFetching}
    onApply={applySearch}
    onReset={() => filters.reset()}
  />

  {#if query.isPending}
    <div class="space-y-2" role="status" aria-live="polite">
      <span class="sr-only">Memuat jejak audit</span>
      {#each [0, 1, 2, 3, 4] as row (row)}
        <div class="bg-muted h-11 animate-pulse rounded-md"></div>
      {/each}
    </div>
  {:else if query.isError}
    <div class="space-y-3" role="alert">
      <p class="text-sm">{query.error?.message ?? 'Jejak audit tidak dapat dimuat.'}</p>
      <Button variant="outline" onclick={() => query.refetch()}>Coba lagi</Button>
    </div>
  {:else if query.data}
    {#if rowCount === 0}
      <EmptyState
        title={filtered ? 'Tidak ada baris audit yang cocok' : 'Belum ada jejak audit'}
        description={filtered
          ? 'Ubah atau kosongkan filter untuk melihat jejak yang lebih luas.'
          : 'Jejak muncul di sini begitu seorang admin mengubah katalog atau membuka PIN.'}
      />
    {:else}
      <AuditList entries={query.data.data} />
    {/if}

    {#if rowCount > 0 || page > 1}
      <div class="flex flex-wrap items-center justify-between gap-3 text-sm">
        {#if rowCount > 0}
          <p class="text-muted-foreground">
            Menampilkan {firstRow}–{lastRow} dari {total} baris audit
          </p>
        {:else}
          <p class="text-muted-foreground">
            Halaman ini kosong; hasilnya ada di halaman sebelumnya.
          </p>
        {/if}
        <div class="flex gap-2">
          <Button
            variant="outline"
            disabled={page <= 1 || query.isFetching}
            onclick={() => filters.goToPage(page - 1)}
          >
            Sebelumnya
          </Button>
          <Button
            variant="outline"
            disabled={!hasMore || query.isFetching}
            onclick={() => filters.goToPage(page + 1)}
          >
            Berikutnya
          </Button>
        </div>
      </div>
    {/if}
  {/if}
</div>
