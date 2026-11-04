<script lang="ts">
  import type { Snippet } from 'svelte';

  /** A real empty state, not an unexplained blank list. */
  let {
    title,
    description,
    children,
  }: { title: string; description?: string; children?: Snippet } = $props();
</script>

<div
  class="border-border/70 bg-muted/30 flex flex-col items-center gap-2 rounded-lg border border-dashed px-6 py-10 text-center"
>
  <p class="text-sm font-medium">{title}</p>
  {#if description}
    <p class="text-muted-foreground max-w-prose text-sm">{description}</p>
  {/if}
  {#if children}
    {@render children()}
  {/if}
</div>
