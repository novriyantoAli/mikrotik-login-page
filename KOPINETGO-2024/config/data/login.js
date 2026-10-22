var cfgLogin = {
  default: 0,
  // No voucher/member mode switch: the Login Page uses one credential pair,
  // Phone Number + PIN (ADR 0004).
  option: 0,
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
