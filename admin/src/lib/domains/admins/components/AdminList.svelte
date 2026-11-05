<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';

  import { roleLabels, type AdminRole } from '$lib/domains/auth';

  import type { Admin } from '../schemas/admin.schema';

  /** A pure table; the query and the mutations belong to AdminManagement. */
  let {
    admins,
    busyId = null,
    onResetPassword,
    onToggleActive,
  }: {
    admins: Admin[];
    busyId?: number | null;
    onResetPassword: (admin: Admin) => void;
    onToggleActive: (admin: Admin) => void;
  } = $props();

  const roleVariants: Record<AdminRole, 'default' | 'outline' | 'success'> = {
    owner: 'default',
    support: 'success',
    auditor: 'outline',
  };

  function formatCreatedAt(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return value;
    }
    return date.toLocaleDateString('id-ID', { dateStyle: 'medium' });
  }
</script>

<div class="overflow-x-auto rounded-lg border">
  <table class="w-full min-w-[44rem] text-left text-sm">
    <thead class="bg-muted/60 text-muted-foreground text-xs uppercase">
      <tr>
        <th class="px-3 py-2 font-medium">Email</th>
        <th class="px-3 py-2 font-medium">Peran</th>
        <th class="px-3 py-2 font-medium">Status</th>
        <th class="px-3 py-2 font-medium">Dibuat</th>
        <th class="px-3 py-2 font-medium">Aksi</th>
      </tr>
    </thead>
    <tbody class="divide-y">
      {#each admins as admin (admin.id)}
        <tr>
          <td class="px-3 py-2 font-medium">{admin.email}</td>
          <td class="px-3 py-2">
            <Badge variant={roleVariants[admin.role]} size="sm">{roleLabels[admin.role]}</Badge>
          </td>
          <td class="px-3 py-2">
            <Badge variant={admin.is_active ? 'success' : 'outline'} size="sm">
              {admin.is_active ? 'Aktif' : 'Nonaktif'}
            </Badge>
          </td>
          <td class="px-3 py-2 whitespace-nowrap">{formatCreatedAt(admin.created_at)}</td>
          <td class="px-3 py-2">
            <div class="flex gap-2">
              <Button variant="outline" onclick={() => onResetPassword(admin)}>
                Reset kata sandi
              </Button>
              <Button
                variant="outline"
                class={admin.is_active ? 'text-destructive' : ''}
                disabled={busyId === admin.id}
                onclick={() => onToggleActive(admin)}
              >
                {admin.is_active ? 'Nonaktifkan' : 'Aktifkan'}
              </Button>
            </div>
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>
