# Third-party notices

MobFI links (or, in the case of vendored assets, ships) the modules
below. Each is redistributed under the license shown. This file
discharges the notice-preservation clause of every MIT / BSD /
Apache-2.0 / ISC / Unlicense grant in the linked set.

## Go module dependencies

Verified against `pkg.go.dev/<module>?tab=licenses` on 2026-07-31.
No copyleft (GPL / AGPL / LGPL) or MPL-linked module ships in the
MobFI binary.

### Direct

| Module | Version | License | SPDX | Upstream |
|---|---|---|---|---|
| `github.com/UserExistsError/conpty` | v0.1.4 | MIT | MIT | https://github.com/UserExistsError/conpty |
| `github.com/alecthomas/chroma/v2` | v2.27.0 | MIT (+ OFL-1.1 for one embedded font, not used) | MIT | https://github.com/alecthomas/chroma |
| `github.com/creack/pty` | v1.1.24 | MIT | MIT | https://github.com/creack/pty |
| `github.com/wailsapp/wails/v2` | v2.13.0 | MIT | MIT | https://github.com/wailsapp/wails |
| `golang.org/x/crypto` | v0.54.0 | BSD-3-Clause | BSD-3-Clause | https://cs.opensource.google/go/x/crypto |
| `golang.org/x/sys` | v0.47.0 | BSD-3-Clause | BSD-3-Clause | https://cs.opensource.google/go/x/sys |
| `modernc.org/sqlite` | v1.54.0 | BSD-3-Clause | BSD-3-Clause | https://gitlab.com/cznic/sqlite |

### Indirect

| Module | Version | License | SPDX | Upstream |
|---|---|---|---|---|
| `git.sr.ht/~jackmordaunt/go-toast/v2` | v2.0.3 | Unlicense OR MIT | Unlicense | https://git.sr.ht/~jackmordaunt/go-toast |
| `github.com/bep/debounce` | v1.2.1 | MIT | MIT | https://github.com/bep/debounce |
| `github.com/dlclark/regexp2/v2` | v2.2.1 | MIT | MIT | https://github.com/dlclark/regexp2 |
| `github.com/dustin/go-humanize` | v1.0.1 | MIT | MIT | https://github.com/dustin/go-humanize |
| `github.com/go-ole/go-ole` | v1.3.0 | MIT | MIT | https://github.com/go-ole/go-ole |
| `github.com/godbus/dbus/v5` | v5.1.0 | BSD-2-Clause | BSD-2-Clause | https://github.com/godbus/dbus |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause | BSD-3-Clause | https://github.com/google/uuid |
| `github.com/gorilla/websocket` | v1.5.3 | BSD-2-Clause | BSD-2-Clause | https://github.com/gorilla/websocket |
| `github.com/jchv/go-winloader` | v0.0.0-20210711 | ISC | ISC | https://github.com/jchv/go-winloader |
| `github.com/labstack/echo/v4` | v4.13.3 | MIT | MIT | https://github.com/labstack/echo |
| `github.com/labstack/gommon` | v0.4.2 | MIT | MIT | https://github.com/labstack/gommon |
| `github.com/leaanthony/go-ansi-parser` | v1.6.1 | MIT | MIT | https://github.com/leaanthony/go-ansi-parser |
| `github.com/leaanthony/gosod` | v1.0.4 | MIT | MIT | https://github.com/leaanthony/gosod |
| `github.com/leaanthony/slicer` | v1.6.0 | MIT | MIT | https://github.com/leaanthony/slicer |
| `github.com/leaanthony/u` | v1.1.1 | MIT | MIT | https://github.com/leaanthony/u |
| `github.com/mattn/go-colorable` | v0.1.13 | MIT | MIT | https://github.com/mattn/go-colorable |
| `github.com/mattn/go-isatty` | v0.0.20 | MIT | MIT | https://github.com/mattn/go-isatty |
| `github.com/ncruces/go-strftime` | v1.0.0 | MIT | MIT | https://github.com/ncruces/go-strftime |
| `github.com/pkg/browser` | v0.0.0-20240102 | BSD-2-Clause | BSD-2-Clause | https://github.com/pkg/browser |
| `github.com/pkg/errors` | v0.9.1 | BSD-2-Clause | BSD-2-Clause | https://github.com/pkg/errors |
| `github.com/remyoudompheng/bigfft` | v0.0.0-20230129 | BSD-3-Clause | BSD-3-Clause | https://github.com/remyoudompheng/bigfft |
| `github.com/rivo/uniseg` | v0.4.7 | MIT | MIT | https://github.com/rivo/uniseg |
| `github.com/samber/lo` | v1.49.1 | MIT | MIT | https://github.com/samber/lo |
| `github.com/tkrajina/go-reflector` | v0.5.8 | Apache-2.0 | Apache-2.0 | https://github.com/tkrajina/go-reflector |
| `github.com/valyala/bytebufferpool` | v1.0.0 | MIT | MIT | https://github.com/valyala/bytebufferpool |
| `github.com/valyala/fasttemplate` | v1.2.2 | MIT | MIT | https://github.com/valyala/fasttemplate |
| `github.com/wailsapp/go-webview2` | v1.0.22 | MIT | MIT | https://github.com/wailsapp/go-webview2 |
| `github.com/wailsapp/mimetype` | v1.4.1 | MIT | MIT | https://github.com/wailsapp/mimetype |
| `golang.org/x/net` | v0.57.0 | BSD-3-Clause | BSD-3-Clause | https://cs.opensource.google/go/x/net |
| `golang.org/x/text` | v0.40.0 | BSD-3-Clause | BSD-3-Clause | https://cs.opensource.google/go/x/text |
| `modernc.org/libc` | v1.74.1 | BSD-3-Clause | BSD-3-Clause | https://gitlab.com/cznic/libc |
| `modernc.org/mathutil` | v1.7.1 | BSD-3-Clause | BSD-3-Clause | https://gitlab.com/cznic/mathutil |
| `modernc.org/memory` | v1.11.0 | BSD-3-Clause | BSD-3-Clause | https://gitlab.com/cznic/memory |

