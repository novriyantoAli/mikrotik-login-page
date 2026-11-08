<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import EmptyState from '$lib/components/shared/EmptyState.svelte';
  import { createSessionQuery } from '$lib/domains/auth';
  import { createAdminPackageListQuery } from '$lib/domains/packages';

  import TransactionFilters from './TransactionFilters.svelte';
  import TransactionList from './TransactionList.svelte';
  import { createAdminTransactionListQuery } from '../queries/transaction.queries';
  import {
    createTransactionFilterState,
    type TransactionSearch,
  } from '../state/transaction.state.svelte';

  /**
   * The Transaction search screen: one query for the page, one for the Package
   * filter's options, and the three states a list must handle (loading, error,
   * empty). It owns all of that so the route stays a thin adapter (skill §7/§11).
   */
  const filters = createTransactionFilterState();
  const query = createAdminTransactionListQuery(() => filters.filter);
  // The Package filter's options come from the packages domain through its barrel;
  // a failed read only leaves that one select empty.
  const packages = createAdminPackageListQuery();
  // Opening a PIN is support's and the owner's act (ADR 0024 §3). The UI hides the
  // control from an auditor, but the backend is what refuses it: hiding a button is
  // not authorisation.
  const session = createSessionQuery();
  const canReveal = $derived(session.data?.role === 'owner' || session.data?.role === 'support');

  const page = $derived(query.data?.page ?? filters.page);
  const pageSize = $derived(query.data?.page_size ?? filters.pageSize);
  const total = $derived(query.data?.total_count ?? 0);
  const rowCount = $derived(query.data?.data.length ?? 0);
  const firstRow = $derived(rowCount === 0 ? 0 : (page - 1) * pageSize + 1);
  const lastRow = $derived(firstRow + rowCount - 1);
  const hasMore = $derived(rowCount > 0 && lastRow < total);
  const filtered = $derived(
    Boolean(
      filters.state || filters.phoneNumber || filters.from || filters.to || filters.packageId,
    ),
  );

  function applySearch(search: TransactionSearch) {
    // Pressing "Cari" on the search already on screen must still refresh: Svelte
    // skips an equal assignment, so the query key would not change and the cached
    // page would be served without a request.
    const unchanged = filters.page === 1 && filters.matches(search);
    filters.apply(search);
    if (unchanged) {
      void query.refetch();
    }
  }
</script>

<div class="space-y-4">
  <TransactionFilters
    initialState={filters.state}
    phoneNumber={filters.phoneNumber}
    from={filters.from}
    to={filters.to}
    packageId={filters.packageId}
    packages={packages.data ?? []}
    busy={query.isFetching}
    onApply={applySearch}
    onReset={() => filters.reset()}
  />

  {#if query.isPending}
    <div class="space-y-2" role="status" aria-live="polite">
      <span class="sr-only">Memuat daftar Transaction</span>
      {#each [0, 1, 2, 3, 4] as row (row)}
        <div class="bg-muted h-11 animate-pulse rounded-md"></div>
      {/each}
    </div>
  {:else if query.isError}
    <div class="space-y-3" role="alert">
      <p class="text-sm">{query.error?.message ?? 'Daftar Transaction tidak dapat dimuat.'}</p>
      <Button variant="outline" onclick={() => query.refetch()}>Coba lagi</Button>
    </div>
  {:else if query.data}
    {#if rowCount === 0}
      <EmptyState
        title={filtered ? 'Tidak ada Transaction yang cocok' : 'Belum ada Transaction'}
        description={filtered
          ? 'Ubah atau kosongkan filter untuk melihat daftar yang lebih luas.'
          : 'Transaction muncul di sini begitu seorang pembeli memulai pembayaran.'}
      />
    {:else}
      <TransactionList transactions={query.data.data} {canReveal} />
    {/if}

    {#if rowCount > 0 || page > 1}
      <div class="flex flex-wrap items-center justify-between gap-3 text-sm">
        {#if rowCount > 0}
          <p class="text-muted-foreground">
            Menampilkan {firstRow}–{lastRow} dari {total} Transaction
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
