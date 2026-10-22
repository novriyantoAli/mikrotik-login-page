<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';

  let {
    label,
    status,
    detail,
  }: {
    label: string;
    status: 'up' | 'down';
    detail: string;
  } = $props();

  const isUp = $derived(status === 'up');
</script>

<li class="flex items-start justify-between gap-4 px-6 py-4" data-dependency-status={status}>
  <div class="space-y-1">
    <p class="text-sm font-medium">{label}</p>
    <p class="text-muted-foreground text-xs break-words">{detail}</p>
  </div>
  <Badge variant={isUp ? 'success' : 'destructive'}>
    {isUp ? 'Terhubung' : 'Tidak terhubung'}
  </Badge>
</li>
