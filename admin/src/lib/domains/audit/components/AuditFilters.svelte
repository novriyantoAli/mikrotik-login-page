<script lang="ts">
  import { untrack } from 'svelte';

  import { Button } from '$lib/components/ui/button';

  import { AUDIT_ACTIONS, auditActionLabels, type AuditAction } from '../schemas/audit.schema';

  /**
   * The audit search form. It holds its own draft inputs and only reports a submitted
   * search, so the query behind it does not refire per keystroke. The route stays a
   * thin adapter: this component never calls the API.
   */
  let {
    adminEmail = '',
    action = undefined,
    from = '',
    to = '',
    busy = false,
    onApply,
    onReset,
  }: {
    adminEmail?: string;
    action?: AuditAction;
    from?: string;
    to?: string;
    busy?: boolean;
    onApply: (search: {
      adminEmail: string;
      action?: AuditAction;
      from: string;
      to: string;
    }) => void;
    onReset: () => void;
  } = $props();

  // Drafts: seeded once from the applied search, so remounting the screen shows what
  // is actually being filtered. `untrack` says that on purpose: a later change to the
  // submitted search must not overwrite what the auditor is typing.
  let draftEmail = $state(untrack(() => adminEmail));
  let draftAction = $state<AuditAction | ''>(untrack(() => action ?? ''));
  let draftFrom = $state(untrack(() => from));
  let draftTo = $state(untrack(() => to));

  function submit(event: SubmitEvent) {
    event.preventDefault();
    onApply({
      adminEmail: draftEmail,
      action: draftAction === '' ? undefined : draftAction,
      from: draftFrom,
      to: draftTo,
    });
  }

  function clear() {
    draftEmail = '';
    draftAction = '';
    draftFrom = '';
    draftTo = '';
    onReset();
  }
</script>

<form class="grid items-end gap-3 sm:grid-cols-2 lg:grid-cols-5" onsubmit={submit}>
  <label class="space-y-1 text-sm">
    <span class="text-muted-foreground block text-xs font-medium">Email admin</span>
    <input
      bind:value={draftEmail}
      class="border-input bg-background w-full rounded-md border px-3 py-2"
      name="admin_email"
      type="email"
      placeholder="owner@example.test"
      autocomplete="off"
    />
  </label>

  <label class="space-y-1 text-sm">
    <span class="text-muted-foreground block text-xs font-medium">Aksi</span>
    <select
      bind:value={draftAction}
      class="border-input bg-background w-full rounded-md border px-3 py-2"
      name="action"
    >
      <option value="">Semua aksi</option>
      {#each AUDIT_ACTIONS as value (value)}
        <option {value}>{auditActionLabels[value]}</option>
      {/each}
    </select>
  </label>

  <label class="space-y-1 text-sm">
    <span class="text-muted-foreground block text-xs font-medium">Dari tanggal</span>
    <input
      bind:value={draftFrom}
      class="border-input bg-background w-full rounded-md border px-3 py-2"
      name="from"
      type="date"
    />
  </label>

  <label class="space-y-1 text-sm">
    <span class="text-muted-foreground block text-xs font-medium">Sampai tanggal</span>
    <input
      bind:value={draftTo}
      class="border-input bg-background w-full rounded-md border px-3 py-2"
      name="to"
      type="date"
    />
  </label>

  <div class="flex gap-2">
    <Button type="submit" disabled={busy}>{busy ? 'Mencari…' : 'Cari'}</Button>
    <Button type="button" variant="outline" onclick={clear}>Kosongkan</Button>
  </div>
</form>
