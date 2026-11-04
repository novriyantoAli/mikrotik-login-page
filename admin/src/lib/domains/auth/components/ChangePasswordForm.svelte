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

  import { createChangePasswordMutation } from '../queries/auth.queries';
  import { ChangePasswordInputSchema, type ChangePasswordInput } from '../schemas/auth.schema';

  /**
   * An admin replacing their own password. The current password is required
   * even though the cookie already proves identity: a stolen cookie must not be
   * enough to lock the real operator out (ADR 0024 §1).
   */
  let { email }: { email: string } = $props();

  const mutation = createChangePasswordMutation();

  let currentPassword = $state('');
  let newPassword = $state('');
  let fieldErrors = $state<Partial<Record<keyof ChangePasswordInput, string>>>({});
  let saved = $state(false);

  function submit(event: SubmitEvent) {
    event.preventDefault();
    mutation.reset();
    saved = false;

    const parsed = ChangePasswordInputSchema.safeParse({
      current_password: currentPassword,
      new_password: newPassword,
    });
    if (!parsed.success) {
      fieldErrors = fieldErrorsFrom<keyof ChangePasswordInput>(parsed.error.issues);
      return;
    }

    if (parsed.data.new_password.trim().toLowerCase() === email.trim().toLowerCase()) {
      fieldErrors = { new_password: 'Kata sandi tidak boleh sama dengan email.' };
      return;
    }

    fieldErrors = {};
    mutation.mutate(parsed.data, {
      onSuccess: () => {
        saved = true;
        currentPassword = '';
        newPassword = '';
      },
    });
  }
</script>

<Card>
  <CardHeader>
    <CardTitle>Ubah Kata Sandi</CardTitle>
    <CardDescription>Minimal 12 karakter, dan tidak boleh sama dengan email Anda.</CardDescription>
  </CardHeader>

  <form onsubmit={submit} novalidate>
    <CardContent class="space-y-4">
      <div class="space-y-1">
        <label class="text-sm font-medium" for="current-password">Kata sandi saat ini</label>
        <input
          id="current-password"
          name="current_password"
          class="bg-background h-10 w-full rounded-md border px-3 text-base"
          type="password"
          autocomplete="current-password"
          bind:value={currentPassword}
          disabled={mutation.isPending}
        />
        {#if fieldErrors.current_password}
          <p class="text-destructive text-sm" role="alert">{fieldErrors.current_password}</p>
        {/if}
      </div>

      <div class="space-y-1">
        <label class="text-sm font-medium" for="new-password">Kata sandi baru</label>
        <input
          id="new-password"
          name="new_password"
          class="bg-background h-10 w-full rounded-md border px-3 text-base"
          type="password"
          autocomplete="new-password"
          bind:value={newPassword}
          disabled={mutation.isPending}
        />
        {#if fieldErrors.new_password}
          <p class="text-destructive text-sm" role="alert">{fieldErrors.new_password}</p>
        {/if}
      </div>

      {#if saved}
        <p class="text-success text-sm" role="status">Kata sandi sudah diganti.</p>
      {:else if mutation.isError}
        <p class="text-destructive text-sm" role="alert">
          {mutation.error?.message ?? 'Kata sandi gagal diganti. Coba lagi.'}
        </p>
      {/if}
    </CardContent>

    <CardFooter>
      <Button type="submit" disabled={mutation.isPending}>
        {mutation.isPending ? 'Menyimpan…' : 'Ganti kata sandi'}
      </Button>
    </CardFooter>
  </form>
</Card>
