<script lang="ts">
  import { useQueryClient } from '@tanstack/svelte-query';

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

  import { versionConflictOf, type PackageVersionConflict } from '../api/package.api';
  import {
    createPackageMutation,
    createUpdatePackageMutation,
    packageKeys,
  } from '../queries/package.queries';
  import {
    CreatePackageInputSchema,
    MAX_PACKAGE_NAME_LENGTH,
    UpdatePackageInputSchema,
    formatDuration,
    formatPrice,
    type AdminPackage,
  } from '../schemas/package.schema';

  /**
   * Create or edit one Package. The rules are the same zod schema the request
   * is built from, so a field the form accepts is a field the API accepts; the
   * service re-checks them because a form is not a boundary (ADR 0025 §6).
   */
  let {
    pkg = null,
    canWrite,
    onSaved,
    onCancel,
  }: {
    pkg?: AdminPackage | null;
    canWrite: boolean;
    onSaved?: (pkg: AdminPackage) => void;
    onCancel?: () => void;
  } = $props();

  const isEditing = $derived(pkg !== null);
  const queryClient = useQueryClient();
  const createMutation = createPackageMutation();
  const updateMutation = createUpdatePackageMutation();

  // The parent remounts this form with a key when the edited row changes, so
  // the snapshot below is the form's starting point, not a stale copy. The
  // closure is what tells the compiler that reading the prop once is intended.
  const start = (() => ({
    code: pkg?.code ?? '',
    name: pkg?.name ?? '',
    price: pkg ? String(pkg.price_idr) : '',
    duration: pkg ? String(pkg.duration_minutes) : '',
    rateLimit: pkg?.rate_limit ?? '',
    version: pkg?.version ?? 0,
  }))();

  let code = $state(start.code);
  let name = $state(start.name);
  let price = $state(start.price);
  let duration = $state(start.duration);
  let rateLimit = $state(start.rateLimit);
  let version = $state(start.version);

  type FieldName = 'code' | 'name' | 'price_idr' | 'duration_minutes' | 'rate_limit';

  let fieldErrors = $state<Partial<Record<FieldName, string>>>({});
  let conflict = $state<PackageVersionConflict | null>(null);

  const pending = $derived(createMutation.isPending || updateMutation.isPending);
  const failure = $derived(
    conflict === null ? (createMutation.error ?? updateMutation.error) : null,
  );

  function submit(event: SubmitEvent) {
    event.preventDefault();
    fieldErrors = {};
    conflict = null;
    createMutation.reset();
    updateMutation.reset();

    const values = {
      code,
      name,
      price_idr: price,
      duration_minutes: duration,
      rate_limit: rateLimit,
    };

    if (pkg === null) {
      const parsed = CreatePackageInputSchema.safeParse(values);
      if (!parsed.success) {
        fieldErrors = fieldErrorsFrom<FieldName>(parsed.error.issues);
        return;
      }

      createMutation.mutate(parsed.data, {
        onSuccess: (created) => onSaved?.(created),
      });
      return;
    }

    const parsed = UpdatePackageInputSchema.safeParse({ ...values, version });
    if (!parsed.success) {
      fieldErrors = fieldErrorsFrom<FieldName>(parsed.error.issues);
      return;
    }

    updateMutation.mutate(
      { id: pkg.id, input: parsed.data },
      {
        onSuccess: (updated) => onSaved?.(updated),
        onError: (error) => {
          const stale = versionConflictOf(error);
          if (stale === null) {
            return;
          }
          // Show what the row says now, and adopt its version so the next
          // submit is based on what the operator can actually see.
          conflict = stale;
          version = stale.current.version;
          name = stale.current.name;
          price = String(stale.current.price_idr);
          duration = String(stale.current.duration_minutes);
          rateLimit = stale.current.rate_limit;
          // The table behind the form is stale too: reload it so the operator
          // sees the same row in both places.
          queryClient.invalidateQueries({ queryKey: packageKeys.all });
        },
      },
    );
  }
</script>

