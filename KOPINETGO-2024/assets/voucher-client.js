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
   * @property {string} activePeriodPath Path of the public Active Period endpoint.
   * @property {number} expiryWarningHours Hours before the Active Period ends that count as "almost over".
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
    activePeriodPath: "/active-period",
    expiryWarningHours: 24,
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
   * @property {string} [mode] Login mode id; defaults to the deployment default (Phone Number + PIN).
   * @property {string} [phoneNumber]
   * @property {string} [pin]
   * @property {string} [accessCode]
   * @property {string} [username]
   * @property {string} [password]
   * @property {string} [chapId]
   * @property {string} [chapChallenge]
   * @property {(payload: LoginPayload) => void} [submit]
   */

  /**
   * @typedef {Object} VoucherClientOptions
   * @property {Partial<VoucherConfig>} [config]
   * @property {LoginConfig} [login]
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
    notConfigured: "Daftar paket belum dikonfigurasi. Hubungi admin.",
    packageTitle: "Paket Internet",
    packageSubtitle: "Pilih paket sesuai kebutuhan Anda.",
    statusUnavailable: "Active Period tidak tersedia.",
    statusLoading: "Memuat Active Period...",
    purchaseAppUnavailable: "Pembelian belum dikonfigurasi. Hubungi admin.",
    packageUnavailable: "Paket ini belum dapat dibeli. Hubungi admin.",
    expired: "Active Period Anda telah berakhir. Silakan beli paket baru.",
    wrongPin: "PIN salah. Periksa kembali PIN Anda.",
    generic: "Login gagal. Periksa Phone Number dan PIN Anda.",
    phoneRequired: "Phone Number wajib diisi.",
    pinRequired: "PIN wajib diisi.",
    invalidPin: "PIN harus 6 digit angka.",
    accessCodeRequired: "Kode Akses wajib diisi.",
    usernameRequired: "Username wajib diisi.",
    passwordRequired: "Password wajib diisi.",
    wrongAccessCode: "Kode Akses salah.",
    wrongUserPassword: "Username atau password salah.",
    genericAccessCode: "Login gagal. Periksa Kode Akses Anda.",
    genericUser: "Login gagal. Periksa username dan password Anda.",
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
   * The Package section's own labels, from the deployment's `cfgIP` (the section
   * that used to render the manual list). Reusing them keeps the heading and
   * subtitle a configuration concern, exactly like the FAQ section, and gives the
   * backend list the title the page already advertises.
   * @returns {{title: string, subtitle: string}}
   */
  function readPackageLabels() {
    var config = typeof globalThis !== "undefined" ? globalThis.cfgIP : undefined;
    var label = (config && config.label) || {};
    return {
      title: label.title || MESSAGES.packageTitle,
      subtitle: label.sub_title || label.subtitle || MESSAGES.packageSubtitle,
    };
  }

  /** @returns {Record<string, unknown>} */
  function readGlobalLoginConfig() {
    if (typeof globalThis !== "undefined" && globalThis.cfgLogin) {
      return globalThis.cfgLogin;
    }
    return {};
  }

  /**
   * @typedef {Object} LoginModeField
   * @property {string} key Credential key the field supplies.
   * @property {"primary"|"secondary"} slot Form slot the field occupies.
   * @property {string} placeholder Field placeholder.
   * @property {"text"|"password"} type Input type.
   * @property {boolean} reveal Whether the show/hide eye toggle is offered.
   */

  /**
   * @typedef {Object} LoginMode
   * @property {string} id
   * @property {string} label Segmented-control label.
   * @property {string} title Form title.
   * @property {LoginModeField[]} fields
   */

  /**
   * @typedef {Object} LoginConfig
   * @property {string} [defaultMode] Enabled mode id the form starts in.
   * @property {Record<string, {status?: unknown, label?: string}>} [modes]
   */

  /**
   * Descriptors for the three hotspot login modes (ADR 0006). The form title,
   * field shape and placeholders come from here rather than from the page
   * markup, so enabling a mode is a configuration change only.
   *
   * @type {Record<string, LoginMode & {
   *   defaultStatus: boolean,
   *   credentialError: string,
   *   genericError: string,
   *   messages: Record<string, string>,
   *   errors: Record<string, string>,
   *   validate: (input: Record<string, any>) => string|null,
   *   credentials: (input: Record<string, any>) => {username: unknown, password: unknown}
   * }>}
   */
  var LOGIN_MODES = {
    phone: {
      id: "phone",
      label: "Nomor HP",
      title: "Login",
      defaultStatus: true,
      fields: [
        { key: "phoneNumber", slot: "primary", placeholder: "Phone Number", type: "text", reveal: false },
        { key: "pin", slot: "secondary", placeholder: "PIN", type: "password", reveal: true },
      ],
      credentialError: MESSAGES.wrongPin,
      genericError: MESSAGES.generic,
      messages: {
        missing_phone_number: MESSAGES.phoneRequired,
        missing_pin: MESSAGES.pinRequired,
        invalid_pin: MESSAGES.invalidPin,
      },
      errors: {
        missing_phone_number: "Phone Number is required",
        missing_pin: "PIN is required",
        invalid_pin: "PIN must be 6 digits",
      },
      validate: function (input) {
        if (isBlank(input.phoneNumber)) {
          return "missing_phone_number";
        }
        if (isBlank(input.pin)) {
          return "missing_pin";
        }
        if (!/^\d{6}$/.test(String(input.pin))) {
          return "invalid_pin";
        }
        return null;
      },
      credentials: function (input) {
        return { username: input.phoneNumber, password: input.pin };
      },
    },
    accessCode: {
      id: "accessCode",
      label: "Kode Akses",
      title: "Login",
      defaultStatus: false,
      fields: [
        { key: "accessCode", slot: "primary", placeholder: "Kode Akses", type: "text", reveal: false },
      ],
      credentialError: MESSAGES.wrongAccessCode,
      genericError: MESSAGES.genericAccessCode,
      messages: { missing_access_code: MESSAGES.accessCodeRequired },
      errors: { missing_access_code: "Access Code is required" },
      validate: function (input) {
        if (isBlank(input.accessCode)) {
          return "missing_access_code";
        }
        return null;
      },
      credentials: function (input) {
        // An Access Code is a single secret: username = password. CHAP hashing
        // still applies before submission.
        return { username: input.accessCode, password: input.accessCode };
      },
    },
    user: {
      id: "user",
      label: "User",
      title: "Login",
      defaultStatus: false,
      fields: [
        { key: "username", slot: "primary", placeholder: "Username", type: "text", reveal: false },
        { key: "password", slot: "secondary", placeholder: "Password", type: "password", reveal: true },
      ],
      credentialError: MESSAGES.wrongUserPassword,
      genericError: MESSAGES.genericUser,
      messages: {
        missing_username: MESSAGES.usernameRequired,
        missing_password: MESSAGES.passwordRequired,
      },
      errors: {
        missing_username: "Username is required",
        missing_password: "Password is required",
      },
      validate: function (input) {
        if (isBlank(input.username)) {
          return "missing_username";
        }
        if (isBlank(input.password)) {
          return "missing_password";
        }
        return null;
      },
      credentials: function (input) {
        return { username: input.username, password: input.password };
      },
    },
  };

  /** Canonical order of the mode toggles; the first enabled one is the default. */
  var LOGIN_MODE_ORDER = ["phone", "accessCode", "user"];
  var DEFAULT_LOGIN_MODE = "phone";

  /**
   * Default DOM slots the Login Page form exposes to the mode control.
   * `primary`/`secondary` are the two credential field slots.
   */
  var DEFAULT_LOGIN_SLOTS = {
    title: "labelForm",
    primary: { group: "domUsername", input: "username", reveal: "iconTypePasswordUser" },
    secondary: { group: "domPassword", input: "password", reveal: "iconTypePassword" },
  };

  /**
   * Merge the deployment login config (cfgLogin) with explicit overrides.
   * @param {LoginConfig} [overrides]
   * @returns {LoginConfig}
   */
  function resolveLoginConfig(overrides) {
    /** @type {LoginConfig} */
    var merged = { modes: {} };
    var sources = [readGlobalLoginConfig(), overrides || {}];

    for (var i = 0; i < sources.length; i++) {
      var source = /** @type {any} */ (sources[i]);
      // The default mode is a deployment choice like the toggles, so it is merged
      // the same way; an empty value keeps whatever an earlier source set.
      if (source && source.defaultMode !== undefined && source.defaultMode !== "") {
        merged.defaultMode = String(source.defaultMode);
      }

      var modes = source && source.modes;
      if (!modes) {
        continue;
      }
      for (var key in modes) {
        if (!Object.prototype.hasOwnProperty.call(modes, key)) {
          continue;
        }
        var setting = modes[key] || {};
        var existing = /** @type {Record<string, any>} */ (merged.modes)[key] || {};
        var next = {};
        if (existing.status !== undefined) {
          next.status = existing.status;
        }
        if (existing.label !== undefined) {
          next.label = existing.label;
        }
        if (setting.status !== undefined) {
          next.status = setting.status;
        }
        if (setting.label !== undefined) {
          next.label = setting.label;
        }
        /** @type {Record<string, any>} */ (merged.modes)[key] = next;
      }
    }

    return merged;
  }

  /**
   * Resolve the login modes enabled by configuration, in canonical order.
   * Always returns at least the default mode so the Login Page stays usable.
   * @param {LoginConfig} [config]
   * @returns {LoginMode[]}
   */
  function resolveLoginModes(config) {
    var settings = (config && config.modes) || {};
    /** @type {LoginMode[]} */
    var modes = [];

    for (var i = 0; i < LOGIN_MODE_ORDER.length; i++) {
      var id = LOGIN_MODE_ORDER[i];
      var descriptor = LOGIN_MODES[id];
      var setting = /** @type {Record<string, any>} */ (settings)[id] || {};
      var status = setting.status === undefined ? descriptor.defaultStatus : Boolean(setting.status);
      if (!status) {
        continue;
      }
      modes.push({
        id: id,
        label: setting.label !== undefined && setting.label !== "" ? String(setting.label) : descriptor.label,
        title: descriptor.title,
        fields: descriptor.fields,
      });
    }

    if (modes.length === 0) {
      var fallback = LOGIN_MODES[DEFAULT_LOGIN_MODE];
      modes.push({ id: fallback.id, label: fallback.label, title: fallback.title, fields: fallback.fields });
    }

    return modes;
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
    merged.packagesPath = normalizePath(merged.packagesPath, DEFAULT_CONFIG.packagesPath);
    merged.activePeriodPath = normalizePath(merged.activePeriodPath, DEFAULT_CONFIG.activePeriodPath);
    merged.expiryWarningHours = Number(merged.expiryWarningHours) > 0 ? Number(merged.expiryWarningHours) : DEFAULT_CONFIG.expiryWarningHours;
    merged.fallbackPackages = Array.isArray(merged.fallbackPackages) ? merged.fallbackPackages : [];

    return Object.freeze(/** @type {VoucherConfig} */ (merged));
  }

  /** @param {unknown} path @param {string} fallback */
  function normalizePath(path, fallback) {
    var value = path == null ? fallback : String(path);
    if (value && value.charAt(0) !== "/") {
      value = "/" + value;
    }
    return value || fallback;
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

  /** @param {unknown} value @returns {number|null} */
  function toMillis(value) {
    if (value === undefined || value === null || value === "") {
      return null;
    }
    if (value instanceof Date) {
      return isNaN(value.getTime()) ? null : value.getTime();
    }
    if (typeof value === "number") {
      if (!isFinite(value)) {
        return null;
      }
      // FreeRADIUS Expiration is conventionally epoch seconds; accept ms too.
      return value < 1e12 ? value * 1000 : value;
    }
    var parsed = Date.parse(String(value));
    return isNaN(parsed) ? null : parsed;
  }

  /** @param {number} remainingMs @returns {string} */
  function formatActivePeriod(remainingMs) {
    var ms = Number(remainingMs);
    if (!isFinite(ms) || ms <= 0) {
      return "";
    }
    var totalMinutes = Math.floor(ms / 60000);
    var days = Math.floor(totalMinutes / 1440);
    var hours = Math.floor((totalMinutes % 1440) / 60);
    var minutes = totalMinutes % 60;
    var parts = [];
    if (days) {
      parts.push(days + " hari");
    }
    if (hours) {
      parts.push(hours + " jam");
    }
    if (minutes && (days === 0 || hours === 0)) {
      parts.push(minutes + " menit");
    }
    if (parts.length === 0) {
      parts.push("kurang dari 1 menit");
    }
    return parts.join(" ");
  }

  /** @param {any} payload @returns {{expiry: unknown}} */
  function normalizeActivePeriod(payload) {
    var value = payload && !Array.isArray(payload) && payload.data ? payload.data : payload;
    if (!value || typeof value !== "object") {
      value = {};
    }
    return { expiry: firstDefined(value.expiry, value.expiredAt, null) };
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
    // Keep any fragment (e.g. an SPA hash route) after the query parameters.
    var fragment = "";
    var hashIndex = base.indexOf("#");
    if (hashIndex !== -1) {
      fragment = base.slice(hashIndex);
      base = base.slice(0, hashIndex);
    }
    var separator = base.indexOf("?") === -1 ? "?" : "&";
    return base + separator + params.join("&") + fragment;
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
    var loginConfig = resolveLoginConfig(opts.login);

    /** @returns {LoginMode[]} */
    function loginModes() {
      return resolveLoginModes(loginConfig);
    }

    /**
     * The mode the form starts in: `cfgLogin.defaultMode` when it names an enabled
     * mode, otherwise the first enabled one in canonical order (ADR 0006). A
     * default that is disabled or unknown is ignored instead of breaking the page.
     * @returns {string}
     */
    function defaultLoginMode() {
      var modes = loginModes();
      var requested = loginConfig && loginConfig.defaultMode ? String(loginConfig.defaultMode) : "";

      for (var i = 0; i < modes.length; i++) {
        if (modes[i].id === requested) {
          return modes[i].id;
        }
      }
      return modes.length ? modes[0].id : DEFAULT_LOGIN_MODE;
    }

    /**
     * Look up a mode descriptor, rejecting a mode the seam does not know.
     * @param {string} [mode]
     * @returns {typeof LOGIN_MODES[string]}
     */
    function loginModeDescriptor(mode) {
      var id = isBlank(mode) ? defaultLoginMode() : String(mode);
      var descriptor = LOGIN_MODES[id];
      if (!descriptor) {
        throw new VoucherError("unknown_mode", "Unknown login mode: " + id);
      }
      return descriptor;
    }

    /**
     * @param {string|Element|null|undefined} ref
     * @returns {Element|null}
     */
    function resolveElement(ref) {
      if (!ref) {
        return null;
      }
      if (typeof ref === "string") {
        if (!doc) {
          return null;
        }
        var first = ref.charAt(0);
        if (first === "#" || first === "." || first === "[") {
          return doc.querySelector(ref);
        }
        return doc.getElementById(ref);
      }
      return ref;
    }

    /**
     * @param {{group?: string|Element, input?: string|Element, reveal?: string|Element}} base
     * @param {{group?: string|Element, input?: string|Element, reveal?: string|Element}} [override]
     */
    function mergeSlot(base, override) {
      var out = { group: base.group, input: base.input, reveal: base.reveal };
      var source = override || {};
      for (var key in source) {
        if (Object.prototype.hasOwnProperty.call(source, key) && source[key] !== undefined) {
          out[key] = source[key];
        }
      }
      return out;
    }

    /** @param {Record<string, any>} [overrides] */
    function resolveFieldSlots(overrides) {
      var slots = overrides || {};
      return {
        title: slots.title !== undefined ? slots.title : DEFAULT_LOGIN_SLOTS.title,
        primary: mergeSlot(DEFAULT_LOGIN_SLOTS.primary, slots.primary),
        secondary: mergeSlot(DEFAULT_LOGIN_SLOTS.secondary, slots.secondary),
      };
    }

    /**
     * Fill one form slot from a mode field, hiding the slot when the mode does
     * not use it.
     * @param {LoginModeField|undefined} field
     * @param {Record<string, any>} slot
     * @param {boolean} reset
     */
    function applyFieldSlot(field, slot, reset) {
      var group = resolveElement(slot.group);
      var input = /** @type {HTMLInputElement|null} */ (resolveElement(slot.input));
      var reveal = /** @type {HTMLElement|null} */ (resolveElement(slot.reveal));

      if (!field) {
        if (group) {
          group.classList.add("hidden");
        }
        return;
      }

      if (group) {
        group.classList.remove("hidden");
      }
      if (input) {
        input.setAttribute("placeholder", field.placeholder);
        input.setAttribute("type", field.type);
        if (reset) {
          input.value = "";
        }
      }
      if (reveal) {
        reveal.style.display = field.reveal ? "" : "none";
      }
    }

    /**
     * Apply a mode descriptor to the form: title, field shape and placeholders.
     * The initial application keeps prefilled values (RouterOS `$(username)`),
     * while a mode switch resets them.
     * @param {typeof LOGIN_MODES[string]} descriptor
     * @param {Record<string, any>} [fields]
     * @param {{reset?: boolean, applyTitle?: boolean}} [options]
     */
    function applyLoginFields(descriptor, fields, options) {
      var slots = resolveFieldSlots(fields);
      var reset = Boolean(options && options.reset);
      // With a single enabled mode the page (cfgLogin.label) keeps owning the
      // title; the descriptor only takes over once modes can be switched.
      var applyTitle = !options || options.applyTitle !== false;

      var titleEl = resolveElement(slots.title);
      if (titleEl && applyTitle) {
        titleEl.textContent = descriptor.title;
      }

      /** @type {Record<string, LoginModeField>} */
      var layout = {};
      for (var i = 0; i < descriptor.fields.length; i++) {
        layout[descriptor.fields[i].slot] = descriptor.fields[i];
      }

      applyFieldSlot(layout.primary, slots.primary, reset);
      applyFieldSlot(layout.secondary, slots.secondary, reset);
    }

    /**
     * @param {LoginMode} mode
     * @param {string} active
     * @param {(id: string) => void} onSelect
     * @returns {Element}
     */
    function buildModeTab(mode, active, onSelect) {
      var tab = createTextElement("button", "login-modes__tab", mode.label);
      tab.setAttribute("type", "button");
      tab.setAttribute("role", "tab");
      tab.setAttribute("data-mode", mode.id);
      tab.setAttribute("aria-selected", mode.id === active ? "true" : "false");
      if (mode.id === active) {
        tab.classList.add("login-modes__tab--active");
      }
      tab.addEventListener("click", function () {
        onSelect(mode.id);
      });
      return tab;
    }

    /**
     * @param {LoginMode[]} modes
     * @param {string} [requested]
     * @returns {string}
     */
    function resolveActiveMode(modes, requested) {
      for (var i = 0; i < modes.length; i++) {
        if (modes[i].id === requested) {
          return String(requested);
        }
      }
      return modes.length ? modes[0].id : DEFAULT_LOGIN_MODE;
    }

    /**
     * Render the single login mode control: a segmented control for the enabled
     * modes (only when more than one is enabled) plus the active mode's form
     * shape. Returns the active mode so the page can pass it to `login`.
     * @param {Element|string} container
     * @param {{config?: LoginConfig, modes?: LoginMode[], activeMode?: string, fields?: Record<string, any>, ariaLabel?: string, onChange?: (mode: string) => void}} [options]
     * @returns {{mode: string, modes: string[], element: Element}}
     */
    function renderLoginModes(container, options) {
      var opts = options || {};
      var target = resolveContainer(container);
      var modes = opts.modes && opts.modes.length ? opts.modes : resolveLoginModes(opts.config !== undefined ? opts.config : loginConfig);
      var requested = opts.activeMode !== undefined ? opts.activeMode : defaultLoginMode();
      var active = resolveActiveMode(modes, requested);

      var control = createElement("div", "login-modes");
      control.setAttribute("role", "tablist");
      control.setAttribute("aria-label", opts.ariaLabel || "Mode login");

      /** @param {string} modeId */
      function select(modeId) {
        if (modeId === active) {
          return;
        }
        active = modeId;
        var tabs = control.querySelectorAll(".login-modes__tab");
        for (var i = 0; i < tabs.length; i++) {
          var isActive = tabs[i].getAttribute("data-mode") === active;
          tabs[i].classList.toggle("login-modes__tab--active", isActive);
          tabs[i].setAttribute("aria-selected", isActive ? "true" : "false");
        }
        applyLoginFields(loginModeDescriptor(active), opts.fields, { reset: true, applyTitle: true });
        if (typeof opts.onChange === "function") {
          opts.onChange(active);
        }
      }

      if (modes.length > 1) {
        for (var i = 0; i < modes.length; i++) {
          control.appendChild(buildModeTab(modes[i], active, select));
        }
      }

      target.innerHTML = "";
      if (modes.length > 1) {
        target.classList.remove("hidden");
        target.appendChild(control);
      } else {
        target.classList.add("hidden");
      }

      applyLoginFields(loginModeDescriptor(active), opts.fields, { reset: false, applyTitle: modes.length > 1 });

      return {
        mode: active,
        modes: modes.map(function (mode) {
          return mode.id;
        }),
        element: target,
      };
    }

    /**
     * Map the form slot values onto the credential keys the active mode expects,
     * so the page never hardcodes which input belongs to which mode.
     * @param {string} mode
     * @param {{primary?: string, secondary?: string}} values
     * @returns {Record<string, string>}
     */
    function loginCredentials(mode, values) {
      var descriptor = loginModeDescriptor(mode);
      var input = values || {};
      /** @type {Record<string, string>} */
      var credentials = {};
      for (var i = 0; i < descriptor.fields.length; i++) {
        var key = descriptor.fields[i].key;
        var value = input[descriptor.fields[i].slot];
        credentials[key] = value == null ? "" : String(value);
      }
      return credentials;
    }

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
     * Fetch the end of the Active Period from the public backend endpoint for a
     * given Phone Number (see ADR 0005).
     * @param {string} phoneNumber
     * @returns {Promise<{expiry: unknown}>}
     */
    function fetchActivePeriod(phoneNumber) {
      if (!config.backendBaseUrl) {
        return Promise.reject(new VoucherError("config_missing", "Backend base URL is not configured"));
      }

      var doFetch = fetchImpl || globalFetch();
      if (typeof doFetch !== "function") {
        return Promise.reject(new VoucherError("fetch_unavailable", "No fetch implementation is available"));
      }

      var url = joinUrl(config.backendBaseUrl, config.activePeriodPath) + "?phone=" + encodeURIComponent(String(phoneNumber == null ? "" : phoneNumber));

      return Promise.resolve()
        .then(function () {
          return doFetch(url, { method: "GET", headers: { Accept: "application/json" } });
        })
        .then(function (response) {
          if (response && response.ok === false) {
            throw new VoucherError("http_error", "Active Period request failed with status " + response.status, response);
          }
          return response.json();
        })
        .then(normalizeActivePeriod)
        .catch(function (error) {
          if (error instanceof VoucherError) {
            throw error;
          }
          throw new VoucherError("network_error", "Active Period request failed", error);
        });
    }

    /**
     * Summarize the Active Period from an expiry value: how much is left, how
     * urgent it is, and what the user should do next.
     * @param {unknown} expiry
     * @param {{ now?: unknown, warningMs?: number }} [options]
     * @returns {{state: "active"|"expiring"|"expired", remainingMs: number, remainingText: string, message: string, actionLabel: string}|null}
     */
    function activePeriodStatus(expiry, options) {
      var opts = options || {};
      var nowMs = opts.now != null ? toMillis(opts.now) : Date.now();
      var expiryMs = toMillis(expiry);
      if (nowMs === null || expiryMs === null) {
        return null;
      }

      var warningMs = opts.warningMs != null ? Number(opts.warningMs) : config.expiryWarningHours * 3600000;
      var remainingMs = expiryMs - nowMs;
      var state = remainingMs <= 0 ? "expired" : remainingMs <= warningMs ? "expiring" : "active";

      var messages = {
        active: "Active Period masih berjalan.",
        expiring: "Active Period hampir habis. Segera Extend.",
        expired: "Active Period telah berakhir. Beli paket baru untuk kembali online.",
      };
      var actionLabels = { active: "Extend", expiring: "Extend", expired: "Beli Paket" };

      return {
        state: /** @type {"active"|"expiring"|"expired"} */ (state),
        remainingMs: remainingMs,
        remainingText: remainingMs > 0 ? formatActivePeriod(remainingMs) : "",
        message: messages[state],
        actionLabel: actionLabels[state],
      };
    }

    /**
     * Render the Active Period summary and its Extend / buy-again guidance. The
     * action points at the Package list so the user can pick a Package.
     * @param {Element|string} container
     * @param {ReturnType<typeof activePeriodStatus>} status
     * @param {{ packagesTarget?: string }} [options]
     * @returns {Element}
     */
    function renderActivePeriod(container, status, options) {
      var target = resolveContainer(container);
      var opts = options || {};
      target.innerHTML = "";

      if (!status) {
        target.appendChild(createTextElement("p", "active-period__unknown", MESSAGES.statusUnavailable));
        return target;
      }

      var wrap = createElement("div", "active-period active-period--" + status.state);
      if (status.remainingMs > 0) {
        wrap.appendChild(createTextElement("p", "active-period__remaining", "Sisa Active Period: " + status.remainingText));
      }
      wrap.appendChild(createTextElement("p", "active-period__message", status.message));

      var action = createElement("a", "active-period__action");
      action.setAttribute("href", "#" + (opts.packagesTarget || "paketInternet"));
      action.textContent = status.actionLabel;
      wrap.appendChild(action);

      target.appendChild(wrap);
      return target;
    }

    /**
     * @param {Element|string} container
     * @param {Array<Record<string, unknown>>} packages
     * @returns {Element}
     */
    function renderPackages(container, packages) {
      var target = resolveContainer(container);
      var items = normalizePackageList(packages);
      var labels = readPackageLabels();
      target.innerHTML = "";

      var section = createElement("section", "package-list");
      var intro = createElement("div", "package-list__intro");
      intro.appendChild(createTextElement("p", "package-list__title", labels.title));
      if (labels.subtitle) {
        intro.appendChild(createTextElement("p", "package-list__subtitle", labels.subtitle));
      }
      section.appendChild(intro);

      var list = createElement("div", "package-list__items");
      if (items.length === 0) {
        list.appendChild(createTextElement("p", "package-list__empty", MESSAGES.empty));
      } else {
        for (var i = 0; i < items.length; i++) {
          list.appendChild(buildPackageCard(items[i]));
        }
      }
      section.appendChild(list);
      target.appendChild(section);
      return section;
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

      var head = createElement("div", "package-card__head");
      head.appendChild(createTextElement("div", "package-card__name", pkg.name));
      head.appendChild(createTextElement("div", "package-card__price", pkg.price));
      card.appendChild(head);

      // The Active Period and the speed limit are attributes of the Package, not
      // prose: pills keep them scannable next to the price.
      var meta = createElement("div", "package-card__meta");
      if (pkg.activePeriod) {
        meta.appendChild(createTextElement("span", "package-card__period", pkg.activePeriod));
      }
      if (pkg.speedLimit) {
        meta.appendChild(createTextElement("span", "package-card__speed", pkg.speedLimit));
      }
      if (meta.childNodes.length > 0) {
        card.appendChild(meta);
      }

      var button = createTextElement("button", "package-card__buy", "Beli");
      button.setAttribute("type", "button");

      // Resolve the destination up front so a missing purchase app (or Package
      // identity) degrades to a disabled button instead of throwing from a
      // click handler on the Login Page. The URL is rebuilt on click so the
      // return target reflects the Login Page at that moment.
      var destination = null;
      var unavailableMessage = MESSAGES.purchaseAppUnavailable;
      try {
        destination = buildPurchaseUrl(config, pkg);
      } catch (error) {
        destination = null;
        if (error && error.code === "missing_package_id") {
          unavailableMessage = MESSAGES.packageUnavailable;
        }
      }
      if (!destination) {
        button.setAttribute("disabled", "");
        button.setAttribute("aria-disabled", "true");
        button.setAttribute("title", unavailableMessage);
      }

      button.addEventListener("click", function () {
        var target = null;
        try {
          target = buildPurchaseUrl(config, pkg);
        } catch (error) {
          target = null;
        }
        if (target) {
          navigate(target);
        }
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

    /**
     * Load the Package list and render it into a container, showing a loading
     * indicator first. A failed fetch never rejects: it degrades to an empty or
     * error state so the rest of the Login Page (notably the hotspot login
     * form) keeps working. An invalid container still throws synchronously,
     * because that is a programming error, not a runtime condition.
     * @param {Element|string} container
     * @returns {Promise<{status: "success"|"empty"|"error", packages: PackageOption[], error: (Error|null)}>}
     */
    function loadPackages(container) {
      renderStatus(container, "loading");
      return fetchPackages().then(
        function (packages) {
          renderPackages(container, packages);
          return {
            status: packages.length ? "success" : "empty",
            packages: packages,
            error: null,
          };
        },
        function (error) {
          var isUnconfigured = Boolean(error && error.code === "config_missing");
          renderStatus(container, "error", isUnconfigured ? MESSAGES.notConfigured : undefined);
          return {
            status: "error",
            packages: [],
            error: error,
          };
        }
      );
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
     * Build the hotspot login payload for the active login mode, hashing the
     * credential with CHAP, and hand it to the submit hook when one is
     * available. `mode` defaults to the deployment's default mode (Phone Number
     * + PIN), and an unknown mode is rejected.
     * @param {LoginCredentials} credentials
     * @returns {LoginPayload}
     */
    function login(credentials) {
      var input = credentials || /** @type {LoginCredentials} */ ({});
      var descriptor = loginModeDescriptor(input.mode);

      var validationError = descriptor.validate(input);
      if (validationError) {
        throw new VoucherError(validationError, descriptor.errors[validationError] || validationError);
      }

      var pair = descriptor.credentials(input);
      var payload = {
        username: String(pair.username),
        password: hashPin(String(pair.password), input.chapId, input.chapChallenge),
      };

      var submit = input.submit || submitLogin;
      if (typeof submit === "function") {
        submit(payload);
      }
      return payload;
    }

    /**
     * Translate a client-side login validation failure (from `login`) into a
     * message the Login Page can show the user. The credential messages follow
     * the active mode.
     * @param {unknown} error
     * @param {string} [mode]
     * @returns {string}
     */
    function describeLoginError(error, mode) {
      var descriptor = loginModeDescriptor(mode);
      var code = error && typeof error === "object" ? /** @type {any} */ (error).code : undefined;
      if (code && descriptor.messages[code]) {
        return descriptor.messages[code];
      }
      return descriptor.genericError;
    }

    /**
     * Translate a raw hotspot error / RouterOS expiry value into a human message,
     * distinguishing an expired account from a wrong credential. The expired
     * message is shared by every mode; the credential message follows the mode.
     * @param {string} [error]
     * @param {string} [expiry]
     * @param {string} [mode]
     * @returns {string}
     */
    function describeError(error, expiry, mode) {
      var descriptor = loginModeDescriptor(mode);
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
        return descriptor.credentialError;
      }
      return descriptor.genericError;
    }

    return {
      config: config,
      fetchPackages: fetchPackages,
      fetchActivePeriod: fetchActivePeriod,
      loadPackages: loadPackages,
      activePeriodStatus: activePeriodStatus,
      formatActivePeriod: formatActivePeriod,
      renderActivePeriod: renderActivePeriod,
      renderPackages: renderPackages,
      renderStatus: renderStatus,
      buildPurchaseUrl: purchaseUrlFor,
      hashPin: hashPin,
      loginModes: loginModes,
      defaultLoginMode: defaultLoginMode,
      loginModeDescriptor: loginModeDescriptor,
      renderLoginModes: renderLoginModes,
      loginCredentials: loginCredentials,
      login: login,
      describeError: describeError,
      describeLoginError: describeLoginError,
    };
  }

  var api = {
    createVoucherClient: createVoucherClient,
    resolveConfig: resolveConfig,
    resolveLoginConfig: resolveLoginConfig,
    resolveLoginModes: resolveLoginModes,
    VoucherError: VoucherError,
    DEFAULT_CONFIG: DEFAULT_CONFIG,
    messages: MESSAGES,
    client: createVoucherClient(),
  };

  return api;
});
