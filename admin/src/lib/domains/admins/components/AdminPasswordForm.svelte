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

  import { createResetAdminPasswordMutation } from '../queries/admin.queries';
  import {
    ResetPasswordInputSchema,
    type Admin,
    type ResetPasswordInput,
  } from '../schemas/admin.schema';

  /**
   * An owner setting a colleague's password. The server revokes every session
   * of that admin in the same transaction, so the old cookie stops working at
   * once (ADR 0024 §5).
   */
  let { admin }: { admin: Admin } = $props();

  const mutation = createResetAdminPasswordMutation();

  let password = $state('');
  let fieldErrors = $state<Partial<Record<keyof ResetPasswordInput, string>>>({});
  let saved = $state(false);

  function submit(event: SubmitEvent) {
    event.preventDefault();
    mutation.reset();
    saved = false;

    const parsed = ResetPasswordInputSchema.safeParse({ password });
    if (!parsed.success) {
      fieldErrors = fieldErrorsFrom<keyof ResetPasswordInput>(parsed.error.issues);
      return;
    }

    fieldErrors = {};
    mutation.mutate(
      { id: admin.id, input: parsed.data },
      {
        onSuccess: () => {
          saved = true;
          password = '';
        },
      },
    );
  }
</script>

<Card>
  <CardHeader>
    <CardTitle>Reset Kata Sandi</CardTitle>
    <CardDescription>
      Untuk {admin.email}. Semua sesi admin itu dicabut begitu kata sandi diganti.
    </CardDescription>
  </CardHeader>

  <form onsubmit={submit} novalidate>
    <CardContent class="space-y-3">
      {#if saved}
        <p class="text-success text-sm" role="status">Kata sandi {admin.email} sudah diganti.</p>
      {:else if mutation.isError}
        <p class="text-destructive text-sm" role="alert">
          {mutation.error?.message ?? 'Kata sandi gagal diganti. Coba lagi.'}
        </p>
      {/if}

      <div class="space-y-1">
        <label class="text-sm font-medium" for="reset-password">Kata sandi baru</label>
        <input
          id="reset-password"
          class="bg-background h-10 w-full rounded-md border px-3 text-base"
          type="password"
          autocomplete="new-password"
          bind:value={password}
          disabled={mutation.isPending}
        />
        {#if fieldErrors.password}
          <p class="text-destructive text-sm" role="alert">{fieldErrors.password}</p>
        {/if}
      </div>
    </CardContent>

    <CardFooter>
      <Button type="submit" disabled={mutation.isPending}>
        {mutation.isPending ? 'Menyimpan…' : 'Ganti kata sandi'}
      </Button>
    </CardFooter>
  </form>
</Card>
