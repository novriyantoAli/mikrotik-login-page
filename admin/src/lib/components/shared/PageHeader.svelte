<script lang="ts">
  import type { Snippet } from 'svelte';

  /**
   * The page's one heading block. Routes stay thin, so the title and the
   * explanation live with the domain screen instead of in `+page.svelte`.
   */
  let { title, description, actions }: { title: string; description?: string; actions?: Snippet } =
    $props();
</script>

<header class="flex flex-wrap items-start justify-between gap-4">
  <div class="space-y-1">
    <h2 class="text-xl font-semibold">{title}</h2>
    {#if description}
      <p class="text-muted-foreground text-sm">{description}</p>
    {/if}
  </div>
  {#if actions}
    <div class="flex items-center gap-2">{@render actions()}</div>
  {/if}
</header>
