<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';

  import { formatDuration, formatPrice, type AdminPackage } from '../schemas/package.schema';

  /**
   * A pure table. The query, the mutations and the states belong to
   * PackageManagement, which owns them; this component only renders and emits.
   */
  let {
    packages,
    canWrite,
    selectedId = null,
    busyId = null,
    onEdit,
    onToggleActive,
  }: {
    packages: AdminPackage[];
    canWrite: boolean;
    selectedId?: number | null;
    busyId?: number | null;
    onEdit: (pkg: AdminPackage) => void;
    onToggleActive: (pkg: AdminPackage) => void;
  } = $props();
</script>

<div class="overflow-x-auto rounded-lg border">
  <table class="w-full min-w-[52rem] text-left text-sm">
    <thead class="bg-muted/60 text-muted-foreground text-xs uppercase">
      <tr>
        <th class="px-3 py-2 font-medium">Code</th>
        <th class="px-3 py-2 font-medium">Nama</th>
        <th class="px-3 py-2 font-medium">Harga</th>
        <th class="px-3 py-2 font-medium">Durasi</th>
        <th class="px-3 py-2 font-medium">Rate limit</th>
        <th class="px-3 py-2 font-medium">Status</th>
        <th class="px-3 py-2 font-medium">Versi</th>
        {#if canWrite}
          <th class="px-3 py-2 font-medium">Aksi</th>
        {/if}
      </tr>
    </thead>
    <tbody class="divide-y">
      {#each packages as pkg (pkg.id)}
        <tr class={selectedId === pkg.id ? 'bg-muted/40' : ''}>
          <td class="px-3 py-2 font-mono text-xs">{pkg.code}</td>
          <td class="px-3 py-2 font-medium">{pkg.name}</td>
          <td class="px-3 py-2 whitespace-nowrap">{formatPrice(pkg.price_idr)}</td>
          <td class="px-3 py-2 whitespace-nowrap">{formatDuration(pkg.duration_minutes)}</td>
          <td class="px-3 py-2 font-mono text-xs">{pkg.rate_limit}</td>
          <td class="px-3 py-2">
            <Badge variant={pkg.is_active ? 'success' : 'outline'} size="sm">
              {pkg.is_active ? 'Aktif' : 'Nonaktif'}
            </Badge>
          </td>
          <td class="px-3 py-2 tabular-nums">{pkg.version}</td>
          {#if canWrite}
            <td class="px-3 py-2">
              <div class="flex gap-2">
                <Button variant="outline" onclick={() => onEdit(pkg)}>Ubah</Button>
                <Button
                  variant="outline"
                  class={pkg.is_active ? 'text-destructive' : ''}
                  disabled={busyId === pkg.id}
                  onclick={() => onToggleActive(pkg)}
                >
                  {pkg.is_active ? 'Nonaktifkan' : 'Aktifkan'}
                </Button>
              </div>
            </td>
          {/if}
        </tr>
      {/each}
    </tbody>
  </table>
</div>
