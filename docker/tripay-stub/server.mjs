// Tripay stub for the end-to-end smoke journey (ADR 0018 §5).
//
// It speaks only what this system's adapter actually calls, so the journey can
// run without the vendor and without a line of fake-mode code in production:
//
//   POST /transaction/create                 what `CreateTransaction` calls
//   GET  /transaction/detail?reference=      what `GetStatus`/reconciliation calls
//   GET  /merchant/payment-channel           what `Channels` calls
//   GET  /bayar?reference=…                  the fake checkout page the buyer opens
//   POST /bayar                              signs a settlement and posts the callback
//   GET  /healthz                            compose healthcheck
//
// The signature is a real HMAC-SHA256 over the raw body with the same private
// key the backend verifies against, so the callback path under test is the
// production one (ADR 0018 §5).
import { createServer } from 'node:http';
import { createHmac } from 'node:crypto';

const PORT = Number(process.env.STUB_PORT ?? '8080');
const PRIVATE_KEY = process.env.TRIPAY_PRIVATE_KEY ?? '';
const CALLBACK_URL = process.env.TRIPAY_CALLBACK_URL ?? 'http://api:8080/api/payments/callback';
// Where a browser (the Playwright run) can reach this page: the compose override
// publishes the stub on the host, because only containers know `tripay-stub`.
const PUBLIC_URL = process.env.STUB_PUBLIC_URL ?? 'http://127.0.0.1:8082';

// Group names and fields mirror the vendor's wire contract, not a convenient
// subset of it: `type: direct` is what tells the adapter a channel is a closed
// payment, and `minimum_amount`/`maximum_amount` are what checkout filters on
// (ADR 0021). Tripay files QRIS under `E-Wallet`, so the stub does too — if the
// stub published a simpler shape, the code-prefix rule would never run here.
const CHANNELS = [
  {
    code: 'BRIVA',
    name: 'BRI Virtual Account',
    group: 'Virtual Account',
    type: 'direct',
    active: true,
    // Rp 10.000 is what the sandbox reports for a virtual account: the reason a
    // Rp 5.000 Package may not offer one.
    minimum_amount: 10000,
    maximum_amount: 10000000,
    fee_customer: { flat: 0, percent: 0 },
  },
  {
    code: 'QRIS',
    name: 'QRIS',
    group: 'E-Wallet',
    type: 'direct',
    active: true,
    // No published maximum: zero means "the vendor published no bound".
    minimum_amount: 1000,
    maximum_amount: 0,
    fee_customer: { flat: 0, percent: 0 },
  },
];

/** merchant_ref -> one created transaction, held in memory for the run. */
const transactions = new Map();

function channelName(code) {
  return CHANNELS.find((channel) => channel.code === code)?.name ?? code;
}

function sendJson(response, status, body) {
  const payload = JSON.stringify(body);
  response.writeHead(status, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(payload),
  });
  response.end(payload);
}

function sendHtml(response, status, body) {
  response.writeHead(status, { 'Content-Type': 'text/html; charset=utf-8' });
  response.end(body);
}

