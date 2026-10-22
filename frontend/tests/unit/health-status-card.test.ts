import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import HealthStatusCardFixture from '../support/HealthStatusCardFixture.svelte';
import { restoreApi, stubApi, stubNetworkFailure } from '../support/api';
import { loadContract } from '../support/contracts';

describe('HealthStatusCard', () => {
  afterEach(() => {
    restoreApi();
  });

  it('shows the backend, Postgres, MySQL FreeRADIUS and Redis as connected', async () => {
    stubApi(() => ({ status: 200, data: loadContract('health.response.json') }));

    render(HealthStatusCardFixture);

    expect(await screen.findByText('Normal')).toBeInTheDocument();
    expect(screen.getByText('Backend')).toBeInTheDocument();
    expect(screen.getByText('Postgres')).toBeInTheDocument();
    expect(screen.getByText('MySQL FreeRADIUS')).toBeInTheDocument();
    expect(screen.getByText('Redis')).toBeInTheDocument();
    expect(screen.getAllByText('Terhubung')).toHaveLength(4);
  });

  it('shows the degraded dependency and an Indonesian reason', async () => {
    stubApi(() => ({ status: 503, data: loadContract('health.degraded.response.json') }));

    render(HealthStatusCardFixture);

    expect(await screen.findByText('Terganggu')).toBeInTheDocument();
    expect(screen.getByText('Tidak dapat dihubungi.')).toBeInTheDocument();
    expect(screen.getByText('Tidak terhubung')).toBeInTheDocument();
    expect(screen.getAllByText('Terhubung')).toHaveLength(3);
  });

  it('retries when the backend cannot be reached and recovers', async () => {
    stubNetworkFailure();

    render(HealthStatusCardFixture);

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Tidak dapat menghubungi server');

    stubApi(() => ({ status: 200, data: loadContract('health.response.json') }));
    await fireEvent.click(screen.getByRole('button', { name: 'Coba lagi' }));

    expect(await screen.findByText('Normal')).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
    expect(screen.getAllByText('Terhubung')).toHaveLength(4);
  });
});
