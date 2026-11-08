import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import { createTransactionFilterState } from '$lib/domains/transactions';
import TransactionManagementFixture from '../support/TransactionManagementFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';
import { loadContract } from '../support/contracts';

const panelPage = () => ({ status: 200, data: loadContract('admin-transactions.response.json') });
const packageOptions = () => ({
  status: 200,
  data: loadContract('admin-packages.response.json'),
});
const panelSession = () => ({
  status: 200,
  data: {
    data: { email: 'support@example.test', role: 'support', expires_at: '2026-10-24T08:00:00Z' },
  },
});

/** Every test needs the Package filter's options as well as the page itself. */
function stubPanel(
  transactions: Parameters<typeof stubApiRoutes>[0][number]['respond'],
  packages: Parameters<typeof stubApiRoutes>[0][number]['respond'] = packageOptions,
) {
  stubApiRoutes([
    { method: 'get', path: '/admin/transactions', respond: transactions },
    { method: 'get', path: '/admin/packages', respond: packages },
    // The screen decides whether to offer the reveal from the live session; the
    // backend refuses a role that may not reveal regardless (ADR 0024 §3).
    { method: 'get', path: '/admin/auth/session', respond: panelSession },
  ]);
}

describe('TransactionManagement', () => {
  afterEach(() => {
    restoreApi();
  });

  it('renders the snapshot, the state and the last SMS of each row', async () => {
    stubPanel(panelPage);

    render(TransactionManagementFixture);

    expect(await screen.findByText('6281234567890')).toBeInTheDocument();

    // The filter's <option>s carry the same labels as the rows, so assertions are
    // scoped to the table.
    const table = within(screen.getByRole('table'));
    // The name and amount are the row's snapshot, not today's catalog.
    expect(table.getByText('Paket Harian')).toBeInTheDocument();
    expect(table.getByText('Aktif (provisioned)')).toBeInTheDocument();
    expect(table.getByText('Terkirim')).toBeInTheDocument();
    expect(table.getByText('TRX-0123456789ABCDEF')).toBeInTheDocument();
    expect(screen.getByText('Menampilkan 1–1 dari 1 Transaction')).toBeInTheDocument();
  });

  it('has no PIN column and no reveal action', async () => {
    stubPanel(panelPage);

    render(TransactionManagementFixture);
    await screen.findByText('6281234567890');

    const headers = within(screen.getByRole('table'))
      .getAllByRole('columnheader')
      .map((header) => header.textContent ?? '');
    expect(headers.some((header) => /pin/i.test(header))).toBe(false);
    expect(screen.queryByRole('button', { name: /buka pin/i })).not.toBeInTheDocument();
  });

  it('refuses a row that carries a PIN instead of rendering it', async () => {
    const fixture = loadContract('admin-transactions.response.json') as {
      data: Record<string, unknown>[];
    };
    stubPanel(() => ({
      status: 200,
      data: {
        data: [{ ...fixture.data[0], pin: '123456' }],
        total_count: 1,
        page: 1,
        page_size: 20,
      },
    }));

    render(TransactionManagementFixture);

    // The strict schema is the defence: an unexpected field fails the read, and the
    // screen shows its error state rather than the credential.
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Respons server tidak sesuai dengan kontrak panel.',
    );
    expect(screen.queryByText('123456')).not.toBeInTheDocument();
  });

  it('explains an empty search instead of showing a bare table', async () => {
    stubPanel(() => ({
      status: 200,
      data: { data: [], total_count: 0, page: 1, page_size: 20 },
    }));

    render(TransactionManagementFixture);

    expect(await screen.findByText('Belum ada Transaction')).toBeInTheDocument();
  });

  it('names the filtered empty state when a search matched nothing', async () => {
    stubPanel(() => ({
      status: 200,
      data: { data: [], total_count: 0, page: 1, page_size: 20 },
    }));

    render(TransactionManagementFixture);
    await screen.findByText('Belum ada Transaction');

    await fireEvent.input(screen.getByLabelText('Phone Number'), {
      target: { value: '08123456789' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Cari' }));

    expect(await screen.findByText('Tidak ada Transaction yang cocok')).toBeInTheDocument();
  });

  it('offers a retry when the search cannot be loaded', async () => {
    stubPanel(() => ({
      status: 500,
      data: { error: { code: 'internal_error', message: 'Terjadi kesalahan pada server.' } },
    }));

    render(TransactionManagementFixture);

    expect(await screen.findByRole('alert')).toHaveTextContent('Terjadi kesalahan pada server.');
    expect(screen.getByRole('button', { name: 'Coba lagi' })).toBeInTheDocument();
  });

  it('sends the filters the operator submitted, not every keystroke', async () => {
    const requests: Record<string, unknown>[] = [];
    stubPanel((config) => {
      requests.push((config.params ?? {}) as Record<string, unknown>);
      return panelPage();
    });

    render(TransactionManagementFixture);
    await screen.findByText('6281234567890');
    expect(requests).toHaveLength(1);

    await fireEvent.input(screen.getByLabelText('Phone Number'), {
      target: { value: '08123456789' },
    });
    // Typing alone must not query; the operator may still be typing.
    expect(requests).toHaveLength(1);

    await fireEvent.click(screen.getByRole('button', { name: 'Cari' }));

    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[1]).toMatchObject({
      phone_number: '08123456789',
      page: 1,
      page_size: 20,
    });
  });

  it('lets the operator filter by Package, including a retired one', async () => {
    const requests: Record<string, unknown>[] = [];
    stubPanel((config) => {
      requests.push((config.params ?? {}) as Record<string, unknown>);
      return panelPage();
    });

    render(TransactionManagementFixture);
    await screen.findByText('6281234567890');
    // The option comes from the packages domain's own list, so a retired Package is
    // still a valid search.
    const option = await screen.findByRole('option', { name: /Paket Harian/ });
    await waitFor(() => expect(option).not.toBeDisabled());

    await fireEvent.change(screen.getByLabelText('Package'), {
      target: { value: '1' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Cari' }));

    await waitFor(() => expect(requests.length).toBeGreaterThan(1));
    expect(requests.at(-1)).toMatchObject({ package_id: 1 });
  });

  it('refreshes when Cari is pressed on the search already on screen', async () => {
    const requests: Record<string, unknown>[] = [];
    stubPanel((config) => {
      requests.push((config.params ?? {}) as Record<string, unknown>);
      return panelPage();
    });

    render(TransactionManagementFixture);
    await screen.findByText('6281234567890');
    expect(requests).toHaveLength(1);

    await fireEvent.click(screen.getByRole('button', { name: 'Cari' }));

    await waitFor(() => expect(requests).toHaveLength(2));
  });

  it('walks to the next page and keeps the pager reachable from an empty page', async () => {
    const fixture = loadContract('admin-transactions.response.json') as { data: unknown[] };
    const requests: Record<string, unknown>[] = [];
    stubPanel((config) => {
      const params = (config.params ?? {}) as Record<string, unknown>;
      requests.push(params);
      const page = Number(params.page ?? 1);
      return {
        status: 200,
        // Page 2 is empty: the operator must still be able to walk back.
        data: { data: page === 1 ? fixture.data : [], total_count: 25, page, page_size: 20 },
      };
    });

    render(TransactionManagementFixture);

    await fireEvent.click(await screen.findByRole('button', { name: 'Berikutnya' }));

    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[1]).toMatchObject({ page: 2 });
    expect(
      await screen.findByText('Halaman ini kosong; hasilnya ada di halaman sebelumnya.'),
    ).toBeInTheDocument();

    // Page 1 is still cached, so walking back is a cache hit, not another request:
    // what matters is that the screen returns to the rows.
    await fireEvent.click(screen.getByRole('button', { name: 'Sebelumnya' }));
    expect(await screen.findByText('Menampilkan 1–1 dari 25 Transaction')).toBeInTheDocument();
    expect(screen.getByText('6281234567890')).toBeInTheDocument();
  });

  it('starts a new search at page 1, so a filter cannot inherit a stale page', async () => {
    const fixture = loadContract('admin-transactions.response.json') as { data: unknown[] };
    const requests: Record<string, unknown>[] = [];
    stubPanel((config) => {
      const params = (config.params ?? {}) as Record<string, unknown>;
      requests.push(params);
      const page = Number(params.page ?? 1);
      return {
        status: 200,
        data: { data: fixture.data, total_count: 25, page, page_size: 20 },
      };
    });

    render(TransactionManagementFixture);
    await screen.findByText('6281234567890');

    await fireEvent.click(screen.getByRole('button', { name: 'Berikutnya' }));
    await waitFor(() => expect(requests.at(-1)).toMatchObject({ page: 2 }));

    await fireEvent.input(screen.getByLabelText('Phone Number'), {
      target: { value: '08123456789' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Cari' }));

    await waitFor(() =>
      expect(requests.at(-1)).toMatchObject({ page: 1, phone_number: '08123456789' }),
    );
  });
});

describe('TransactionFilterState', () => {
  it('resets the page when a search is applied and forgets everything on reset', () => {
    const filters = createTransactionFilterState();
    filters.goToPage(4);

    filters.apply({
      state: 'paid',
      phoneNumber: ' 08123456789 ',
      from: '2026-10-23',
      to: '2026-10-24',
      packageId: 3,
    });

    expect(filters.page).toBe(1);
    expect(
      filters.matches({
        state: 'paid',
        phoneNumber: '08123456789',
        from: '2026-10-23',
        to: '2026-10-24',
        packageId: 3,
      }),
    ).toBe(true);

    filters.reset();
    expect(filters.filter).toEqual({ page: 1, page_size: 20 });
  });
});

describe('Transaction detail row', () => {
  afterEach(() => {
    restoreApi();
  });

  it('opens one row at a time and offers the audited reveal to support', async () => {
    stubPanel(panelPage);

    render(TransactionManagementFixture);
    await screen.findByText('6281234567890');

    const detailButtons = screen.getAllByRole('button', { name: 'Detail' });
    await fireEvent.click(detailButtons[0]);

    const detail = await screen.findByTestId('transaction-detail');
    expect(detail).toHaveTextContent('TRX-0123456789ABCDEF');
    expect(within(detail).getByText(/tercatat di audit log/i)).toBeInTheDocument();
    expect(within(detail).getByRole('button', { name: 'Buka PIN' })).toBeInTheDocument();
  });

  it('shows no PIN in the list even after a row has been opened', async () => {
    stubPanel(panelPage);

    render(TransactionManagementFixture);
    await screen.findByText('6281234567890');

    await fireEvent.click(screen.getAllByRole('button', { name: 'Detail' })[0]);
    await screen.findByTestId('transaction-detail');

    // Opening the row is not a reveal: no digits exist until the endpoint is called.
    expect(screen.queryByTestId('revealed-pin')).not.toBeInTheDocument();
    expect(screen.queryByText('123456')).not.toBeInTheDocument();
  });
});
