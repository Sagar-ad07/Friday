# Friday — App Icon Assets

Generated from the master logo `interface/assets/friday-logo.svg` (concept **F1 —
Bold F + living core**). Palette: indigo `#9AA6FF` -> mint `#55E2C0` on obsidian
`#070A12`.

## Files
- `friday-logo.svg` / `.png` — full logo with dark rounded tile (brand + maskable icon).
- `friday-mark.svg` — transparent "F" mark (for use on the app's dark UI, topbar, splash).
- `ic_launcher_foreground.svg` + `ic_fg_*.png` — adaptive icon foreground (F mark,
  centered in the 108dp safe zone).
- `ic_launcher_background.svg` + `ic_bg_*.png` — adaptive icon background (dark radial).
- `ic_launcher_{48,72,96,144,192,512}.png` — legacy square launcher icons.
- `ic_launcher.xml` — adaptive-icon descriptor.

## Placing into the Android project (mipmap buckets)
Map the rendered PNGs to density buckets:

| Density  | legacy ic_launcher | adaptive fg/bg |
|----------|--------------------|----------------|
| mdpi     | ic_launcher_48     | ic_fg_108 / ic_bg_108 |
| hdpi     | ic_launcher_72     | ic_fg_162 / ic_bg_162 |
| xhdpi    | ic_launcher_96     | ic_fg_216 / ic_bg_216 |
| xxhdpi   | ic_launcher_144    | ic_fg_324 / ic_bg_324 |
| xxxhdpi  | ic_launcher_192    | ic_fg_432 / ic_bg_432 |

Copy into:
```
app/src/main/res/mipmap-mdpi/ic_launcher.png        (= ic_launcher_48.png)
app/src/main/res/mipmap-mdpi/ic_launcher_foreground.png (= ic_fg_108.png)
app/src/main/res/mipmap-mdpi/ic_launcher_background.png (= ic_bg_108.png)
...repeat per density...
app/src/main/res/mipmap-anydpi-v26/ic_launcher.xml  (= ic_launcher.xml)
app/src/main/res/mipmap-anydpi-v26/ic_launcher_round.xml (copy of ic_launcher.xml)
```
`ic_launcher_512.png` = Play Store / high-res preview (not needed for sideload).

> Rendered with headless Edge from the SVGs. To regenerate at other sizes, re-run the
> render commands in the build plan.
