var cfgLogin = {
  default: 0,
  // Legacy voucher/member switch, kept at 0: the Login Page uses the `modes`
  // section below instead of that old pair of labels (ADR 0006).
  option: 0,

  // Login modes (ADR 0006). Each toggle enables one credential scheme and
  // `label` is the segmented-control label for it. Only Phone Number + PIN is
  // enabled by default, so the Login Page behaves exactly as before and no tab
  // is shown; the segmented control only appears once more than one is enabled.
  // The form title, field shape and placeholders are defined by the mode
  // descriptors in assets/voucher-client.js, not here.
  modes: {
    accessCode: { status: 0, label: "Kode Akses" },
    phone: { status: 1, label: "Nomor HP" },
    user: { status: 0, label: "User" },
  },

  menu: {
    voucher: {
      status: 1,
    },
    member: {
      status: 1,
    },
  },

  voucher: {
    value: "default",
    _comment: "default / upper / lower",
  },

  member: {
    value: "default",
    _comment: "default / upper / lower",
  },

  scan_qrcode: {
    status: 0,
    url: "https://eriiksanjaya.github.io/qr",
  },

  label: {
    menu: {
      status: "Status",
      login: "Login",
      voucher: "VOUCHER",
      member: "MEMBER",
      trial: "GRATIS",
      scan: "SCAN",
    },
    title: {
      voucher: "Login",
      member: "Login",
    },
    button: {
      login: "Login",
      logout: "Logout",
    },

    form: {
      voucher_placeholder: "Phone Number",
      member_placeholder: "Phone Number",
      password_placeholder: "PIN",
    },

    status: {
      login: "Info Login",
      logout: "Info Logout",
    },
  },
};
