<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import EmptyState from '$lib/components/shared/EmptyState.svelte';

  import AdminCreateForm from './AdminCreateForm.svelte';
  import AdminList from './AdminList.svelte';
  import AdminPasswordForm from './AdminPasswordForm.svelte';
  import { createAdminListQuery, createSetAdminActiveMutation } from '../queries/admin.queries';
  import type { Admin } from '../schemas/admin.schema';

  /**
   * The team screen. Owner-only: the route checks the role, the server enforces
   * it, and this component renders nothing for a role the nav already hid.
   */
  let { canManage }: { canManage: boolean } = $props();

  const query = createAdminListQuery();
  const toggleMutation = createSetAdminActiveMutation();

  let passwordFor = $state<Admin | null>(null);
  let passwordFormKey = $state(0);
  let busyId = $state<number | null>(null);

  function askResetPassword(admin: Admin) {
    passwordFor = admin;
    passwordFormKey += 1;
  }

  function toggleActive(admin: Admin) {
    busyId = admin.id;
    toggleMutation.mutate(
      { id: admin.id, input: { active: !admin.is_active } },
      {
        onSettled: () => {
          busyId = null;
        },
      },
    );
  }
</script>

<div class="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
  <section class="space-y-3">
    <h3 class="font-semibold">Daftar Admin</h3>

    {#if toggleMutation.isError}
      <p class="text-destructive text-sm" role="alert">
        {toggleMutation.error?.message ?? 'Status admin gagal diubah. Coba lagi.'}
      </p>
    {/if}

    {#if query.isPending}
      <div class="space-y-2" role="status" aria-live="polite">
        <span class="sr-only">Memuat daftar admin</span>
        {#each [0, 1, 2] as row (row)}
          <div class="bg-muted h-11 animate-pulse rounded-md"></div>
        {/each}
      </div>
    {:else if query.isError}
      <div class="space-y-3" role="alert">
        <p class="text-sm">{query.error?.message ?? 'Daftar admin tidak dapat dimuat.'}</p>
        <Button variant="outline" onclick={() => query.refetch()}>Coba lagi</Button>
      </div>
    {:else if query.data && query.data.length === 0}
      <EmptyState
        title="Belum ada admin"
        description="Seharusnya ada satu owner dari proses migrate. Tambahkan anggota tim dari formulir di samping."
      />
    {:else if query.data}
      <AdminList
        admins={query.data}
        {busyId}
        onResetPassword={askResetPassword}
        onToggleActive={toggleActive}
      />
    {/if}
  </section>

  {#if canManage}
    <aside class="space-y-4">
      <AdminCreateForm />
      {#if passwordFor}
        {#key passwordFormKey}
          <AdminPasswordForm admin={passwordFor} />
        {/key}
      {/if}
    </aside>
  {/if}
</div>
