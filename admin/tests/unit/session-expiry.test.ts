import { fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import { AppError } from '$lib/api/client';
import { authApi, watchSessionExpiry } from '$lib/domains/auth/api/auth.api';
import { sessionExpiredMessage } from '$lib/domains/auth/schemas/auth.schema';
import { packageApi } from '$lib/domains/packages/api/package.api';
import ShellFixture from '../support/ShellFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';
import { loadContract } from '../support/contracts';

const expired = {
  status: 401,
  data: { error: { code: 'unauthorized', message: 'Sesi admin tidak ditemukan.' } },
};

describe('session expiry', () => {
  afterEach(() => {
    restoreApi();
  });

  it('notifies the panel when a session-bearing request answers 401', async () => {
    stubApiRoutes([{ method: 'get', path: '/admin/packages', respond: () => expired }]);

    const seen: AppError[] = [];
    const stop = watchSessionExpiry((error) => seen.push(error));

    await expect(packageApi.list()).rejects.toBeInstanceOf(AppError);

    stop();
    expect(seen).toHaveLength(1);
    expect(seen[0].status).toBe(401);
  });

  it('does not treat a refused sign-in as an expired session', async () => {
    stubApiRoutes([
      {
        method: 'post',
        path: '/admin/auth/login',
        respond: () => ({
          status: 401,
          data: { error: { code: 'unauthorized', message: 'Email atau kata sandi salah.' } },
        }),
      },
      { method: 'get', path: '/admin/auth/session', respond: () => expired },
    ]);

    const seen: AppError[] = [];
    const stop = watchSessionExpiry((error) => seen.push(error));

    await expect(
      authApi.login({ email: 'owner@example.test', password: 'kata-sandi-yang-salah' }),
    ).rejects.toBeInstanceOf(AppError);
    // A missing session is an answer the shell already handles, not an expiry.
    await expect(authApi.session()).resolves.toBeNull();

    stop();
    expect(seen).toHaveLength(0);
  });

  it('returns the operator to the login form with a message', async () => {
    stubApiRoutes([
      {
        method: 'get',
        path: '/admin/auth/session',
        respond: () => ({ status: 200, data: loadContract('admin-session.response.json') }),
      },
      { method: 'get', path: '/admin/packages', respond: () => expired },
      {
        method: 'post',
        path: '/admin/auth/login',
        respond: () => ({ status: 200, data: loadContract('admin-session.response.json') }),
      },
    ]);

    render(ShellFixture);

    expect(await screen.findByRole('link', { name: 'Package' })).toBeInTheDocument();

    // Something behind the shell needed the session and got 401.
    await expect(packageApi.list()).rejects.toBeInstanceOf(AppError);

    expect(await screen.findByText(sessionExpiredMessage)).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'Masuk' })).toBeInTheDocument();

    // Signing in again clears the notice: it would no longer be true.
    await fireEvent.input(screen.getByLabelText('Email'), {
      target: { value: 'owner@example.test' },
    });
    await fireEvent.input(screen.getByLabelText('Kata sandi'), {
      target: { value: 'kata-sandi-yang-panjang' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Masuk' }));

    expect(await screen.findByRole('link', { name: 'Package' })).toBeInTheDocument();
    expect(screen.queryByText(sessionExpiredMessage)).not.toBeInTheDocument();
  });
});
