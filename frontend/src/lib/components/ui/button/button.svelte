<script lang="ts">
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import type { Snippet } from 'svelte';

  import { cn } from '$lib/utils';

  let {
    variant = 'default',
    class: className,
    children,
    ...rest
  }: HTMLButtonAttributes & {
    variant?: 'default' | 'outline';
    children?: Snippet;
  } = $props();

  const variants = {
    default: 'bg-primary text-primary-foreground hover:bg-primary/90',
    outline: 'border bg-background hover:bg-muted',
  };
</script>

<button
  data-slot="button"
  class={cn(
    'inline-flex h-9 items-center justify-center rounded-md px-4 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50',
    variants[variant],
    className,
  )}
  {...rest}
>
  {@render children?.()}
</button>
