export const PACKAGE_FIXTURE = {
  id: 1,
  nama: "Paket Harian",
  harga: "Rp 5.000",
  durasiActivePeriod: "1 Hari",
  batasKecepatan: "10M",
};

/** Point the deployment config at stubbed backend/purchase URLs for this page load. */
export async function configureDeployment(page, overrides) {
  await page.route("**/config/data/voucher.js*", async (route) => {
    const response = await route.fetch();
    let body = await response.text();
    if (overrides.backendBaseUrl !== undefined) {
      body = body.replace('backendBaseUrl: ""', `backendBaseUrl: "${overrides.backendBaseUrl}"`);
    }
    if (overrides.appBaseUrl !== undefined) {
      body = body.replace('appBaseUrl: ""', `appBaseUrl: "${overrides.appBaseUrl}"`);
    }
    await route.fulfill({ response, body });
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
