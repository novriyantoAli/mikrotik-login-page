var translations = {
  see_all: "Lihat Semua",
  customer_name: "Nama",
  customer_phone: "No HP",
  customer_address: "Alamat",
  note: "Catatan",
  admin_note: "Catatan Admin",
  customer_note: "Catatan Pelanggan",
  customer_note_placeholder: "Tinggalkan catatan di sini",
  product: "Produk",
  cart: "Keranjang",
  order_detail: "Data Pesanan",
  add_to_cart: "+ Keranjang",
  total: "Total",
  total_order: "Total Order",
  checkout: "Pembayaran",
  order_process: "Buat Pesanan",
  hello: "Halo",
  order_confirmation: "Saya barusan melakukan pesanan",
  customer_data: "Data Pelanggan",
  payment_method: "Metode Pembayaran",
};

function getTranslation(key) {
  return translations[key] || key;
}

var cfgStore = {
  status: 0,

  name: "Dapoer Nikmat",
  slogan: "Lezatnya Bikin Ketagihan!",
  address: "Jl. Kenyang Sampai Pingsan No. 35",

  home: {
    display_product: {
      limit: 3,
    },
  },

  slide: {
    status: 0,
    delay: 5,
    imagePath: [
      "./public/store/slide/03.jpg",
      "./public/store/slide/04.jpg",
      "./public/store/slide/05.jpg",
    ],
  },

  adminNote: {
    status: 0,
    note: "Total order, belum termasuk ongkir, akan dihitung setelah Anda melakukan proses order.",
  },

  confirmToAdmin: {
    salutation: "Bang",
    name: "Jay",
    phoneNumber: "6289672057180",
  },

  orderForm: {
    address: {
      status: 0,
    },
  },

  methodPaymentGW: {
    status: 0,
    data: [
      {
        name: "Mayar (alfa, idm, qris, va)",
        link: "https://mayar.id",
      },
      {
        name: "Xendit (alfa, idm, qris, va)",
        link: "https://xendit.com",
      },
    ],
  },

  methodPaymentLocalBank: {
    status: 0,
    data: [
      {
        bankName: "COD",
        accountNumber: "",
        accountHolder: "",
      },
      {
        bankName: "BCA",
        accountNumber: "61610000000",
        accountHolder: "Erik",
      },
      {
        bankName: "Mandiri",
        accountNumber: "12340000000",
        accountHolder: "Erik",
      },
    ],
  },

  products: [
    {
      image: "./public/store/product/1.webp",
      name: "Chicken Enchilada Casserole",
      price: 28000,
    },
    {
      image: "./public/store/product/2.webp",
      name: "Cream Cheese Tart",
      price: 18000,
    },
    {
      image: "./public/store/product/3.webp",
      name: "Honey Teriyaki Salmon",
      price: 27000,
    },
    {
      image: "./public/store/product/4.webp",
      name: "Nanaimo Bars",
      price: 16500,
    },
  ],
};
