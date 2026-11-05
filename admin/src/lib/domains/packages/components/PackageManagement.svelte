<script lang="ts">
  import { Button } from '$lib/components/ui/button';

  import EmptyState from '$lib/components/shared/EmptyState.svelte';

  import PackageForm from './PackageForm.svelte';
  import PackageList from './PackageList.svelte';
  import {
    createAdminPackageListQuery,
    createSetActivePackageMutation,
  } from '../queries/package.queries';
  import type { AdminPackage } from '../schemas/package.schema';

  /**
   * The catalog screen: one query for the list, one mutation for retiring or
   * reviving a Package, and a form that can be creating or editing. It owns the
   * loading/error/empty states so the route stays a thin adapter (skill §7/§11).
   */
  let { canWrite }: { canWrite: boolean } = $props();

  const query = createAdminPackageListQuery();
  const toggleMutation = createSetActivePackageMutation();

  // `null` means the form is creating; a row means it is editing that row.
  let selected = $state<AdminPackage | null>(null);
  // Bumping the key remounts the form, which resets its fields to the row.
  let formKey = $state(0);
  let busyId = $state<number | null>(null);

  function edit(pkg: AdminPackage) {
    selected = pkg;
    formKey += 1;
  }

  function createNew() {
    selected = null;
    formKey += 1;
  }

  function toggleActive(pkg: AdminPackage) {
    busyId = pkg.id;
    toggleMutation.mutate(
      { id: pkg.id, input: { active: !pkg.is_active } },
      {
        onSettled: () => {
          busyId = null;
        },
      },
    );
  }

  function afterSave(saved: AdminPackage) {
    selected = saved;
    formKey += 1;
  }
</script>

<div class="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
  <section class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <h3 class="font-semibold">Daftar Package</h3>
      {#if canWrite}
        <Button variant="outline" onclick={createNew}>Package baru</Button>
      {/if}
    </div>

    {#if toggleMutation.isError}
      <p class="text-destructive text-sm" role="alert">
        {toggleMutation.error?.message ?? 'Status Package gagal diubah. Coba lagi.'}
      </p>
    {/if}

    {#if query.isPending}
      <div class="space-y-2" role="status" aria-live="polite">
        <span class="sr-only">Memuat katalog Package</span>
        {#each [0, 1, 2, 3] as row (row)}
          <div class="bg-muted h-11 animate-pulse rounded-md"></div>
        {/each}
      </div>
    {:else if query.isError}
      <div class="space-y-3" role="alert">
        <p class="text-sm">{query.error?.message ?? 'Katalog Package tidak dapat dimuat.'}</p>
        <Button variant="outline" onclick={() => query.refetch()}>Coba lagi</Button>
      </div>
    {:else if query.data && query.data.length === 0}
      <EmptyState
        title="Belum ada Package"
        description="Login Page tidak akan menjual apa pun sampai ada Package aktif. Buat yang pertama dari formulir di samping."
      />
    {:else if query.data}
      <PackageList
        packages={query.data}
        {canWrite}
        selectedId={selected?.id ?? null}
        {busyId}
        onEdit={edit}
        onToggleActive={toggleActive}
      />
    {/if}
  </section>

  {#if canWrite}
    <aside>
      {#key formKey}
        <PackageForm pkg={selected} {canWrite} onSaved={afterSave} onCancel={createNew} />
      {/key}
    </aside>
  {/if}
</div>
