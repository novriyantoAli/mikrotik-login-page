var cfgLogin = {
  default: 0,
  option: 1,
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
      voucher: "Login Voucher",
      member: "Login Member",
    },
    button: {
      login: "Login",
      logout: "Logout",
    },

    form: {
      voucher_placeholder: "Voucher",
      member_placeholder: "Username",
      password_placeholder: "Password",
    },

    status: {
      login: "Info Login",
      logout: "Info Logout",
    },
  },
};
