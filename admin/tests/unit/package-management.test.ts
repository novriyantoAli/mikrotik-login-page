import { render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import PackageManagementFixture from '../support/PackageManagementFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';
import { loadContract } from '../support/contracts';

const catalog = () => ({ status: 200, data: loadContract('admin-packages.response.json') });

describe('PackageManagement', () => {
  afterEach(() => {
    restoreApi();
  });

  it('renders the whole catalog with its write actions for an owner', async () => {
    stubApiRoutes([{ method: 'get', path: '/admin/packages', respond: catalog }]);

    render(PackageManagementFixture, { canWrite: true });

    expect(await screen.findByText('Paket Harian')).toBeInTheDocument();
    expect(screen.getByText('Aktif')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Ubah' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Nonaktifkan' })).toBeInTheDocument();
    expect(screen.getByText('Package Baru')).toBeInTheDocument();
  });

  it('shows the catalog without write actions for support and auditor', async () => {
    stubApiRoutes([{ method: 'get', path: '/admin/packages', respond: catalog }]);

    render(PackageManagementFixture, { canWrite: false });

    expect(await screen.findByText('Paket Harian')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Ubah' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Nonaktifkan' })).not.toBeInTheDocument();
    expect(screen.queryByText('Package Baru')).not.toBeInTheDocument();
  });

  it('explains an empty catalog instead of showing a bare table', async () => {
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/packages',
        respond: () => ({ status: 200, data: { data: [] } }),
      },
    ]);

    render(PackageManagementFixture);

    expect(await screen.findByText('Belum ada Package')).toBeInTheDocument();
  });

  it('offers a retry when the catalog cannot be loaded', async () => {
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/packages',
        respond: () => ({
          status: 500,
          data: { error: { code: 'internal_error', message: 'Terjadi kesalahan pada server.' } },
        }),
      },
    ]);

    render(PackageManagementFixture);

    expect(await screen.findByRole('alert')).toHaveTextContent('Terjadi kesalahan pada server.');
    expect(screen.getByRole('button', { name: 'Coba lagi' })).toBeInTheDocument();
  });
});