async function readBody(request) {
  const chunks = [];
  for await (const chunk of request) {
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString('utf8');
}

function escapeHtml(value) {
  return String(value).replace(
    /[&<>"']/g,
    (character) =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[character],
  );
}

function createTransaction(form) {
  const merchantReference = form.get('merchant_ref') ?? '';
  const channel = form.get('method') ?? 'BRIVA';
  const record = {
    merchantReference,
    channel,
    amount: Number(form.get('amount') ?? '0'),
    customerPhone: form.get('customer_phone') ?? '',
    callbackUrl: form.get('callback_url') || CALLBACK_URL,
    expiresAt: Number(form.get('expired_time') ?? '0'),
    paidAt: null,
  };
  transactions.set(merchantReference, record);

  // A pay code the buyer can read back on the app's instruction screen; QRIS
  // gets a QR payload instead, which is what the real vendor does.
  const isQris = channel === 'QRIS';

  return {
    success: true,
    message: '',
    data: {
      reference: `STUB-${merchantReference}`,
      merchant_ref: merchantReference,
      payment_name: channelName(channel),
      pay_code: isQris ? '' : `8808${String(record.amount).padStart(10, '0')}`,
      pay_url: `${PUBLIC_URL}/bayar?reference=${encodeURIComponent(merchantReference)}`,
      qr_string: isQris ? `00020101021226${merchantReference}5802ID` : '',
      qr_url: '',
      amount: record.amount,
      expired_time: record.expiresAt,
      instructions: [
        {
          title: 'Instruksi Pembayaran',
          steps: [
            'Buka halaman bayar stub ini.',
            'Tekan tombol Bayar sekarang.',
            'Kembali ke aplikasi dan tunggu PIN Anda muncul.',
          ],
        },
      ],
    },
  };
}

function detail(reference) {
  const record = transactions.get(reference);
  if (!record) {
    return { success: false, message: 'transaction not found', data: null };
  }
  return {
    success: true,
    message: '',
    data: {
      reference: `STUB-${record.merchantReference}`,
      merchant_ref: record.merchantReference,
      status: record.paidAt === null ? 'UNPAID' : 'PAID',
      amount: record.amount,
      paid_at: record.paidAt,
    },
  };
}

function payPage(reference) {
  const record = transactions.get(reference);
  if (!record) {
    return `<h1>Transaksi tidak dikenal</h1><p>Stub Tripay tidak punya reference ${escapeHtml(
      reference,
    )}.</p>`;
  }
  return `<!doctype html>
<html lang="id"><head><meta charset="utf-8"><title>Stub Tripay — bayar</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family: system-ui, sans-serif; max-width: 32rem; margin: 3rem auto; padding: 0 1rem">
  <h1>Stub Tripay</h1>
  <p>Reference <code>${escapeHtml(record.merchantReference)}</code></p>
  <p>Kanal ${escapeHtml(channelName(record.channel))} · Jumlah <strong>${record.amount}</strong></p>
  <form method="POST" action="/bayar">
    <input type="hidden" name="reference" value="${escapeHtml(record.merchantReference)}">
    <button type="submit" style="font-size: 1rem; padding: 0.6rem 1rem">Bayar sekarang</button>
  </form>
</body></html>`;
}

async function settle(reference) {
  const record = transactions.get(reference);
  if (!record) {
    return { status: 404, detail: 'reference tidak dikenal' };
  }
  if (record.paidAt === null) {
    record.paidAt = Math.floor(Date.now() / 1000);
  }

  // The vendor owns the amount and the reference it echoes back: the backend
  // refuses a callback whose amount does not match the Transaction (ADR 0015).
  const body = JSON.stringify({
    success: true,
    event: 'payment_status',
    data: {
      reference: `STUB-${record.merchantReference}`,
      merchant_ref: record.merchantReference,
      status: 'PAID',
      amount: record.amount,
      paid_at: record.paidAt,
    },
  });
  const signature = createHmac('sha256', PRIVATE_KEY).update(body).digest('hex');

  try {
    const response = await fetch(record.callbackUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Callback-Signature': signature },
      body,
    });
    const detailText = await response.text();
    process.stdout.write(
      `tripay-stub: callback ${record.merchantReference} -> HTTP ${response.status}\n`,
    );
    return { status: response.status, detail: detailText.slice(0, 400) };
  } catch (error) {
    process.stdout.write(`tripay-stub: callback failed ${record.merchantReference}: ${error}\n`);
    return { status: 0, detail: String(error) };
  }
}

const server = createServer(async (request, response) => {
  const url = new URL(request.url ?? '/', `http://localhost:${PORT}`);

  if (request.method === 'GET' && url.pathname === '/healthz') {
    sendJson(response, 200, { status: 'ok' });
    return;
  }

  if (request.method === 'POST' && url.pathname === '/transaction/create') {
    const form = new URLSearchParams(await readBody(request));
    if (!form.get('merchant_ref')) {
      sendJson(response, 400, { success: false, message: 'merchant_ref is required', data: null });
      return;
    }
    const created = createTransaction(form);
    process.stdout.write(`tripay-stub: created ${form.get('merchant_ref')} (${form.get('method')})\n`);
    sendJson(response, 200, created);
    return;
  }

  if (request.method === 'GET' && url.pathname === '/transaction/detail') {
    sendJson(response, 200, detail(url.searchParams.get('reference') ?? ''));
    return;
  }

  if (request.method === 'GET' && url.pathname === '/merchant/payment-channel') {
    sendJson(response, 200, { success: true, message: '', data: CHANNELS });
    return;
  }

  if (request.method === 'GET' && url.pathname === '/bayar') {
    sendHtml(response, 200, payPage(url.searchParams.get('reference') ?? ''));
    return;
  }

  if (request.method === 'POST' && url.pathname === '/bayar') {
    const form = new URLSearchParams(await readBody(request));
    const reference = form.get('reference') ?? '';
    const result = await settle(reference);
    sendHtml(
      response,
      result.status >= 200 && result.status < 300 ? 200 : 502,
      `<!doctype html>
<html lang="id"><head><meta charset="utf-8"><title>Stub Tripay — hasil</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family: system-ui, sans-serif; max-width: 32rem; margin: 3rem auto; padding: 0 1rem">
  <h1>Callback dikirim</h1>
  <p>Reference <code>${escapeHtml(reference)}</code></p>
  <p>Backend menjawab <strong data-testid="callback-status">${result.status}</strong></p>
  <pre style="white-space: pre-wrap">${escapeHtml(result.detail)}</pre>
</body></html>`,
    );
    return;
  }

  if (request.method === 'GET' && url.pathname === '/') {
    const rows = [...transactions.values()]
      .map(
        (record) =>
          `<li><a href="/bayar?reference=${encodeURIComponent(record.merchantReference)}">${escapeHtml(
            record.merchantReference,
          )}</a> — ${record.amount} — ${record.paidAt === null ? 'UNPAID' : 'PAID'}</li>`,
      )
      .join('');
    sendHtml(
      response,
      200,
      `<!doctype html><html lang="id"><head><meta charset="utf-8"><title>Stub Tripay</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto">
<h1>Stub Tripay</h1><ul>${rows || '<li>belum ada transaksi</li>'}</ul></body></html>`,
    );
    return;
  }

  sendJson(response, 404, { success: false, message: 'not found', data: null });
});

server.listen(PORT, () => {
  if (PRIVATE_KEY === '') {
    process.stdout.write('tripay-stub: TRIPAY_PRIVATE_KEY is empty, callbacks cannot be signed\n');
  }
  process.stdout.write(`tripay-stub listening on http://0.0.0.0:${PORT}\n`);
});
