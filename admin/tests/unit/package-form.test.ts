import { fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import {
  AdminPackageSchema,
  type AdminPackage,
} from '$lib/domains/packages/schemas/package.schema';
import PackageFormFixture from '../support/PackageFormFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';
import { loadContract } from '../support/contracts';

function fixturePackage(): AdminPackage {
  const fixture = loadContract('admin-packages.response.json') as { data: unknown[] };
  return AdminPackageSchema.parse(fixture.data[0]);
}

describe('PackageForm', () => {
  afterEach(() => {
    restoreApi();
  });

  it('explains every empty field per field', async () => {
    render(PackageFormFixture);

    await fireEvent.click(screen.getByRole('button', { name: 'Buat Package' }));

    expect(await screen.findByText('Code paket wajib diisi.')).toBeInTheDocument();
    expect(screen.getByText('Nama paket wajib diisi.')).toBeInTheDocument();
    expect(screen.getByText(/Harga harus bilangan bulat lebih dari 0/)).toBeInTheDocument();
    expect(screen.getByText(/Durasi harus lebih dari 0/)).toBeInTheDocument();
    expect(screen.getByText('Batas kecepatan wajib diisi.')).toBeInTheDocument();
  });

  it('refuses a rate limit that is not the MikroTik shape', async () => {
    render(PackageFormFixture);

    await fireEvent.input(screen.getByLabelText('Code'), { target: { value: 'harian' } });
    await fireEvent.input(screen.getByLabelText('Nama'), { target: { value: 'Paket Harian' } });
    await fireEvent.input(screen.getByLabelText('Harga (Rp)'), { target: { value: '5000' } });
    await fireEvent.input(screen.getByLabelText('Durasi (menit)'), {
      target: { value: '1440' },
    });
    await fireEvent.input(screen.getByLabelText('Rate limit'), { target: { value: '5M' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Buat Package' }));

    expect(
      await screen.findByText('Batas kecepatan harus berbentuk unduh/unggah seperti 5M/5M.'),
    ).toBeInTheDocument();
  });

  it('creates a Package and reports the saved row', async () => {
    const row = fixturePackage();
    stubApiRoutes([
      {
        method: 'post',
        path: '/admin/packages',
        respond: () => ({ status: 201, data: { data: row } }),
      },
    ]);

    render(PackageFormFixture);

    await fireEvent.input(screen.getByLabelText('Code'), { target: { value: 'harian' } });
    await fireEvent.input(screen.getByLabelText('Nama'), { target: { value: 'Paket Harian' } });
    await fireEvent.input(screen.getByLabelText('Harga (Rp)'), { target: { value: '5000' } });
    await fireEvent.input(screen.getByLabelText('Durasi (menit)'), {
      target: { value: '1440' },
    });
    await fireEvent.input(screen.getByLabelText('Rate limit'), { target: { value: '5M/5M' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Buat Package' }));

    expect(await screen.findByText('Tersimpan: Paket Harian')).toBeInTheDocument();
  });

  it('shows the current row on a stale edit and resubmits at its version', async () => {
    const before = fixturePackage();
    const current = {
      ...before,
      name: 'Paket Harian Baru',
      price_idr: 7000,
      version: 2,
    };

    const bodies: Array<Record<string, unknown>> = [];
    stubApiRoutes([
      {
        method: 'put',
        path: `/admin/packages/${before.id}`,
        respond: (config) => {
          bodies.push(JSON.parse(String(config.data)) as Record<string, unknown>);
          if (bodies.length === 1) {
            return {
              status: 409,
              data: {
                error: {
                  code: 'conflict',
                  message: 'Paket sudah diubah orang lain. Muat ulang lalu ulangi.',
                  current,
                },
              },
            };
          }
          return { status: 200, data: { data: current } };
        },
      },
    ]);

    render(PackageFormFixture, { pkg: before });

    await fireEvent.click(screen.getByRole('button', { name: 'Simpan perubahan' }));

    expect(await screen.findByText('Paket sudah diubah orang lain.')).toBeInTheDocument();
    expect(screen.getByText(/versi 2/)).toBeInTheDocument();
    expect(screen.getByLabelText('Nama')).toHaveValue('Paket Harian Baru');

    await fireEvent.click(screen.getByRole('button', { name: 'Simpan perubahan' }));

    expect(await screen.findByText('Tersimpan: Paket Harian Baru')).toBeInTheDocument();
    expect(bodies).toHaveLength(2);
    expect(bodies[0].version).toBe(1);
    // The second submit is based on the row the operator was just shown.
    expect(bodies[1].version).toBe(2);
  });
});
