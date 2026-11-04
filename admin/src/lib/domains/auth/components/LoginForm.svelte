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

  import { createLoginMutation } from '../queries/auth.queries';
  import { LoginInputSchema, type LoginInput } from '../schemas/auth.schema';

  /**
   * The panel's only door. It never reveals whether an email exists: the
   * backend answers an unknown email and a wrong password with one body, so the
   * form shows whatever message it got and nothing else (ADR 0024 §4).
   */
  let { notice = '' }: { notice?: string } = $props();

  const mutation = createLoginMutation();

  let email = $state('');
  let password = $state('');
  let fieldErrors = $state<Partial<Record<keyof LoginInput, string>>>({});

  function submit(event: SubmitEvent) {
    event.preventDefault();
    mutation.reset();

    const parsed = LoginInputSchema.safeParse({ email, password });
    if (!parsed.success) {
      fieldErrors = fieldErrorsFrom<keyof LoginInput>(parsed.error.issues);
      return;
    }

    fieldErrors = {};
    mutation.mutate(parsed.data);
  }
</script>

<Card class="mx-auto w-full max-w-md">
  <CardHeader>
    <CardTitle>Masuk Panel Admin</CardTitle>
    <CardDescription>
      Gunakan email dan kata sandi admin. Panel ini terpisah dari aplikasi pembelian.
    </CardDescription>
  </CardHeader>

  <form onsubmit={submit} novalidate>
    <CardContent class="space-y-4">
      {#if notice}
        <p class="bg-muted rounded-md px-3 py-2 text-sm" role="status">{notice}</p>
      {/if}

      <div class="space-y-1">
        <label class="text-sm font-medium" for="admin-email">Email</label>
        <input
          id="admin-email"
          name="email"
          class="bg-background h-10 w-full rounded-md border px-3 text-base"
          type="email"
          autocomplete="username"
          bind:value={email}
          aria-invalid={fieldErrors.email ? 'true' : 'false'}
          aria-describedby="admin-email-error"
          disabled={mutation.isPending}
        />
        {#if fieldErrors.email}
          <p id="admin-email-error" class="text-destructive text-sm" role="alert">
            {fieldErrors.email}
          </p>
        {/if}
      </div>

      <div class="space-y-1">
        <label class="text-sm font-medium" for="admin-password">Kata sandi</label>
        <input
          id="admin-password"
          name="password"
          class="bg-background h-10 w-full rounded-md border px-3 text-base"
          type="password"
          autocomplete="current-password"
          bind:value={password}
          aria-invalid={fieldErrors.password ? 'true' : 'false'}
          aria-describedby="admin-password-error"
          disabled={mutation.isPending}
        />
        {#if fieldErrors.password}
          <p id="admin-password-error" class="text-destructive text-sm" role="alert">
            {fieldErrors.password}
          </p>
        {/if}
      </div>

      {#if mutation.isError}
        <p class="text-destructive text-sm" role="alert">
          {mutation.error?.message ?? 'Tidak dapat masuk. Coba lagi.'}
        </p>
      {/if}
    </CardContent>

    <CardFooter>
      <Button class="h-10 w-full" type="submit" disabled={mutation.isPending}>
        {mutation.isPending ? 'Memeriksa…' : 'Masuk'}
      </Button>
    </CardFooter>
  </form>
</Card>
