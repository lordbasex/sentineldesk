# Your own certificates

Put your certificate here to serve TLS:

- **With Caddy** (`TLS_MODE=custom`): name them `fullchain.pem` (certificate plus
  intermediate chain) and `privkey.pem` (private key). This works for a
  purchased SSL wildcard (`*.example.com`), a corporate certificate, or one
  issued with certbot.
- **Without a proxy, directly in the backend**: mount the files and set
  `TLS_CERT=/certs/fullchain.pem` and `TLS_KEY=/certs/privkey.pem`. This
  directory is already mounted as `/certs` in the desktop container.

The folder is mounted read-only, and the keys are never copied into the image.
