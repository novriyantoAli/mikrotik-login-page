<script lang="ts">
  import { cn } from '$lib/utils';

  import { formatChannelFee } from '../format';
  import type { Channel } from '../schemas/checkout.schema';

  let {
    channels,
    selected = null,
    disabled = false,
    onSelect,
  }: {
    channels: Channel[];
    selected?: string | null;
    disabled?: boolean;
    onSelect: (code: string) => void;
  } = $props();
</script>

<fieldset class="space-y-2" {disabled}>
  <legend class="pb-1 text-sm font-medium">Kanal pembayaran</legend>

  {#each channels as channel (channel.code)}
    <label
      class={cn(
        'flex cursor-pointer items-center justify-between gap-3 rounded-lg border p-3 text-sm transition-colors',
        selected === channel.code ? 'border-primary bg-muted/60' : 'bg-background',
        disabled && 'pointer-events-none opacity-60',
      )}
    >
      <span class="flex flex-col">
        <span class="font-medium">{channel.name}</span>
        <span class="text-muted-foreground text-xs">{channel.group}</span>
      </span>
      <span class="text-right">
        <span class="text-muted-foreground block text-xs">Biaya</span>
        <span class="font-medium"
          >{formatChannelFee(channel.fee_flat_idr, channel.fee_percent)}</span
        >
      </span>
      <input
        class="sr-only"
        type="radio"
        name="channel"
        value={channel.code}
        checked={selected === channel.code}
        onchange={() => onSelect(channel.code)}
      />
    </label>
  {/each}
</fieldset>
