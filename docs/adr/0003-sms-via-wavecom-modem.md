# Send SMS via a local Wavecom modem (AT commands), not a third-party gateway

Magic links and PINs are sent as SMS using a Wavecom modem attached via USB to the backend machine (AT commands / gammu). Chosen for zero per-SMS cost and full locality; the consequence is that the backend must run on the same machine as the modem, the modem is a single point of failure, and there is no fallback channel. Third-party gateways (Fonnte/Twilio/etc.) were rejected due to recurring cost and dependence on external connectivity.
