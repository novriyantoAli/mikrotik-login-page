import { fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import { signedOutMessage } from '$lib/domains/auth/schemas/auth.schema';
import ShellFixture from '../support/ShellFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';
import { loadContract } from '../support/contracts';

const unauthorized = {
  status: 401,
  data: { error: { code: 'unauthorized', message: 'Sesi admin tidak ditemukan.' } },
};

describe('panel shell', () => {
  afterEach(() => {
    restoreApi();
  });

  it('asks for a password when there is no session, then opens the panel', async () => {
    stubApiRoutes([
      { method: 'get', path: '/admin/auth/session', respond: () => unauthorized },
      {
        method: 'post',
        path: '/admin/auth/login',
        respond: () => ({ status: 200, data: loadContract('admin-session.response.json') }),
      },
    ]);

    render(ShellFixture);

    expect(await screen.findByRole('button', { name: 'Masuk' })).toBeInTheDocument();

    await fireEvent.input(screen.getByLabelText('Email'), {
      target: { value: 'owner@example.test' },
    });
    await fireEvent.input(screen.getByLabelText('Kata sandi'), {
      target: { value: 'kata-sandi-yang-panjang' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Masuk' }));

    expect(await screen.findByRole('link', { name: 'Package' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Admin' })).toBeInTheDocument();
    expect(screen.getByText('owner@example.test')).toBeInTheDocument();
    // The page behind the layout is rendered only with a live session.
    expect(screen.getByText('Konten halaman')).toBeInTheDocument();
  });

  it('shows one uniform message when the credentials are refused', async () => {
    stubApiRoutes([
      { method: 'get', path: '/admin/auth/session', respond: () => unauthorized },
      {
        method: 'post',
        path: '/admin/auth/login',
        respond: () => ({
          status: 401,
          data: {
            error: { code: 'unauthorized', message: 'Email atau kata sandi salah.' },
          },
        }),
      },
    ]);

    render(ShellFixture);

    await fireEvent.input(await screen.findByLabelText('Email'), {
      target: { value: 'owner@example.test' },
    });
    await fireEvent.input(screen.getByLabelText('Kata sandi'), {
      target: { value: 'kata-sandi-yang-salah' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Masuk' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Email atau kata sandi salah.');
    // The form stays, so a typo is retryable.
    expect(screen.getByRole('button', { name: 'Masuk' })).toBeInTheDocument();
  });

  it('hides the team screen from a role that may not manage admins', async () => {
    const session = loadContract('admin-session.response.json') as {
      data: Record<string, unknown>;
    };
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/auth/session',
        respond: () => ({ status: 200, data: { data: { ...session.data, role: 'auditor' } } }),
      },
    ]);

    render(ShellFixture);

    expect(await screen.findByRole('link', { name: 'Package' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Admin' })).not.toBeInTheDocument();
  });

  it('signs out immediately and says so', async () => {
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/auth/session',
        respond: () => ({ status: 200, data: loadContract('admin-session.response.json') }),
      },
      { method: 'post', path: '/admin/auth/logout', respond: () => ({ status: 204 }) },
    ]);

    render(ShellFixture);

    await fireEvent.click(await screen.findByRole('button', { name: 'Keluar' }));

    expect(await screen.findByText(signedOutMessage)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Masuk' })).toBeInTheDocument();
  });
});
