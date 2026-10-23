<script lang="ts">
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  import type { Package } from '../schemas/package.schema';

  let { pkg }: { pkg: Package } = $props();
</script>

<Card>
  <CardHeader>
    <CardTitle>{pkg.nama}</CardTitle>
  </CardHeader>
  <CardContent class="space-y-1 text-sm">
    <p class="flex items-baseline justify-between">
      <span class="text-muted-foreground">Harga</span>
      <span class="text-base font-semibold">{pkg.harga}</span>
    </p>
    <p class="flex items-baseline justify-between">
      <span class="text-muted-foreground">Active Period</span>
      <span class="font-medium">{pkg.durasiActivePeriod}</span>
    </p>
    <p class="flex items-baseline justify-between">
      <span class="text-muted-foreground">Batas kecepatan</span>
      <span class="font-medium">{pkg.batasKecepatan}</span>
    </p>
    <p class="text-muted-foreground pt-2 text-xs">
      Jumlah yang ditagih sama dengan harga Package. Biaya kanal pembayaran ditampilkan terpisah.
    </p>
  </CardContent>
</Card>
