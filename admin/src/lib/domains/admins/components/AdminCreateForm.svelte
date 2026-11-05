<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import {
    Card,
    CardContent,
    CardDescription,
    CardFooter,
    CardHeader,
    CardTitle,
  } from '$lib/components/ui/card';

  import { fieldErrorsFrom } from '$lib/utils';
  import { ADMIN_ROLES, roleDescriptions, roleLabels, type AdminRole } from '$lib/domains/auth';

  import { createAdminMutation } from '../queries/admin.queries';
  import { CreateAdminInputSchema, type CreateAdminInput } from '../schemas/admin.schema';

  /**
   * An owner adding a colleague. The password rule is the same zod schema the
   * request is built from; the server re-checks it and also refuses an email
   * that is already taken (ADR 0024 §1).
   */
  let { onCreated }: { onCreated?: () => void } = $props();

  const mutation = createAdminMutation();

  let email = $state('');
  let role = $state<AdminRole>('support');
  let password = $state('');
  let fieldErrors = $state<Partial<Record<keyof CreateAdminInput, string>>>({});
  let added = $state('');

  function submit(event: SubmitEvent) {
    event.preventDefault();
    mutation.reset();
    added = '';

    const parsed = CreateAdminInputSchema.safeParse({ email, role, password });
    if (!parsed.success) {
      fieldErrors = fieldErrorsFrom<keyof CreateAdminInput>(parsed.error.issues);
      return;
    }

    fieldErrors = {};
    mutation.mutate(parsed.data, {
      onSuccess: (created) => {
        added = `Admin ${created.email} ditambahkan.`;
        email = '';
        role = 'support';
        password = '';
        onCreated?.();
      },
    });
  }
</script>

<Card>
  <CardHeader>
    <CardTitle>Admin Baru</CardTitle>
    <CardDescription>Owner menambah anggota tim tanpa deploy ulang.</CardDescription>
  </CardHeader>

  <form onsubmit={submit} novalidate>
    <CardContent class="space-y-4">
      {#if added}
        <p class="text-success text-sm" role="status">{added}</p>
      {:else if mutation.isError}
        <p class="text-destructive text-sm" role="alert">
          {mutation.error?.message ?? 'Admin gagal ditambahkan. Coba lagi.'}
        </p>
      {/if}

      <div class="space-y-1">
        <label class="text-sm font-medium" for="new-admin-email">Email</label>
        <input
          id="new-admin-email"
          class="bg-background h-10 w-full rounded-md border px-3 text-base"
          type="email"
          autocomplete="off"
          bind:value={email}
          disabled={mutation.isPending}
        />
        {#if fieldErrors.email}
          <p class="text-destructive text-sm" role="alert">{fieldErrors.email}</p>
        {/if}
      </div>

      <div class="space-y-1">
        <label class="text-sm font-medium" for="new-admin-role">Peran</label>
        <select
          id="new-admin-role"
          class="bg-background h-10 w-full rounded-md border px-3 text-sm"
          bind:value={role}
          disabled={mutation.isPending}
        >
          {#each ADMIN_ROLES as option (option)}
            <option value={option}>{roleLabels[option]}</option>
          {/each}
        </select>
        <p class="text-muted-foreground text-xs">{roleDescriptions[role]}</p>
      </div>

      <div class="space-y-1">
        <label class="text-sm font-medium" for="new-admin-password">Kata sandi awal</label>
        <input
          id="new-admin-password"
          class="bg-background h-10 w-full rounded-md border px-3 text-base"
          type="password"
          autocomplete="new-password"
          bind:value={password}
          disabled={mutation.isPending}
        />
        {#if fieldErrors.password}
          <p class="text-destructive text-sm" role="alert">{fieldErrors.password}</p>
        {:else}
          <p class="text-muted-foreground text-xs">Minimal 12 karakter.</p>
        {/if}
      </div>
    </CardContent>

    <CardFooter>
      <Button type="submit" disabled={mutation.isPending}>
        {mutation.isPending ? 'Menyimpan…' : 'Tambah admin'}
      </Button>
    </CardFooter>
  </form>
</Card>
