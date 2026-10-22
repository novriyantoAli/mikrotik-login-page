<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';
  import {
    Card,
    CardContent,
    CardDescription,
    CardFooter,
    CardHeader,
    CardTitle,
  } from '$lib/components/ui/card';

  import { createHealthQuery } from '../queries/health.queries';
  import {
    dependencyLabels,
    failureLabels,
    type DependencyName,
    type HealthCheck,
  } from '../schemas/health.schema';
  import DependencyRow from './DependencyRow.svelte';

  const dependencyOrder: DependencyName[] = ['postgres', 'freeradius_mysql', 'redis'];

  const query = createHealthQuery();
  const health = $derived(query.data);
  const degraded = $derived(health?.status === 'degraded');

  function checkDetail(check: HealthCheck): string {
    return check.status === 'up'
      ? `Merespons dalam ${check.latency_ms} ms`
      : failureLabels[check.error];
  }

  function formatCheckedAt(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return value;
    }
    return date.toLocaleString('id-ID', { dateStyle: 'medium', timeStyle: 'medium' });
  }
</script>

<Card>
  <CardHeader>
    <div class="flex items-start justify-between gap-4">
      <div class="space-y-1">
        <CardTitle>Status Sistem</CardTitle>
        <CardDescription>Backend dan layanan yang dipakainya.</CardDescription>
      </div>
      {#if health}
        <Badge variant={degraded ? 'destructive' : 'success'}>
          {degraded ? 'Terganggu' : 'Normal'}
        </Badge>
      {/if}
    </div>
  </CardHeader>

  <CardContent class="px-0">
    {#if query.isPending}
      <div class="space-y-3 px-6" role="status" aria-live="polite">
        <span class="sr-only">Memuat status sistem</span>
        {#each [0, 1, 2] as row (row)}
          <div class="bg-muted h-10 animate-pulse rounded-md"></div>
        {/each}
      </div>
    {:else if query.isError}
      <div class="space-y-3 px-6" role="alert">
        <p class="text-sm">
          {query.error?.message ?? 'Status sistem tidak dapat dimuat.'}
        </p>
        <Button variant="outline" onclick={() => query.refetch()}>Coba lagi</Button>
      </div>
    {:else if health}
      <ul class="divide-y">
        <DependencyRow
          label="Backend"
          status="up"
          detail={`${health.service} · versi ${health.version}`}
        />
        {#each dependencyOrder as name (name)}
          <DependencyRow
            label={dependencyLabels[name]}
            status={health.checks[name].status}
            detail={checkDetail(health.checks[name])}
          />
        {/each}
      </ul>
    {/if}
  </CardContent>

  {#if health}
    <CardFooter class="justify-between gap-4">
      <p class="text-muted-foreground text-xs">
        Terakhir diperiksa {formatCheckedAt(health.checked_at)}
      </p>
      <Button variant="outline" onclick={() => query.refetch()}>Periksa lagi</Button>
    </CardFooter>
  {/if}
</Card>
