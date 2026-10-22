# Three hotspot login modes

The Login Page supports three credential schemes, each deriving the hotspot username/password pair differently:

1. **Access Code** — a prepaid code used as both username and password (`password = username`).
2. **Phone Number + PIN** — Phone Number is the username, a 6-digit PIN is the password (ADR 0004).
3. **User** — a general username and password, neither a Phone Number/PIN nor an Access Code.

`password = username` is acceptable only for **Access Code**, where the code is a single secret by design. It stays rejected for **Phone Number + PIN**, where the Phone Number is public and must not double as the password, so ADR 0004 remains in force for that mode. PIN validation (exactly 6 digits) applies only to Phone Number + PIN. Each mode can be enabled per deployment and the user switches between the enabled modes with a segmented control; the default is Phone Number + PIN. Credential error messages follow the active mode, while the expired-account message is shared by all modes.
