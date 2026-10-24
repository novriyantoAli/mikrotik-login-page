<script lang="ts">
  import { resolve } from '$app/paths';

  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { PackageSummary, createPackageListQuery } from '$lib/domains/packages';

  // The empty state points at the catalog: someone who never bought anything has
  // no Active Period, and buying is the only way out of it (PRD story 51).
  const packages = createPackageListQuery();
</script>

<Card>
  <CardHeader>
    <CardTitle>Belum ada Active Period</CardTitle>
    <p class="text-muted-foreground text-sm">
      Anda belum pernah membeli Package, jadi belum ada masa aktif yang bisa ditampilkan. Pilih
      salah satu Package berikut untuk mulai.
    </p>
  </CardHeader>
  <CardContent class="space-y-3">
    {#if packages.isPending}
      <div class="space-y-3" role="status" aria-live="polite">
        <span class="sr-only">Memuat daftar Package</span>
        <div class="bg-muted h-5 w-1/2 animate-pulse rounded"></div>
        <div class="bg-muted h-4 w-2/3 animate-pulse rounded"></div>
      </div>
    {:else if packages.isError}
      <p class="text-destructive text-sm" role="alert">
        {packages.error?.message ?? 'Daftar Package tidak dapat dimuat.'}
      </p>
      <Button
        class="h-10 w-full"
        type="button"
        variant="outline"
        onclick={() => packages.refetch()}
      >
        Coba lagi
      </Button>
    {:else if (packages.data ?? []).length === 0}
      <p class="text-muted-foreground text-sm">
        Katalog Package masih kosong. Buka Login Page hotspot untuk melihat Package yang tersedia.
      </p>
    {:else}
      {#each packages.data ?? [] as pkg (pkg.id)}
        <a
          class="hover:bg-muted/40 block rounded-lg transition-colors"
          href={resolve(`/beli?package=${pkg.id}`)}
        >
          <PackageSummary {pkg} />
        </a>
      {/each}
    {/if}
  </CardContent>
</Card>