{#if canWrite}
  <Card>
    <CardHeader>
      <CardTitle>{isEditing ? `Ubah ${pkg?.code}` : 'Package Baru'}</CardTitle>
      <CardDescription>
        {isEditing
          ? 'Code tidak bisa diubah. Harga, nama, durasi, dan rate limit boleh.'
          : 'Code menjadi identitas Package dan tidak bisa diubah setelah dibuat.'}
      </CardDescription>
    </CardHeader>

    <form onsubmit={submit} novalidate>
      <CardContent class="space-y-4">
        {#if conflict}
          <div class="bg-muted space-y-2 rounded-md px-3 py-2 text-sm" role="alert">
            <p class="font-medium">Paket sudah diubah orang lain.</p>
            <p class="text-muted-foreground">
              Nilai terbaru: {conflict.current.name} · {formatPrice(conflict.current.price_idr)} · {formatDuration(
                conflict.current.duration_minutes,
              )} · {conflict.current.rate_limit} (versi {conflict.current.version}).
            </p>
            <p class="text-muted-foreground">
              Form sudah dimuat dengan nilai terbaru. Periksa lagi sebelum menyimpan.
            </p>
            <Button
              variant="outline"
              type="button"
              onclick={() => queryClient.invalidateQueries({ queryKey: packageKeys.all })}
            >
              Muat ulang daftar
            </Button>
          </div>
        {:else if failure}
          <p class="text-destructive text-sm" role="alert">
            {failure.message ?? 'Package gagal disimpan. Coba lagi.'}
          </p>
        {/if}

        <div class="space-y-1">
          <label class="text-sm font-medium" for="package-code">Code</label>
          <input
            id="package-code"
            class="bg-background h-10 w-full rounded-md border px-3 font-mono text-sm disabled:opacity-60"
            bind:value={code}
            placeholder="paket-harian"
            disabled={isEditing || pending}
            aria-invalid={fieldErrors.code ? 'true' : 'false'}
          />
          {#if fieldErrors.code}
            <p class="text-destructive text-sm" role="alert">{fieldErrors.code}</p>
          {/if}
        </div>

        <div class="space-y-1">
          <label class="text-sm font-medium" for="package-name">Nama</label>
          <input
            id="package-name"
            class="bg-background h-10 w-full rounded-md border px-3 text-base"
            bind:value={name}
            maxlength={MAX_PACKAGE_NAME_LENGTH}
            placeholder="Paket Harian"
            disabled={pending}
            aria-invalid={fieldErrors.name ? 'true' : 'false'}
          />
          {#if fieldErrors.name}
            <p class="text-destructive text-sm" role="alert">{fieldErrors.name}</p>
          {/if}
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <div class="space-y-1">
            <label class="text-sm font-medium" for="package-price">Harga (Rp)</label>
            <input
              id="package-price"
              class="bg-background h-10 w-full rounded-md border px-3 text-base"
              type="text"
              inputmode="numeric"
              bind:value={price}
              placeholder="5000"
              disabled={pending}
              aria-invalid={fieldErrors.price_idr ? 'true' : 'false'}
            />
            {#if fieldErrors.price_idr}
              <p class="text-destructive text-sm" role="alert">{fieldErrors.price_idr}</p>
            {/if}
          </div>

          <div class="space-y-1">
            <label class="text-sm font-medium" for="package-duration">Durasi (menit)</label>
            <input
              id="package-duration"
              class="bg-background h-10 w-full rounded-md border px-3 text-base"
              type="text"
              inputmode="numeric"
              bind:value={duration}
              placeholder="1440"
              disabled={pending}
              aria-invalid={fieldErrors.duration_minutes ? 'true' : 'false'}
            />
            {#if fieldErrors.duration_minutes}
              <p class="text-destructive text-sm" role="alert">{fieldErrors.duration_minutes}</p>
            {/if}
          </div>
        </div>

        <div class="space-y-1">
          <label class="text-sm font-medium" for="package-rate-limit">Rate limit</label>
          <input
            id="package-rate-limit"
            class="bg-background h-10 w-full rounded-md border px-3 font-mono text-sm"
            bind:value={rateLimit}
            placeholder="5M/5M"
            disabled={pending}
            aria-invalid={fieldErrors.rate_limit ? 'true' : 'false'}
          />
          {#if fieldErrors.rate_limit}
            <p class="text-destructive text-sm" role="alert">{fieldErrors.rate_limit}</p>
          {:else}
            <p class="text-muted-foreground text-xs">
              Bentuk unduh/unggah, misalnya 5M/5M atau 1M/512k.
            </p>
          {/if}
        </div>
      </CardContent>

      <CardFooter class="justify-between gap-3">
        <Button variant="outline" type="button" disabled={pending} onclick={() => onCancel?.()}>
          Batal
        </Button>
        <Button type="submit" disabled={pending}>
          {pending ? 'Menyimpan…' : isEditing ? 'Simpan perubahan' : 'Buat Package'}
        </Button>
      </CardFooter>
    </form>
  </Card>
{/if}
