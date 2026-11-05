<script lang="ts">
  import { resolve } from '$app/paths';

  import PageHeader from '$lib/components/shared/PageHeader.svelte';
  import {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
  } from '$lib/components/ui/card';
  import { ChangePasswordForm, createSessionQuery, roleLabels } from '$lib/domains/auth';

  const session = createSessionQuery();
</script>

<svelte:head>
  <title>Beranda — Panel Admin</title>
</svelte:head>

{#if session.data}
  {@const identity = session.data}
  <div class="space-y-6">
    <PageHeader
      title="Beranda"
      description={`Masuk sebagai ${identity.email} (${roleLabels[identity.role]}).`}
    />

    <div class="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_24rem]">
      <Card>
        <CardHeader>
          <CardTitle>Yang bisa Anda lakukan</CardTitle>
          <CardDescription>
            Aksi di bawah ini mengikuti peran Anda. Penegakannya tetap di server.
          </CardDescription>
        </CardHeader>
        <CardContent class="space-y-3 text-sm">
          <p>
            <a class="font-medium underline-offset-4 hover:underline" href={resolve('/paket')}
              >Katalog Package</a
            >
            — lihat seluruh Package, termasuk yang sudah nonaktif.
            {#if identity.role === 'owner'}
              Owner juga bisa membuat, mengubah, menonaktifkan, dan mengaktifkan kembali.
            {:else}
              Peran Anda hanya membaca katalog.
            {/if}
          </p>
          {#if identity.role === 'owner'}
            <p>
              <a class="font-medium underline-offset-4 hover:underline" href={resolve('/admin')}
                >Kelola admin</a
              >
              — tambah anggota tim, ganti peran, reset kata sandi, dan nonaktifkan admin.
            </p>
          {/if}
        </CardContent>
      </Card>

      <ChangePasswordForm email={identity.email} />
    </div>
  </div>
{/if}
