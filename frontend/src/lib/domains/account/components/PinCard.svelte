<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';

  // The PIN arrives from the summary and is hidden by default: a shoulder-surfing
  // measure, not an authorization one (ADR 0017). While hidden the digits are not
  // in the DOM at all.
  let { pin, visible, onToggle }: { pin: string; visible: boolean; onToggle: () => void } =
    $props();
</script>

<Card>
  <CardHeader>
    <CardTitle>PIN saat ini</CardTitle>
    <p class="text-muted-foreground text-sm">
      Dipakai untuk masuk ke hotspot bersama Phone Number Anda. Jangan bagikan PIN kepada siapa pun.
    </p>
  </CardHeader>
  <CardContent class="space-y-3">
    <div class="bg-muted/60 rounded-lg p-3 text-center">
      <p class="text-muted-foreground text-xs">PIN</p>
      <p class="font-mono text-xl font-semibold tracking-[0.3em]" data-testid="account-pin">
        {visible ? pin : '••••••'}
      </p>
    </div>
    <Button
      class="h-10 w-full"
      type="button"
      variant="outline"
      aria-pressed={visible}
      onclick={onToggle}
    >
      {visible ? 'Sembunyikan PIN' : 'Tunjukkan PIN'}
    </Button>
  </CardContent>
</Card>
