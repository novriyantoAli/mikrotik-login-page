import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import AuditLogFixture from '../support/AuditLogFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';
import { loadContract } from '../support/contracts';

const auditPage = () => ({ status: 200, data: loadContract('admin-audit.response.json') });

describe('AuditLog', () => {
  afterEach(() => {
    restoreApi();
  });

  it('renders the actor, the act and its target for every row', async () => {
    stubApiRoutes([{ method: 'get', path: '/admin/audit', respond: auditPage }]);

    render(AuditLogFixture);

    expect(await screen.findByText('owner@example.test')).toBeInTheDocument();

    const table = within(screen.getByRole('table'));
    // The act reads as copy, with its wire name underneath.
    expect(table.getByText('Ubah Package')).toBeInTheDocument();
    expect(table.getByText('package.update')).toBeInTheDocument();
    expect(table.getByText('Buka PIN')).toBeInTheDocument();
    expect(table.getByText('pin.reveal')).toBeInTheDocument();
    // The reveal row still names its Transaction, even though the row is gone from
    // the Transaction list.
    expect(table.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('Menampilkan 1–2 dari 2 baris audit')).toBeInTheDocument();
  });

  it('shows the before and after of a catalog change when the payload is opened', async () => {
    stubApiRoutes([{ method: 'get', path: '/admin/audit', respond: auditPage }]);

    render(AuditLogFixture);
    await screen.findByText('owner@example.test');

    await fireEvent.click(screen.getAllByRole('button', { name: 'Lihat payload' })[0]);

    const payload = await screen.findByTestId('audit-payload');
    expect(within(payload).getByText('Harga')).toBeInTheDocument();
    // Both sides of the change are on screen: 5000 → 7500.
    expect(within(payload).getByText('5000')).toBeInTheDocument();
    expect(within(payload).getByText('7500')).toBeInTheDocument();
  });

  it('offers no control that could change or remove a row', async () => {
    stubApiRoutes([{ method: 'get', path: '/admin/audit', respond: auditPage }]);

    render(AuditLogFixture);
    await screen.findByText('owner@example.test');

    expect(
      screen.queryByRole('button', { name: /hapus|edit|ubah|bersihkan/i }),
    ).not.toBeInTheDocument();
    const headers = within(screen.getByRole('table'))
      .getAllByRole('columnheader')
      .map((header) => header.textContent ?? '');
    expect(headers.some((header) => /hapus|ubah|edit/i.test(header))).toBe(false);
  });

  it('never renders a plaintext PIN', async () => {
    stubApiRoutes([{ method: 'get', path: '/admin/audit', respond: auditPage }]);

    render(AuditLogFixture);
    await screen.findByText('owner@example.test');

    // Open every payload: the reveal row says what happened, never the digits.
    for (const button of screen.getAllByRole('button', { name: 'Lihat payload' })) {
      await fireEvent.click(button);
    }
    expect(document.body.textContent ?? '').not.toMatch(/\b\d{6}\b/);
  });

  it('handles the loading state with a skeleton, not a blank screen', async () => {
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/audit',
        respond: () => new Promise<never>(() => {}),
      },
    ]);

    render(AuditLogFixture);

    expect(screen.getByRole('status')).toHaveTextContent('Memuat jejak audit');
  });

  it('offers a retry when the trail cannot be loaded', async () => {
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/audit',
        respond: () => ({
          status: 500,
          data: { error: { code: 'internal_error', message: 'Terjadi kesalahan pada server.' } },
        }),
      },
    ]);

    render(AuditLogFixture);

    expect(await screen.findByRole('alert')).toHaveTextContent('Terjadi kesalahan pada server.');
    expect(screen.getByRole('button', { name: 'Coba lagi' })).toBeInTheDocument();
  });

  it('shows an empty state, and names the filtered one when a search matched nothing', async () => {
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/audit',
        respond: () => ({
          status: 200,
          data: { data: [], total_count: 0, page: 1, page_size: 20 },
        }),
      },
    ]);

    render(AuditLogFixture);
    expect(await screen.findByText('Belum ada jejak audit')).toBeInTheDocument();

    await fireEvent.input(screen.getByLabelText('Email admin'), {
      target: { value: 'owner@example.test' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Cari' }));

    expect(await screen.findByText('Tidak ada baris audit yang cocok')).toBeInTheDocument();
  });

  it('sends the filters the auditor submitted, not every keystroke', async () => {
    const requests: Record<string, unknown>[] = [];
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/audit',
        respond: (config) => {
          requests.push((config.params ?? {}) as Record<string, unknown>);
          return auditPage();
        },
      },
    ]);

    render(AuditLogFixture);
    await screen.findByText('owner@example.test');
    expect(requests).toHaveLength(1);

    await fireEvent.input(screen.getByLabelText('Email admin'), {
      target: { value: 'owner@example.test' },
    });
    // Typing alone must not query; the auditor may still be typing.
    expect(requests).toHaveLength(1);

    await fireEvent.change(screen.getByLabelText('Aksi'), {
      target: { value: 'package.update' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Cari' }));

    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[1]).toMatchObject({
      admin_email: 'owner@example.test',
      action: 'package.update',
      page: 1,
      page_size: 20,
    });
  });

  it('walks to the next page and keeps the pager reachable from an empty page', async () => {
    const fixture = loadContract('admin-audit.response.json') as { data: unknown[] };
    const requests: Record<string, unknown>[] = [];
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/audit',
        respond: (config) => {
          const params = (config.params ?? {}) as Record<string, unknown>;
          requests.push(params);
          const page = Number(params.page ?? 1);
          return {
            status: 200,
            data: { data: page === 1 ? fixture.data : [], total_count: 25, page, page_size: 20 },
          };
        },
      },
    ]);

    render(AuditLogFixture);

    await fireEvent.click(await screen.findByRole('button', { name: 'Berikutnya' }));

    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[1]).toMatchObject({ page: 2 });
    expect(
      await screen.findByText('Halaman ini kosong; hasilnya ada di halaman sebelumnya.'),
    ).toBeInTheDocument();

    await fireEvent.click(screen.getByRole('button', { name: 'Sebelumnya' }));
    expect(await screen.findByText('Menampilkan 1–2 dari 25 baris audit')).toBeInTheDocument();
  });
});

describe('audit filter state', () => {
  it('is UI-only state on the domain barrier', async () => {
    // The state factory is exported through the barrel, and a screen (or a test)
    // creates one instance per mount instead of sharing an applied search.
    const { createAuditFilterState } = await import('$lib/domains/audit');

    const filters = createAuditFilterState();
    filters.goToPage(4);
    filters.apply({
      adminEmail: ' owner@example.test ',
      action: 'pin.reveal',
      from: '2026-10-23',
      to: '2026-10-24',
    });

    expect(filters.page).toBe(1);
    expect(
      filters.matches({
        adminEmail: 'owner@example.test',
        action: 'pin.reveal',
        from: '2026-10-23',
        to: '2026-10-24',
      }),
    ).toBe(true);

    filters.reset();
    expect(filters.filter).toEqual({ page: 1, page_size: 20 });
  });
});
