import { fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import { loadContract } from '../support/contracts';
import AdminCreateFormFixture from '../support/AdminCreateFormFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';

describe('AdminCreateForm', () => {
  afterEach(() => {
    restoreApi();
  });

  it('refuses a weak password before the request leaves the panel', async () => {
    render(AdminCreateFormFixture);

    await fireEvent.input(screen.getByLabelText('Email'), {
      target: { value: 'support@example.test' },
    });
    await fireEvent.input(screen.getByLabelText('Kata sandi awal'), {
      target: { value: 'pendek' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Tambah admin' }));

    expect(await screen.findByText('Kata sandi minimal 12 karakter.')).toBeInTheDocument();
  });

  it('shows the server reason when the email is already taken', async () => {
    stubApiRoutes([
      {
        method: 'post',
        path: '/admin/admins',
        respond: () => ({
          status: 409,
          data: { error: { code: 'conflict', message: 'Email sudah dipakai admin lain.' } },
        }),
      },
    ]);

    render(AdminCreateFormFixture);

    await fireEvent.input(screen.getByLabelText('Email'), {
      target: { value: 'owner@example.test' },
    });
    await fireEvent.input(screen.getByLabelText('Kata sandi awal'), {
      target: { value: 'kata-sandi-yang-panjang' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Tambah admin' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Email sudah dipakai admin lain.');
  });

  it('adds the colleague and clears the form', async () => {
    const roster = loadContract('admin-admins.response.json') as {
      data: Record<string, unknown>[];
    };
    const created = roster.data[1];

    stubApiRoutes([
      {
        method: 'post',
        path: '/admin/admins',
        respond: () => ({ status: 201, data: { data: created } }),
      },
    ]);

    render(AdminCreateFormFixture);

    await fireEvent.input(screen.getByLabelText('Email'), {
      target: { value: 'support@example.test' },
    });
    await fireEvent.input(screen.getByLabelText('Kata sandi awal'), {
      target: { value: 'kata-sandi-yang-panjang' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Tambah admin' }));

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Admin support@example.test ditambahkan.',
    );
    expect(screen.getByLabelText('Email')).toHaveValue('');
  });
});
