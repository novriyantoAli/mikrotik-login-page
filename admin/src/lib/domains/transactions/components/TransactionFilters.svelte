<script lang="ts">
  import { untrack } from 'svelte';

  import { Button } from '$lib/components/ui/button';
  import type { AdminPackage } from '$lib/domains/packages';

  import {
    TRANSACTION_STATES,
    transactionStateLabels,
    type TransactionState,
  } from '../schemas/transaction.schema';

  /**
   * The search form. It holds its own draft inputs and only reports a submitted
   * search, so the query behind it does not refire per keystroke. The route stays a
   * thin adapter: this component never calls the API.
   *
   * The Package select is fed by the caller (the screen owns that query), so this
   * component stays presentational.
   */
  let {
    initialState = undefined,
    phoneNumber = '',
    from = '',
    to = '',
    packageId = undefined,
    packages = [],
    busy = false,
    onApply,
    onReset,
  }: {
    initialState?: TransactionState;
    phoneNumber?: string;
    from?: string;
    to?: string;
    packageId?: number;
    packages?: AdminPackage[];
    busy?: boolean;
    onApply: (search: {
      state?: TransactionState;
      phoneNumber: string;
      from: string;
      to: string;
      packageId?: number;
    }) => void;
    onReset: () => void;
  } = $props();

  // Drafts: seeded once from the applied search, so remounting the screen shows
  // what is actually being filtered. `untrack` says that on purpose: a later change
  // to the submitted search must not overwrite what the operator is typing.
  let draftState = $state<TransactionState | ''>(untrack(() => initialState ?? ''));
  let draftPhone = $state(untrack(() => phoneNumber));
  let draftFrom = $state(untrack(() => from));
  let draftTo = $state(untrack(() => to));
  // A <select> reports strings; '' is "every Package".
  let draftPackage = $state<string>(untrack(() => (packageId ? String(packageId) : '')));

  function submit(event: SubmitEvent) {
    event.preventDefault();
    onApply({
      state: draftState === '' ? undefined : draftState,
      phoneNumber: draftPhone,
      from: draftFrom,
      to: draftTo,
      packageId: draftPackage === '' ? undefined : Number(draftPackage),
    });
  }

  function clear() {
    draftState = '';
    draftPhone = '';
    draftFrom = '';
    draftTo = '';
    draftPackage = '';
    onReset();
  }
</script>

<form class="grid items-end gap-3 sm:grid-cols-2 lg:grid-cols-6" onsubmit={submit}>
  <label class="space-y-1 text-sm">
    <span class="text-muted-foreground block text-xs font-medium">State</span>
    <select
      bind:value={draftState}
      class="border-input bg-background w-full rounded-md border px-3 py-2"
      name="state"
    >
      <option value="">Semua state</option>
      {#each TRANSACTION_STATES as value (value)}
        <option {value}>{transactionStateLabels[value]}</option>
      {/each}
    </select>
  </label>

  <label class="space-y-1 text-sm">
    <span class="text-muted-foreground block text-xs font-medium">Phone Number</span>
    <input
      bind:value={draftPhone}
      class="border-input bg-background w-full rounded-md border px-3 py-2"
      name="phone_number"
      inputmode="tel"
      placeholder="0812… atau 62812…"
      autocomplete="off"
    />
  </label>

  <label class="space-y-1 text-sm">
    <span class="text-muted-foreground block text-xs font-medium">Package</span>
    <select
      bind:value={draftPackage}
      class="border-input bg-background w-full rounded-md border px-3 py-2"
      name="package_id"
    >
      <option value="">Semua package</option>
      {#each packages as pkg (pkg.id)}
        <option value={String(pkg.id)}>
          {pkg.name}{pkg.is_active ? '' : ' (nonaktif)'}
        </option>
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
    <Button type="submit" disabled={busy}>Cari</Button>
    <Button type="button" variant="outline" onclick={clear}>Reset</Button>
  </div>
</form>
