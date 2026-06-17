Optional bootstrap credentials for device-specific Windows installers.

If present at build time, stage these exact files here:
- device.crt
- device.key
- ca-chain.pem

The generic installer should normally keep this directory empty except for this
placeholder file.
