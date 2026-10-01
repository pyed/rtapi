# rtapi

`rtapi` is a small Go client for rTorrent's XML-RPC interface over SCGI. It is
the library used by [rtelegram](https://github.com/pyed/rtelegram).

## Requirements

- Go 1.26 or newer.
- rTorrent built with XML-RPC support.
- A local SCGI endpoint such as a protected Unix socket or
  `scgi_port = 127.0.0.1:5000`.

The rTorrent RPC endpoint has no authentication and exposes powerful methods.
Prefer a permission-protected Unix socket. Never expose SCGI directly to an
untrusted network; see rTorrent's
[official XML-RPC security guidance](https://github.com/rakshasa/rtorrent-doc/blob/master/RPC-Setup-XMLRPC.md).

## Install

```sh
go get github.com/pyed/rtapi@latest
```

## Example

```go
package main

import (
	"fmt"
	"log"

	"github.com/pyed/rtapi"
)

func main() {
	rt, err := rtapi.NewRtorrent("/run/user/1000/rtorrent.sock")
	if err != nil {
		log.Fatal(err)
	}

	torrents, err := rt.Torrents()
	if err != nil {
		log.Fatal(err)
	}
	for _, torrent := range torrents {
		fmt.Printf("%s: %d/%d bytes\n", torrent.Name, torrent.Completed, torrent.Size)
	}
}
```

TCP addresses such as `127.0.0.1:5000` are also accepted. Every SCGI request is
bounded by `rtapi.DefaultTimeout` (30 seconds) unless `Rtorrent.Timeout` is set.
Responses default to a 16 MiB safety bound; set `Rtorrent.MaxResponseSize` when
a legitimately large library needs more. Transport errors, malformed responses,
and XML-RPC faults are returned to the caller; `errors.As` can inspect an
`*rtapi.XMLRPCFault`.

## Important APIs and compatibility

- Transfer fields (`Size`, `Completed`, and `UpTotal`) contain exact byte counts.
- `Path` (`d.base_path`) is empty until rTorrent opens a torrent. `Directory`
  and `MultiFile` are always reported: `Directory` is the data directory of a
  multi-file torrent, or the directory containing a single-file torrent's file.
- `SpeedsWithError` reports failures. `Speeds` remains as a deprecated
  compatibility shim that cannot distinguish failure from zero traffic.
- `DownloadRaw` loads torrent bytes directly, avoiding credential-bearing
  intermediary URLs. `DownloadWithOptions` remains available for URL loading.
- `Torrents.Sort` takes an explicit `rtapi.Sorting` value. The unsafe
  process-global `CurrentSorting` variable was removed; call `Sort` on each
  returned value instead.
- `DeleteMetadata` erases metadata only after rTorrent acknowledges the RPC.
  The older `Delete(false, ...)` form remains as a deprecated compatibility shim.
- `Delete(true, ...)` returns `ErrUnsafeDataDelete` before any RPC or local
  filesystem access. Data belongs to the rTorrent host; an application that
  offers data deletion must enforce its own explicit local root and containment
  policy.

## Development

```sh
go test ./...
go vet ./...
go build ./...
```
