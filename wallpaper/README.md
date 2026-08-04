# Wallpaper

Drop your background images here (`.png`, `.jpg`, `.jpeg` or `.webp`). They are
picked up on the next start.

```bash
cp ~/Downloads/my-wallpaper.jpg wallpaper/
docker compose -f deploy/docker-compose.yml restart sentineldesk
```

With **two or more images the wallpaper rotates**, picking a random one every
five minutes and never repeating the current one. `WALLPAPER_ROTATE_SECS`
changes the interval, and `0` turns rotation off. Images added while the
container is running are noticed without a restart.

To pin one image and stop the rotation, set an explicit path:
`WALLPAPER=/wallpaper/one.jpg`.

With nothing here, the built-in fallback wallpaper is used (rendered from SVG to
PNG at build time).

Recommended resolution: the desktop's own
(`DISPLAY_WIDTH`x`DISPLAY_HEIGHT`, 1920x1080 by default). Images are scaled to
fill and cropped when the aspect ratio does not match.