Test-only dependencies (present in `go.sum`, not linked into the
shipped binary): `github.com/stretchr/testify` (MIT),
`github.com/davecgh/go-spew` (ISC),
`github.com/pmezard/go-difflib` (BSD-3-Clause),
`gopkg.in/yaml.v3` (MIT + Apache-2.0),
`github.com/hashicorp/golang-lru/v2` (MPL-2.0, transitive test dep,
not imported by MobFI), `github.com/hexops/gotextdiff`
(BSD-3-Clause, transitive test dep, not imported by MobFI).

## Vendored assets

Distributed unmodified in `cmd/mfi-gui/frontend/dist/vendor/`.

| File | Origin | License | Notice file |
|---|---|---|---|
| `xterm.js` | https://github.com/xtermjs/xterm.js | MIT | `xterm.LICENSE` |
| `xterm.css` | https://github.com/xtermjs/xterm.js | MIT | header preserved in file |
| `addon-fit.js` | https://github.com/xtermjs/xterm.js/tree/master/addons/xterm-addon-fit | MIT | `addon-fit.LICENSE` |

## License texts

### MIT

Every module tagged MIT above is redistributed under the standard
MIT terms. Representative text (identical wording; only the
copyright line differs per author):

```
Copyright (c) <year> <holder>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

Per-author copyright lines live in each module's upstream `LICENSE`
file; consult the Upstream column above.

### BSD-2-Clause

Representative text:

```
Copyright (c) <year> <holder>. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

  1. Redistributions of source code must retain the above copyright notice,
     this list of conditions and the following disclaimer.
  2. Redistributions in binary form must reproduce the above copyright
     notice, this list of conditions and the following disclaimer in the
     documentation and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES ARE DISCLAIMED. IN NO EVENT SHALL THE
COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT,
INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES ... [standard BSD-2
disclaimer continues]
```

### BSD-3-Clause

Same as BSD-2-Clause plus the third clause forbidding endorsement:

```
  3. Neither the name of the copyright holder nor the names of its
     contributors may be used to endorse or promote products derived from
     this software without specific prior written permission.
```

### ISC

```
Copyright (c) <year> <holder>

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE ... [ISC disclaimer]
```

### Apache-2.0

The `github.com/tkrajina/go-reflector` module is Apache-2.0. Its
upstream ships no `NOTICE` file, so no additional attribution is
owed beyond a reference to the license text (available at
https://www.apache.org/licenses/LICENSE-2.0). If a future direct
import of an Apache-2.0 module that DOES ship a NOTICE file lands
in `go.mod`, its NOTICE contents must be reproduced here.

### Unlicense

Public-domain dedication; no notice requirement. Included for
completeness only.

## Verification

The dependency inventory in this file matches the audit recorded in
`LICENSE-AUDIT.md`. Refresh it when `go.mod` / `go.sum` changes and
any new module's license differs from the ones already listed.
