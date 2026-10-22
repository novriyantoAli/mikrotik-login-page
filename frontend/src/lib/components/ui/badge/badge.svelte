<script lang="ts">
  import type { Snippet } from 'svelte';

  import { cn } from '$lib/utils';

  type Variant = 'default' | 'success' | 'destructive' | 'outline';
  type Size = 'default' | 'sm';

  let {
    variant = 'default',
    size = 'default',
    class: className,
    children,
  }: {
    variant?: Variant;
    size?: Size;
    class?: string;
    children?: Snippet;
  } = $props();

  const variants: Record<Variant, string> = {
    default: 'border-transparent bg-primary text-primary-foreground',
    success: 'border-transparent bg-success text-success-foreground',
    destructive: 'border-transparent bg-destructive text-destructive-foreground',
    outline: 'text-foreground',
  };

  const sizes: Record<Size, string> = {
    default: 'px-2.5 py-0.5',
    sm: 'px-2 py-0.5 text-xs',
  };
</script>

<span
  data-slot="badge"
  class={cn(
    'inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-full border text-xs font-medium whitespace-nowrap',
    variants[variant],
    sizes[size],
    className,
  )}
>
  {@render children?.()}
</span>
