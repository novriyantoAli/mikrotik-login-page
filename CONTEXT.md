# Mikrotik Login Page — Voucher Purchase

System for managing internet hotspot voucher purchases: the MikroTik login page displays a package list from the backend, the user is directed to a web app (Svelte) to buy an active period, then logs in to the hotspot using their phone number and PIN.

## Language

**Package**:
A sellable product consisting of a name, price, duration (active period), and speed limit. Managed by admin.
_Avoid_: Voucher, internet product

**Active Period**:
The internet access duration purchased by the user, computed from the expiration attribute.
_Avoid_: Time quota, validity, masa aktif

**Phone Number**:
The user's phone number; used as the account identity in the app and as the FreeRADIUS username.
_Avoid_: Username, MSISDN

**PIN**:
A 6-digit code auto-generated on every successful purchase, used as the FreeRADIUS password for hotspot login.
_Avoid_: Password, secret

**Access Code**:
A prepaid code sold or printed, used as both the FreeRADIUS username and password when logging in to the hotspot.
_Avoid_: Voucher, Voucher Code, kode voucher

**User**:
A hotspot account identified by a username and password that are neither a Phone Number/PIN nor an Access Code.
_Avoid_: account, member

**Magic Link**:
A clickable URL sent via SMS to the phone number for passwordless app login.
_Avoid_: OTP, token

**Extend**:
Buying another package while the active period is still running; the new duration is added to the existing expiration.
_Avoid_: Renew, top-up

**Transaction**:
A single package purchase by a user, covering payment status and provisioning result.
_Avoid_: Order, purchase

**Login Page**:
The MikroTik hotspot captive portal (KOPINETGO-2024) where the user enters Phone Number + PIN to get online and views the Package list.
_Avoid_: Landing page, portal

**Account Status**:
The page in the Svelte app showing the active package, remaining active period, and the user's transaction history.
_Avoid_: Dashboard

**Admin Panel**:
The page in the Svelte app for admins to manage Packages, view transactions, and list active users.
_Avoid_: Backoffice
