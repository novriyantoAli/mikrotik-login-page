import { test, expect } from "@playwright/test";
import { createHash } from "node:crypto";
import { PACKAGE_FIXTURE, configureDeployment, configureLoginModes, stubPackageBackend, servePage } from "./helpers.mjs";

const serveLoginPage = (page, substitutions) => servePage(page, "**/login.html*", substitutions);

test.describe("Login Page smoke", () => {
  test("shows a clear Package error and keeps hotspot login usable when no backend is configured", async ({ page }) => {
    const voucherProblems = [];
    page.on("console", (message) => {
      if (message.type() === "error" && /voucher/i.test(message.text())) {
        voucherProblems.push(message.text());
      }
    });
    page.on("pageerror", (error) => {
      if (/voucher/i.test(String(error))) {
        voucherProblems.push(String(error));
      }
    });

    await page.goto("/login.html");

    // The existing Login Page still renders its login form.
    await expect(page).toHaveTitle(/KOPINETGO/i);
    await expect(page.locator("#loginPage")).toBeVisible();
    await expect(page.locator("#username")).toBeVisible();
    await expect(page.locator("#btnLogin")).toBeVisible();
    await expect(page.locator("#btnLogin")).toBeEnabled();

    // The voucher client is exposed as one public interface.
    const exposed = await page.evaluate(() => {
      const api = window.VoucherClient;
      return Boolean(
        api &&
          api.client &&
          typeof api.client.fetchPackages === "function" &&
          typeof api.client.loadPackages === "function" &&
          typeof api.client.renderPackages === "function" &&
          typeof api.client.buildPurchaseUrl === "function" &&
          typeof api.client.hashPin === "function" &&
          typeof api.client.login === "function",
      );
    });
    expect(exposed).toBe(true);

    // The Package section is mounted and, with no backend configured, degrades
    // to a clear message instead of blocking the login form.
    await expect(page.locator("#paketInternet")).toBeVisible();
    await expect(page.locator("#paketInternet")).toContainText(/belum dikonfigurasi/i);

    // Loading the section never leaves an uncaught voucher error behind.
    expect(voucherProblems).toEqual([]);
  });

  test("renders the Package list from the backend response", async ({ page }) => {
    await configureDeployment(page, { backendBaseUrl: "https://api.test" });
    await stubPackageBackend(page, [PACKAGE_FIXTURE]);

    await page.goto("/login.html");

    const packages = page.locator("#paketInternet");
    await expect(packages).toContainText("Paket Harian");
    await expect(packages).toContainText("Rp 5.000");
    await expect(packages).toContainText("1 Hari");
    await expect(packages).toContainText("10M");

    await expect(page.locator("#btnLogin")).toBeEnabled();
  });

  test("sends the chosen Package and the Login Page return target to the purchase app on Beli", async ({ page }) => {
    await configureDeployment(page, { backendBaseUrl: "https://api.test", appBaseUrl: "https://app.test/beli" });
    await stubPackageBackend(page, [PACKAGE_FIXTURE]);
    await page.route("https://app.test/**", async (route) => {
      await route.fulfill({ status: 200, contentType: "text/html", body: "<html><body>purchase app</body></html>" });
    });

    // The Login Page keeps its hotspot context in the query string.
    await page.goto("/login.html?dst=http%3A%2F%2Fexample.com");
    const loginUrl = page.url();

    await page.locator("#paketInternet").getByRole("button", { name: "Beli", exact: true }).click();

    await page.waitForURL(/app\.test\/beli/);
    const purchaseUrl = new URL(page.url());
    expect(purchaseUrl.searchParams.get("package")).toBe("1");
    expect(purchaseUrl.searchParams.get("return")).toBe(loginUrl);
    expect(purchaseUrl.searchParams.get("return")).toContain("dst=");
  });

  test("asks for Phone Number + masked PIN and has no voucher/member switch", async ({ page }) => {
    await page.goto("/login.html");

    await expect(page.locator("#username")).toBeVisible();
    await expect(page.locator("#username")).toHaveAttribute("placeholder", /phone number/i);
    await expect(page.locator("#password")).toBeVisible();
    await expect(page.locator("#password")).toHaveAttribute("placeholder", /pin/i);
    await expect(page.locator("#password")).toHaveAttribute("type", "password");
    await expect(page.locator("#labelForm")).toContainText(/login/i);
    await expect(page.locator("#loginOpt")).toBeHidden();

    // The PIN can be revealed and re-hidden with its toggle.
    await page.locator("#iconTypePassword").click();
    await expect(page.locator("#password")).toHaveAttribute("type", "text");
    await page.locator("#iconTypePassword").click();
    await expect(page.locator("#password")).toHaveAttribute("type", "password");
  });

  test("hashes the PIN with CHAP and never sends the Phone Number as the password", async ({ page }) => {
    await page.goto("/login.html");

    const submitted = await page.evaluate(() => {
      document.getElementById("username").value = "08123456789";
      document.getElementById("password").value = "123456";
      document.sendin.submit = function () {};
      window.doLogin();
      return {
        username: document.sendin.username.value,
        password: document.sendin.password.value,
      };
    });

    const expected = createHash("md5").update("$(chap-id)" + "123456" + "$(chap-challenge)").digest("hex");
    expect(submitted.username).toBe("08123456789");
    expect(submitted.password).toBe(expected);
    expect(submitted.password).not.toBe("08123456789");
    expect(submitted.password).not.toBe("123456");
  });

  test("maps a wrong PIN and an expired account to different messages", async ({ page }) => {
    await serveLoginPage(page, { "$(error)": "invalid password" });
    await page.goto("/login.html");
    await expect(page.locator("#serverError")).toContainText(/PIN salah/i);

    await serveLoginPage(page, { "$(error)": "uptime limit reached" });
    await page.goto("/login.html");
    await expect(page.locator("#serverError")).toContainText(/berakhir/i);
  });

  test("shows a clear message when the PIN is not 6 digits", async ({ page }) => {
    await page.goto("/login.html");

    await page.fill("#username", "08123456789");
    await page.fill("#password", "12345");
    await page.locator("#btnLogin").click();

    await expect(page.locator("#loginError")).toBeVisible();
    await expect(page.locator("#loginError")).toContainText(/6 digit/i);
  });

  test("keeps the trial flow available", async ({ page }) => {
    await serveLoginPage(page, { "$(trial)": "yes" });
    await page.goto("/login.html");

    await expect(page.locator("#btnTrial")).toBeVisible();
    await expect(page.locator("#linkTrial")).toHaveAttribute("href", /username=T-/);
    await expect(page.locator("#password")).toHaveAttribute("type", "password");
  });

  test("shows no mode tabs while only Phone Number + PIN is enabled", async ({ page }) => {
    await page.goto("/login.html");

    await expect(page.locator("#loginModes")).toBeHidden();
    await expect(page.locator(".login-modes__tab")).toHaveCount(0);
    await expect(page.locator("#labelForm")).toContainText(/login/i);
    await expect(page.locator("#domPassword")).toBeVisible();
  });

  test("still reveals the PIN field when the voucher client fails to load", async ({ page }) => {
    await page.route("**/assets/voucher-client.js*", (route) => route.abort());
    await page.goto("/login.html");

    await expect(page.locator("#domPassword")).toBeVisible();
    await expect(page.locator("#password")).toBeVisible();
    await expect(page.locator("#btnLogin")).toBeEnabled();
  });

  test("offers a segmented control once more than one login mode is enabled", async ({ page }) => {
    await configureLoginModes(page, { user: 1 });
    await page.goto("/login.html");

    const tabs = page.locator("#loginModes .login-modes__tab");
    await expect(tabs).toHaveCount(2);
    await expect(tabs.nth(0)).toHaveText(/nomor hp/i);
    await expect(tabs.nth(1)).toHaveText(/^user$/i);

    // Phone Number + PIN stays the default mode.
    await expect(page.locator("#username")).toHaveAttribute("placeholder", /phone number/i);
    await expect(page.locator("#password")).toHaveAttribute("placeholder", /pin/i);

    await tabs.nth(1).click();

    await expect(page.locator("#username")).toHaveAttribute("placeholder", /username/i);
    await expect(page.locator("#password")).toHaveAttribute("placeholder", /password/i);
  });

  test("submits User mode credentials with a separately hashed password", async ({ page }) => {
    await configureLoginModes(page, { user: 1 });
    await page.goto("/login.html");
    await page.locator("#loginModes .login-modes__tab").nth(1).click();

    const submitted = await page.evaluate(() => {
      document.getElementById("username").value = "budi";
      document.getElementById("password").value = "rahasia";
      document.sendin.submit = function () {};
      window.doLogin();
      return {
        username: document.sendin.username.value,
        password: document.sendin.password.value,
      };
    });

    const expected = createHash("md5").update("$(chap-id)" + "rahasia" + "$(chap-challenge)").digest("hex");
    expect(submitted.username).toBe("budi");
    expect(submitted.password).toBe(expected);
    expect(submitted.password).not.toBe("budi");
  });

  test("uses User mode by default when it is the only enabled mode", async ({ page }) => {
    await configureLoginModes(page, { phone: 0, user: 1 });
    await page.goto("/login.html");

    // Only one mode is enabled, so there is no tab: the form starts in User mode.
    await expect(page.locator("#loginModes")).toBeHidden();
    await expect(page.locator(".login-modes__tab")).toHaveCount(0);
    await expect(page.locator("#labelForm")).toContainText(/login/i);
    await expect(page.locator("#username")).toHaveAttribute("placeholder", /username/i);
    await expect(page.locator("#domPassword")).toBeVisible();
    await expect(page.locator("#password")).toHaveAttribute("placeholder", /password/i);

    const submitted = await page.evaluate(() => {
      document.getElementById("username").value = "budi";
      document.getElementById("password").value = "rahasia";
      document.sendin.submit = function () {};
      window.doLogin();
      return {
        username: document.sendin.username.value,
        password: document.sendin.password.value,
      };
    });

    const expected = createHash("md5").update("$(chap-id)" + "rahasia" + "$(chap-challenge)").digest("hex");
    expect(submitted.username).toBe("budi");
    expect(submitted.password).toBe(expected);
  });

  test("asks for a username and a masked password in User mode", async ({ page }) => {
    await configureLoginModes(page, { user: 1 });
    await page.goto("/login.html");
    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /^user$/i }).click();

    // User mode offers both credential fields, with a plain username and a
    // masked password; the username has no reveal toggle.
    await expect(page.locator("#domUsername")).toBeVisible();
    await expect(page.locator("#username")).toBeVisible();
    await expect(page.locator("#username")).toHaveAttribute("placeholder", /username/i);
    await expect(page.locator("#username")).toHaveAttribute("type", "text");
    await expect(page.locator("#iconTypePasswordUser")).toBeHidden();
    await expect(page.locator("#domPassword")).toBeVisible();
    await expect(page.locator("#password")).toBeVisible();
    await expect(page.locator("#password")).toHaveAttribute("placeholder", /password/i);
    await expect(page.locator("#password")).toHaveAttribute("type", "password");

    // The password can still be revealed and hidden again.
    await page.locator("#iconTypePassword").click();
    await expect(page.locator("#password")).toHaveAttribute("type", "text");
    await page.locator("#iconTypePassword").click();
    await expect(page.locator("#password")).toHaveAttribute("type", "password");
  });

  test("follows the mode for a wrong User credential while keeping the expired message", async ({ page }) => {
    await configureLoginModes(page, { user: 1 });
    await serveLoginPage(page, { "$(error)": "invalid password" });
    await page.goto("/login.html");

    await expect(page.locator("#serverError")).toContainText(/PIN salah/i);
    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /^user$/i }).click();
    await expect(page.locator("#serverError")).toContainText(/username atau password salah/i);

    // An expired account keeps the shared message in User mode too.
    await serveLoginPage(page, { "$(error)": "uptime limit reached" });
    await page.goto("/login.html");
    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /^user$/i }).click();
    await expect(page.locator("#serverError")).toContainText(/berakhir/i);
  });

  test("accepts a non-6-digit User password without the PIN rule", async ({ page }) => {
    await configureLoginModes(page, { user: 1 });
    await page.goto("/login.html");
    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /^user$/i }).click();

    const submitted = await page.evaluate(() => {
      document.getElementById("username").value = "budi";
      document.getElementById("password").value = "abc";
      document.sendin.submit = function () {};
      window.doLogin();
      return {
        username: document.sendin.username.value,
        password: document.sendin.password.value,
      };
    });

    await expect(page.locator("#loginError")).toBeHidden();
    const expected = createHash("md5").update("$(chap-id)" + "abc" + "$(chap-challenge)").digest("hex");
    expect(submitted.username).toBe("budi");
    expect(submitted.password).toBe(expected);
  });

  test("keeps the Package list, Beli button and trial flow with User enabled", async ({ page }) => {
    await configureLoginModes(page, { user: 1 });
    await configureDeployment(page, { backendBaseUrl: "https://api.test", appBaseUrl: "https://app.test/beli" });
    await stubPackageBackend(page, [PACKAGE_FIXTURE]);
    await serveLoginPage(page, { "$(trial)": "yes" });

    await page.goto("/login.html");
    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /^user$/i }).click();

    // User keeps both credential fields visible.
    await expect(page.locator("#username")).toBeVisible();
    await expect(page.locator("#password")).toBeVisible();

    // The Package list, its Beli button and the trial flow stay intact.
    const packages = page.locator("#paketInternet");
    await expect(packages).toContainText("Paket Harian");
    await expect(packages.getByRole("button", { name: "Beli", exact: true })).toBeVisible();
    await expect(page.locator("#btnTrial")).toBeVisible();
    await expect(page.locator("#linkTrial")).toHaveAttribute("href", /username=T-/);
  });

  test("offers a single Access Code field and hashes the code as the password", async ({ page }) => {
    await configureLoginModes(page, { accessCode: 1 });
    await page.goto("/login.html");

    const tabs = page.locator("#loginModes .login-modes__tab");
    await expect(tabs).toHaveCount(2);
    await tabs.filter({ hasText: /kode akses/i }).click();

    await expect(page.locator("#domPassword")).toBeHidden();
    await expect(page.locator("#username")).toHaveAttribute("placeholder", /kode akses/i);

    const submitted = await page.evaluate(() => {
      document.getElementById("username").value = "ABC";
      document.sendin.submit = function () {};
      window.doLogin();
      return {
        username: document.sendin.username.value,
        password: document.sendin.password.value,
      };
    });

    const expected = createHash("md5").update("$(chap-id)" + "ABC" + "$(chap-challenge)").digest("hex");
    expect(submitted.username).toBe("ABC");
    expect(submitted.password).toBe(expected);
  });

  test("follows the mode for a wrong-credential message while keeping the expired message", async ({ page }) => {
    await configureLoginModes(page, { accessCode: 1 });
    await serveLoginPage(page, { "$(error)": "invalid password" });
    await page.goto("/login.html");

    // The default mode is Phone Number + PIN.
    await expect(page.locator("#serverError")).toContainText(/PIN salah/i);

    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /kode akses/i }).click();
    await expect(page.locator("#serverError")).toContainText(/kode akses salah/i);

    // An expired account keeps the shared message in every mode.
    await serveLoginPage(page, { "$(error)": "uptime limit reached" });
    await page.goto("/login.html");
    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /kode akses/i }).click();
    await expect(page.locator("#serverError")).toContainText(/berakhir/i);
  });

  test("accepts a short numeric Access Code without the 6-digit PIN rule", async ({ page }) => {
    await configureLoginModes(page, { accessCode: 1 });
    await page.goto("/login.html");
    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /kode akses/i }).click();

    const submitted = await page.evaluate(() => {
      document.getElementById("username").value = "12";
      document.sendin.submit = function () {};
      window.doLogin();
      return {
        username: document.sendin.username.value,
        password: document.sendin.password.value,
      };
    });

    await expect(page.locator("#loginError")).toBeHidden();
    const expected = createHash("md5").update("$(chap-id)" + "12" + "$(chap-challenge)").digest("hex");
    expect(submitted.username).toBe("12");
    expect(submitted.password).toBe(expected);
  });

  test("keeps the Package list, Beli button and trial flow with Access Code enabled", async ({ page }) => {
    await configureLoginModes(page, { accessCode: 1 });
    await configureDeployment(page, { backendBaseUrl: "https://api.test", appBaseUrl: "https://app.test/beli" });
    await stubPackageBackend(page, [PACKAGE_FIXTURE]);
    await serveLoginPage(page, { "$(trial)": "yes" });

    await page.goto("/login.html");
    await page.locator("#loginModes .login-modes__tab").filter({ hasText: /kode akses/i }).click();

    // Access Code keeps exactly one visible credential field.
    await expect(page.locator("#username")).toBeVisible();
    await expect(page.locator("#domPassword")).toBeHidden();

    // The Package list, its Beli button and the trial flow stay intact.
    const packages = page.locator("#paketInternet");
    await expect(packages).toContainText("Paket Harian");
    await expect(packages.getByRole("button", { name: "Beli", exact: true })).toBeVisible();
    await expect(page.locator("#btnTrial")).toBeVisible();
    await expect(page.locator("#linkTrial")).toHaveAttribute("href", /username=T-/);
  });
});
