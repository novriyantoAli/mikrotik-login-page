# Hotspot credentials: phone number as username + per-purchase PIN as password

RouterOS hotspot login (CHAP) requires a username/password pair, while app login uses a magic link (passwordless). The FreeRADIUS identity is therefore defined as: username = phone number, password = a 6-digit PIN generated anew on every successful purchase. Rejected alternatives: a fixed password per user (less secure) and password = username (insecure). A magic link cannot be the hotspot credential because CHAP requires a static hashable password.
