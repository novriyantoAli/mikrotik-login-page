// @ts-check
/**
 * Voucher client — the single client-side seam for the voucher purchase flow.
 *
 * This is the only module the Login Page talks to for anything voucher related.
 * It is deliberately transport/DOM agnostic: `fetch`, `md5`, `document` and the
 * navigation/submit hooks are injectable so every behavior can be exercised at
 * this boundary without reaching into RouterOS internals or the page markup.
 *
 * Loaded as a classic script on the Login Page (sets `window.VoucherClient`),
 * and as a CommonJS module by the test harness.
 */
(function (root, factory) {
  var api = factory();
  var globalRoot = /** @type {any} */ (root);

  if (typeof module === "object" && module.exports) {
    module.exports = api;
  } else {
    globalRoot.VoucherClient = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  /**
   * @typedef {Object} VoucherConfig
   * @property {string} backendBaseUrl Base URL of the backend serving the Package list.
   * @property {string} packagesPath Path of the public Package list endpoint.
   * @property {string} appBaseUrl Base URL of the purchase web app (Svelte).
   * @property {number} packagesFromBackend 1 to read from the backend, 0 for the static list.
   * @property {Array<Record<string, unknown>>} fallbackPackages Static list used when the toggle is off.
   */

  /**
   * @typedef {Object} PackageOption
   * @property {string|number|null} id Package identity used by the purchase app.
   * @property {string} name Display name.
   * @property {string} price Display price.
   * @property {string} activePeriod Display Active Period.
   * @property {string} speedLimit Display speed limit.
   */

  /** @type {VoucherConfig} */
  var DEFAULT_CONFIG = {
    backendBaseUrl: "",
    packagesPath: "/packages",
    appBaseUrl: "",
    packagesFromBackend: 0,
    fallbackPackages: [],
  };

  /**
   * @typedef {Object} LoginPayload
   * @property {string} username
   * @property {string} password
   */

  /**
   * @typedef {Object} LoginCredentials
   * @property {string} phoneNumber
   * @property {string} pin
   * @property {string} [chapId]
   * @property {string} [chapChallenge]
   * @property {(payload: LoginPayload) => void} [submit]
   */

  /**
   * @typedef {Object} VoucherClientOptions
   * @property {Partial<VoucherConfig>} [config]
   * @property {typeof fetch} [fetch]
   * @property {(input: string) => string} [md5]
   * @property {Document} [document]
   * @property {(url: string) => void} [navigate]
   * @property {((payload: LoginPayload) => void) | null} [submitLogin]
   */

  var MESSAGES = {
    loading: "Memuat paket...",
    empty: "Belum ada paket tersedia.",
    error: "Gagal memuat paket. Silakan coba lagi.",
    expired: "Masa aktif Anda telah berakhir. Silakan beli paket baru.",
    wrongPin: "PIN salah. Periksa kembali PIN Anda.",
    generic: "Login gagal. Periksa Phone Number dan PIN Anda.",
  };

  /**
   * Error carrying a machine-readable `code` so callers can branch on failures.
   * @param {string} code
   * @param {string} message
   * @param {unknown} [cause]
   */
  function VoucherError(code, message, cause) {
    Error.call(this, message);
    this.name = "VoucherError";
    this.message = message;
    this.code = code;
    if (cause !== undefined) {
      this.cause = cause;
    }
    if (typeof Error.captureStackTrace === "function") {
      Error.captureStackTrace(this, VoucherError);
    } else {
      this.stack = new Error(message).stack;
    }
  }
  VoucherError.prototype = Object.create(Error.prototype);
  VoucherError.prototype.constructor = VoucherError;

  /** @returns {Record<string, unknown>} */
  function readGlobalConfig() {
    if (typeof globalThis !== "undefined" && globalThis.cfgVoucher) {
      return globalThis.cfgVoucher;
    }
    return {};
  }

  /**
   * Merge defaults, the deployment config and explicit overrides into one
   * normalized, immutable config object.
   * @param {Partial<VoucherConfig>} [overrides]
   * @returns {VoucherConfig}
   */
  function resolveConfig(overrides) {
    /** @type {Record<string, any>} */
    var merged = {};
    var sources = [DEFAULT_CONFIG, readGlobalConfig(), overrides || {}];

    for (var i = 0; i < sources.length; i++) {
      var source = sources[i];
      for (var key in source) {
        if (Object.prototype.hasOwnProperty.call(source, key) && source[key] !== undefined) {
          merged[key] = source[key];
        }
      }
    }

    merged.packagesFromBackend = merged.packagesFromBackend ? 1 : 0;
    merged.packagesPath = normalizePath(merged.packagesPath);
    merged.fallbackPackages = Array.isArray(merged.fallbackPackages) ? merged.fallbackPackages : [];

    return Object.freeze(/** @type {VoucherConfig} */ (merged));
  }

  /** @param {unknown} path */
  function normalizePath(path) {
    var value = path == null ? DEFAULT_CONFIG.packagesPath : String(path);
    if (value && value.charAt(0) !== "/") {
      value = "/" + value;
    }
    return value || DEFAULT_CONFIG.packagesPath;
  }

  /**
   * @param {string} base
   * @param {string} path
   */
  function joinUrl(base, path) {
    var trimmedBase = String(base || "").replace(/\/+$/, "");
    var trimmedPath = String(path || "").replace(/^\/+/, "");
    return trimmedPath ? trimmedBase + "/" + trimmedPath : trimmedBase;
  }

  /** @param {unknown} value */
  function isBlank(value) {
    return value === undefined || value === null || value === "";
  }

  /**
   * @param {unknown} a
   * @param {unknown} b
   * @param {unknown} fallback
   */
  function firstDefined(a, b, fallback) {
    if (!isBlank(a)) {
      return a;
    }
    if (!isBlank(b)) {
      return b;
    }
    return fallback;
  }

  /**
   * @param {any} raw
   * @returns {PackageOption}
   */
  function normalizePackage(raw) {
    var value = raw || {};
    return {
      id: value.id != null ? value.id : value.packageId != null ? value.packageId : null,
      name: String(firstDefined(value.name, value.nama, "")),
      price: String(firstDefined(value.price, value.harga, "")),
      activePeriod: String(firstDefined(value.activePeriod, value.durasiActivePeriod, "")),
      speedLimit: String(firstDefined(value.speedLimit, value.batasKecepatan, "")),
    };
  }

  /**
   * Accept either a bare array or a wrapped response and normalize each entry.
   * @param {any} payload
   * @returns {PackageOption[]}
   */
  function normalizePackageList(payload) {
    var list = payload;
    if (payload && !Array.isArray(payload)) {
      if (Array.isArray(payload.data)) {
        list = payload.data;
      } else if (Array.isArray(payload.packages)) {
        list = payload.packages;
      } else {
        list = [];
      }
    }
    if (!Array.isArray(list)) {
      list = [];
    }
    return list.map(normalizePackage);
  }

  /** @returns {typeof fetch | null} */
  function globalFetch() {
    if (typeof globalThis !== "undefined" && typeof globalThis.fetch === "function") {
      return globalThis.fetch.bind(globalThis);
    }
    return null;
  }

  /** @returns {((input: string) => string) | null} */
  function globalMd5() {
    if (typeof globalThis !== "undefined" && typeof globalThis.hexMD5 === "function") {
      return globalThis.hexMD5;
    }
    return null;
  }

  /** @param {string} url */
  function defaultNavigate(url) {
    if (typeof globalThis !== "undefined" && globalThis.location) {
      globalThis.location.href = url;
    }
  }

  /** @returns {string} */
  function currentUrl() {
    if (typeof globalThis !== "undefined" && globalThis.location) {
      return String(globalThis.location.href);
    }
    return "";
  }

  /**
   * @param {VoucherConfig} config
   * @param {PackageOption} pkg
   * @param {{ returnUrl?: string }} [context]
   */
  function buildPurchaseUrl(config, pkg, context) {
    if (!config.appBaseUrl) {
      throw new VoucherError("config_missing", "Purchase app base URL is not configured");
    }

    var id = pkg && typeof pkg === "object" ? pkg.id : pkg;
    if (isBlank(id)) {
      throw new VoucherError("missing_package_id", "A Package identity is required to build a purchase URL");
    }

    var requestedReturn = context && context.returnUrl !== undefined ? context.returnUrl : currentUrl();
    var params = ["package=" + encodeURIComponent(String(id))];
    if (requestedReturn) {
      params.push("return=" + encodeURIComponent(String(requestedReturn)));
    }

    var base = String(config.appBaseUrl);
    var separator = base.indexOf("?") === -1 ? "?" : "&";
    return base + separator + params.join("&");
  }

  /**
   * Build the interface consumed by the Login Page and by tests.
   * @param {VoucherClientOptions} [options]
   */
  function createVoucherClient(options) {
    var opts = options || {};
    var config = resolveConfig(opts.config);
    var fetchImpl = opts.fetch || null;
    var md5Impl = opts.md5 || null;
    var doc = opts.document || (typeof document !== "undefined" ? document : null);
    var navigate = opts.navigate || defaultNavigate;
    var submitLogin = opts.submitLogin || null;

    /**
     * Fetch the Package list from the backend, or the static fallback when the
     * backend list is disabled.
     * @returns {Promise<PackageOption[]>}
     */
    function fetchPackages() {
      if (!config.packagesFromBackend) {
        return Promise.resolve(normalizePackageList(config.fallbackPackages));
      }

      if (!config.backendBaseUrl) {
        return Promise.reject(new VoucherError("config_missing", "Backend base URL is not configured"));
      }

      var doFetch = fetchImpl || globalFetch();
      if (typeof doFetch !== "function") {
        return Promise.reject(new VoucherError("fetch_unavailable", "No fetch implementation is available"));
      }

      var url = joinUrl(config.backendBaseUrl, config.packagesPath);

      return Promise.resolve()
        .then(function () {
          return doFetch(url, {
            method: "GET",
            headers: { Accept: "application/json" },
          });
        })
        .then(function (response) {
          if (response && response.ok === false) {
            throw new VoucherError("http_error", "Package list request failed with status " + response.status, response);
          }
          return response.json();
        })
        .then(normalizePackageList)
        .catch(function (error) {
          if (error instanceof VoucherError) {
            throw error;
          }
          throw new VoucherError("network_error", "Package list request failed", error);
        });
    }

    /**
     * @param {Element|string} container
     * @param {Array<Record<string, unknown>>} packages
     * @returns {Element}
     */
    function renderPackages(container, packages) {
      var target = resolveContainer(container);
      var items = normalizePackageList(packages);
      target.innerHTML = "";

      var list = createElement("div", "package-list");
      if (items.length === 0) {
        list.appendChild(createTextElement("p", "package-list__empty", MESSAGES.empty));
      } else {
        for (var i = 0; i < items.length; i++) {
          list.appendChild(buildPackageCard(items[i]));
        }
      }
      target.appendChild(list);
      return list;
    }

    /**
     * Render a transient state (loading / empty / error) into the container.
     * @param {Element|string} container
     * @param {"loading"|"empty"|"error"} state
     * @param {string} [message]
     * @returns {Element}
     */
    function renderStatus(container, state, message) {
      var target = resolveContainer(container);
      var text = message || MESSAGES[state] || "";
      target.innerHTML = "";
      target.appendChild(createTextElement("p", "package-status package-status--" + state, text));
      return target;
    }

    /**
     * @param {PackageOption} pkg
     * @returns {Element}
     */
    function buildPackageCard(pkg) {
      var card = createElement("div", "package-card");
      card.appendChild(createTextElement("div", "package-card__name", pkg.name));
      card.appendChild(createTextElement("div", "package-card__price", pkg.price));
      card.appendChild(createTextElement("div", "package-card__period", pkg.activePeriod));
      card.appendChild(createTextElement("div", "package-card__speed", pkg.speedLimit));

      var button = createTextElement("button", "package-card__buy", "Beli");
      button.setAttribute("type", "button");
      button.addEventListener("click", function () {
        navigate(buildPurchaseUrl(config, pkg));
      });
      card.appendChild(button);
      return card;
    }

    /** @param {Element|string} container */
    function resolveContainer(container) {
      var target = null;
      if (typeof container === "string") {
        target = doc ? doc.querySelector(container) : null;
      } else {
        target = container;
      }
      if (!target) {
        throw new VoucherError("missing_container", "A container element is required to render Packages");
      }
      return target;
    }

    /**
     * @param {keyof HTMLElementTagNameMap} tag
     * @param {string} className
     */
    function createElement(tag, className) {
      var element = doc.createElement(tag);
      element.className = className;
      return element;
    }

    /**
     * @param {keyof HTMLElementTagNameMap} tag
     * @param {string} className
     * @param {string} text
     */
    function createTextElement(tag, className, text) {
      var element = createElement(tag, className);
      element.textContent = text;
      return element;
    }

    /** @param {PackageOption} pkg @param {{ returnUrl?: string }} [context] */
    function purchaseUrlFor(pkg, context) {
      return buildPurchaseUrl(config, pkg, context);
    }

    /**
     * Hash a PIN for CHAP submission. Never returns the raw PIN.
     * @param {string} pin
     * @param {string} [chapId]
     * @param {string} [chapChallenge]
     * @returns {string}
     */
    function hashPin(pin, chapId, chapChallenge) {
      var md5 = md5Impl || globalMd5();
      if (typeof md5 !== "function") {
        throw new VoucherError("md5_unavailable", "No MD5 implementation is available for CHAP hashing");
      }
      return md5(String(chapId || "") + String(pin == null ? "" : pin) + String(chapChallenge || ""));
    }

    /**
     * Build the hotspot login payload for Phone Number + PIN, hashing the PIN
     * with CHAP, and hand it to the submit hook when one is available.
     * @param {LoginCredentials} credentials
     * @returns {LoginPayload}
     */
    function login(credentials) {
      var input = credentials || /** @type {LoginCredentials} */ ({});
      if (input.phoneNumber === undefined || input.phoneNumber === null || input.phoneNumber === "") {
        throw new VoucherError("missing_phone_number", "Phone Number is required");
      }
      if (input.pin === undefined || input.pin === null || input.pin === "") {
        throw new VoucherError("missing_pin", "PIN is required");
      }
      if (!/^\d{6}$/.test(String(input.pin))) {
        throw new VoucherError("invalid_pin", "PIN must be 6 digits");
      }

      var payload = {
        username: String(input.phoneNumber),
        password: hashPin(input.pin, input.chapId, input.chapChallenge),
      };

      var submit = input.submit || submitLogin;
      if (typeof submit === "function") {
        submit(payload);
      }
      return payload;
    }

    /**
     * Translate a raw hotspot error / RouterOS expiry value into a human message,
     * distinguishing an expired account from a wrong PIN.
     * @param {string} [error]
     * @param {string} [expiry]
     * @returns {string}
     */
    function describeError(error, expiry) {
      var errorText = String(error == null ? "" : error).toLowerCase();
      var expiryText = String(expiry == null ? "" : expiry).toLowerCase();

      var expiryPattern = /expire|expired|kedaluwarsa|habis|no\s*quota|out\s*of\s*(time|quota)|uptime\s*limit|limit\s*reached|quota\s*(reached|exceeded)/;
      var isExpired =
        expiryPattern.test(errorText) ||
        expiryPattern.test(expiryText) ||
        expiryText === "0";

      if (isExpired) {
        return MESSAGES.expired;
      }
      if (/invalid\s*password|wrong\s*password|bad\s*password|\bpassword\b|\bpin\b|salah/.test(errorText)) {
        return MESSAGES.wrongPin;
      }
      return MESSAGES.generic;
    }

    return {
      config: config,
      fetchPackages: fetchPackages,
      renderPackages: renderPackages,
      renderStatus: renderStatus,
      buildPurchaseUrl: purchaseUrlFor,
      hashPin: hashPin,
      login: login,
      describeError: describeError,
    };
  }

  var api = {
    createVoucherClient: createVoucherClient,
    resolveConfig: resolveConfig,
    VoucherError: VoucherError,
    DEFAULT_CONFIG: DEFAULT_CONFIG,
    messages: MESSAGES,
    client: createVoucherClient(),
  };

  return api;
});
