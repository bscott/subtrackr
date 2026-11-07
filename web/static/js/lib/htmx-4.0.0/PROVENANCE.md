# htmx 4.0.0 (vendored)

`htmx.min.js` and `LICENSE` are copied unmodified from the npm package
`htmx.org@4.0.0` (`dist/htmx.min.js`, `LICENSE`). They are served locally so
pages do not depend on a third-party CDN.

- Upstream: https://github.com/bigskysoftware/htmx
- Package: https://registry.npmjs.org/htmx.org/-/htmx.org-4.0.0.tgz
- Tarball sha1 (npm `shasum`): `72323bd24261364fe3111803c46b80da1fb33445`
- Tarball integrity (npm `dist.integrity`):
  `sha512-T/171FUY93Kdfp8t+DnHdk45QvKRiBhVhhrwSzrXgUi4pHKvhp77dUA/qg8FAjsFWPIHNbmUuIdCrcVHuiZWng==`
- `htmx.min.js` sha256: `e484d9171a9db30a39c8f16e3d709d4137f3211c659f8e6125816635033d593f`
- `htmx.min.js` SRI (used in the templates' `integrity` attribute):
  `sha384-BvJpBiO8Kh31EqtJe5DRIeWrHWnCGkwytKs9NKFi86Hhw96dEqdEMzZDeK9iEGTc`
- License: Zero-Clause BSD (see `LICENSE`)

To verify or upgrade:

```bash
npm pack htmx.org@<version>
shasum htmx.org-<version>.tgz        # compare with `npm view htmx.org@<version> dist.shasum`
tar xzf htmx.org-<version>.tgz
openssl dgst -sha384 -binary package/dist/htmx.min.js | base64
```

Upgrading means a new `htmx-<version>/` directory, new script `src` and
`integrity` values in every template that loads htmx, and updated hashes here.
