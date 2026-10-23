<script lang="ts">
  import { resolve } from '$app/paths';

  import { Button } from '$lib/components/ui/button';
  import {
    Card,
    CardContent,
    CardDescription,
    CardFooter,
    CardHeader,
    CardTitle,
  } from '$lib/components/ui/card';

  import { createRequestMagicLinkMutation } from '../queries/auth.queries';
  import { queuedLinkMessage } from '../schemas/auth.schema';

  // The Login Page hands over the Package it was bought from and where to go
  // back to; the backend re-validates the return target before it stores it.
  let { packageId, returnUrl }: { packageId?: number; returnUrl?: string } = $props();

  const mutation = createRequestMagicLinkMutation();

  let phoneNumber = $state('');
  let submittedPhone = $state('');
  let validationError = $state('');

  function submit(event: SubmitEvent) {
    event.preventDefault();
    validationError = '';

    const value = phoneNumber.trim();
    if (value === '') {
      validationError = 'Phone Number wajib diisi.';
      return;
    }

    mutation.mutate(
      { phone_number: value, package_id: packageId, return_url: returnUrl },
      {
        onSuccess: () => {
          submittedPhone = value;
        },
      },
    );
  }

  function tryAgain() {
    mutation.reset();
    submittedPhone = '';
  }
</script>

{#if submittedPhone}
  <Card>
    <CardHeader>
      <CardTitle>Periksa SMS Anda</CardTitle>
      <CardDescription>{queuedLinkMessage}</CardDescription>
    </CardHeader>
    <CardContent class="space-y-3 text-sm">
      <p>
        Dikirim ke <span class="font-medium">{submittedPhone}</span>.
      </p>
      <p class="text-muted-foreground">
        Tautan hanya bisa dipakai sekali dan berlaku sebentar. Bila SMS belum datang, tunggu
        sebentar sebelum meminta lagi.
      </p>
    </CardContent>
    <CardFooter class="justify-between gap-3">
      <a class="text-sm underline-offset-4 hover:underline" href={resolve('/')}>Kembali</a>
      <Button variant="outline" type="button" onclick={tryAgain}>Ganti nomor</Button>
    </CardFooter>
  </Card>
{:else}
  <Card>
    <CardHeader>
      <CardTitle>Masuk dengan Phone Number</CardTitle>
      <CardDescription>Kami kirim tautan masuk lewat SMS. Tidak perlu kata sandi.</CardDescription>
    </CardHeader>

    <form onsubmit={submit} novalidate>
      <CardContent class="space-y-2">
        <label class="text-sm font-medium" for="phone-number">Phone Number</label>
        <input
          id="phone-number"
          name="phone_number"
          class="bg-background h-10 w-full rounded-md border px-3 text-base"
          type="tel"
          inputmode="tel"
          autocomplete="tel"
          placeholder="0812 3456 7890"
          bind:value={phoneNumber}
          aria-invalid={validationError !== '' || mutation.isError}
          aria-describedby="phone-number-help"
          disabled={mutation.isPending}
        />
        <p id="phone-number-help" class="text-muted-foreground text-xs">
          Boleh ditulis 0812… atau +62 812….
        </p>

        {#if validationError}
          <p class="text-destructive text-sm" role="alert">{validationError}</p>
        {:else if mutation.isError}
          <p class="text-destructive text-sm" role="alert">
            {mutation.error?.message ?? 'Permintaan gagal dikirim. Coba lagi.'}
          </p>
        {/if}
      </CardContent>

      <CardFooter>
        <Button class="h-10 w-full" type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? 'Mengirim…' : 'Kirim tautan masuk'}
        </Button>
      </CardFooter>
    </form>
  </Card>
{/if}
