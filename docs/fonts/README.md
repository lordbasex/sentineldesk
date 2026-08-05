# Fonts

`archivo-variable-latin.woff2` — Archivo, variable (weight 100–900, width 62–125%),
latin subset, 88 KB.

Self-hosted rather than loaded from a font CDN. The site is served from GitHub
Pages, so a third-party request would be the only thing on the page that can be
slow, blocked, or logged elsewhere; and it keeps the page working from a clone
with no route to the internet, which is how the rest of this project already
treats its assets.

The `latin` subset is enough for all three languages the site is written in:
it covers U+0000–00FF, which carries every accented character Spanish and
Portuguese need (á é í ó ú ü ñ ¿ ¡ ã õ ç â ê ô à).

**Licence: SIL Open Font License 1.1** — see `OFL.txt`, which must travel with
the font file in any redistribution. Copyright 2020 The Archivo Project Authors,
<https://github.com/Omnibus-Type/Archivo>. The OFL is separate from and
compatible with the Apache 2.0 licence covering this repository's own code; the
font is not a derivative work of SentinelDesk and SentinelDesk is not a
derivative work of the font.

Source: <https://fonts.google.com/specimen/Archivo> (v25).
