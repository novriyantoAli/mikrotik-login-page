export const PACKAGE_FIXTURE = {
  id: 1,
  nama: "Paket Harian",
  harga: "Rp 5.000",
  durasiActivePeriod: "1 Hari",
  batasKecepatan: "10M",
};

/**
 * Point the deployment config at stubbed backend/purchase URLs for this page load.
 * The overrides are appended as assignments instead of rewriting the deployment's
 * own values: `config/data/voucher.js` is a tracked file a site is expected to
 * edit (backend URL, app URL), and a literal replacement silently stops working
 * the moment that file is configured — which made the suite fail on a machine
 * that had simply filled in its deployment URLs.
 */
export async function configureDeployment(page, overrides) {
  await page.route("**/config/data/voucher.js*", async (route) => {
    const response = await route.fetch();
    const body = await response.text();
    const assignments = Object.entries(overrides)
      .filter(([, value]) => value !== undefined)
      .map(([key, value]) => `cfgVoucher.${key} = ${JSON.stringify(value)};`)
      .join("\n");

    await route.fulfill({ response, body: body + "\n" + assignments + "\n" });
  });
}

/**
 * Enable/disable login modes (cfgLogin.modes) for this page load. Modes left
 * out keep their deployed default.
 */
export async function configureLoginModes(page, modes) {
  await page.route("**/config/data/login.js*", async (route) => {
    const response = await route.fetch();
    let body = await response.text();
    const assignments = Object.entries(modes)
      .map(([id, status]) => `cfgLogin.modes["${id}"].status = ${status ? 1 : 0};`)
      .join("\n");
    body += "\n" + assignments + "\n";
    await route.fulfill({ response, body });
  });
}

export async function stubPackageBackend(page, packages) {
  await page.route("https://api.test/packages", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ data: packages }),
    });
  });
}

export async function stubActivePeriodExpiry(page, expiry) {
  await page.route("https://api.test/active-period*", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ data: { expiry } }),
    });
  });
}

/** Serve a variant of a RouterOS page with template values substituted. */
export async function servePage(page, pattern, substitutions) {
  await page.route(pattern, async (route) => {
    const response = await route.fetch();
    let body = await response.text();
    for (const [from, to] of Object.entries(substitutions)) {
      body = body.split(from).join(to);
    }
    await route.fulfill({ response, body });
  });
}
