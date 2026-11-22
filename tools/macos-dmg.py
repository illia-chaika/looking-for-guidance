"""Finder layout for the Looking for Guidance download. Loaded by dmgbuild."""

from pathlib import Path

app = Path(defines["app"])
files = [str(app)]
symlinks = {"Applications": "/Applications"}
icon = str(app / "Contents" / "Resources" / "AppIcon.icns")
background = defines["background"]

format = "UDZO"
filesystem = "HFS+"
default_view = "icon-view"
window_rect = ((180, 160), (660, 300))
icon_size = 128
text_size = 14
icon_locations = {app.name: (150, 135), "Applications": (510, 135)}
