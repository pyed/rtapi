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

TCP addresses such as `127.0.0.1:5000` are also accepted, as are `http://` and
`https://` XML-RPC URLs for rTorrent behind a web server, such as
`https://user:password@seedbox.example/RPC2`. Credentials in the URL are sent
with HTTP basic authentication and kept out of error messages; requests use
`Rtorrent.HTTPClient` when it is set, as for a proxy or a certificate
authority of one's own, and `http.DefaultClient`, which honors `HTTPS_PROXY`,
otherwise. Basic authentication over plain `http://` sends the password
readably; use `https://` beyond the local machine.

`NewRtorrent` asks rTorrent for its version, so it fails while rTorrent is
down. `NewClient` asks nothing, for programs that may start before rTorrent;
`GetVersion` asks for the version later. An address that is neither a URL nor
`host:port` is a socket path, whether or not the socket exists yet.

Every method has a `...Context` variant, such as `TorrentsContext`, and
`NewRtorrentContext`. Cancelling the context interrupts the request.
Cancellations and timeouts match `context.Canceled` and
`context.DeadlineExceeded` with `errors.Is`. Every SCGI request is
bounded by `rtapi.DefaultTimeout` (30 seconds) unless `Rtorrent.Timeout` is set.
Responses default to a 16 MiB safety bound; set `Rtorrent.MaxResponseSize` when
a legitimately large library needs more. Transport errors, malformed responses,
and XML-RPC faults are returned to the caller; `errors.As` can inspect an
`*rtapi.XMLRPCFault`.

## API notes

- Transfer fields (`Size`, `Completed`, and `UpTotal`) contain exact byte counts.
- `Path` (`d.base_path`) is empty until rTorrent opens a torrent. `Directory`
  and `MultiFile` are always reported: `Directory` is the data directory of a
  multi-file torrent, or the directory containing a single-file torrent's file.
- `SpeedsWithError` returns the current transfer rates.
- `DownloadRaw` loads torrent bytes directly, avoiding credential-bearing
  intermediary URLs. `DownloadWithOptions` remains available for URL loading.
  Set `DotTorrentWithOptions.Stopped` to load a torrent without starting it. An
  empty `Dir` or `Label` leaves rTorrent's default. rTorrent parses them as
  command arguments and runs one that starts with `$` as a command, so loads
  refuse such values, and those with control characters or invalid UTF-8,
  with `ErrCommandArgument`. URL loads use rTorrent's verbose load commands,
  so rTorrent logs why a link failed to load.
- `Torrents` makes two requests: one for every torrent's details and one, with
  a call per torrent, for their trackers. For large libraries, `List` with
  `ListOptions{}` skips the trackers, and `Trackers` fills them in later for
  the torrents that need them. `Hashes` lists only info-hashes, the cheapest
  way to see which torrents are loaded, and `Transfers` lists how much of each
  torrent's data has been uploaded and downloaded. Those counts leave out the
  protocol messages exchanged with peers, which rTorrent's global totals
  (`Stats`) include; a seeding library alone receives hundreds of megabytes
  of those a day.
- `GetTorrent` requests only the one torrent rather than listing them all.
- Errors of calls on a torrent rTorrent does not have, such as one removed
  since it was listed, match `ErrNotFound` with `errors.Is`; timeouts and
  other faults do not.
- `Torrent.Finished` is when a torrent completed and `Torrent.Started` when it
  first started, in Unix seconds, or 0 until then; rTorrent keeps both across
  restarts. `Torrent.Age` is when rTorrent loaded the torrent, which it does
  again for every torrent each time it starts, so `Started` is the better
  guide to when a torrent was added. `Torrent.Added` is `Started`, or `Age`
  for a torrent that never started, and `ByAdded` sorts by it; `ByAge`
  orders torrents by the time they were loaded.
- `Torrent.TrackerCount` is how many trackers a torrent lists. `Message` is
  what the last tracker to answer said, which need not be the first.
- `Torrent.Label` is `d.custom1`, where ruTorrent keeps its label, and
  `SetLabel` sets it. ruTorrent stores labels percent-encoded, as
  JavaScript's `encodeURIComponent` writes them, and decodes them to show
  them; rtapi passes labels through as they are.
- `Files` lists a torrent's files, and `SetFilePriorities` skips or prioritizes
  them by index (`FileSkip`, `FileNormal`, `FileHigh`).
- `GlobalLimits` and `SetGlobalLimits` read and set the global download and
  upload rate limits, in bytes per second; zero means unlimited.
- `FreeDiskSpace` reports the free space on the filesystem holding a torrent,
  and `FreeDiskSpaces` that of several torrents in one request. rTorrent
  knows where that is only for torrents it has opened, such as active ones,
  and reports 0 for the rest.
- `Connections` counts peer connections in one request: those peers opened
  to rTorrent, which they can only do when its port is reachable, and those
  rTorrent opened.
- `Torrents.Sort` takes an explicit `rtapi.Sorting` value. Sorting is stable
  and compares names case-insensitively.
- `DeleteMetadata` erases torrents from rTorrent and checks that rTorrent
  acknowledged every one. rtapi never deletes data: the data belongs to the
  rTorrent host, so an application that offers it must enforce its own
  containment policy there.

## Versions

From v1.0.0, rtapi follows semantic versioning: v1 releases add to the API but
do not break it.

v1.1.0 adds `List`, `Trackers`, and `Hashes`, and decodes responses several
times faster with a fraction of the memory, which matters for libraries of
thousands of torrents. If rTorrent drops `d.multicall2`, as it plans to,
listing switches to `d.multicall`.

v1.2.0 adds `Torrent.Started`, and v1.3.0 adds `Transfers`, `Stats.PID`
(rTorrent's process ID, which tells when rTorrent has restarted),
`Torrent.Private`, `SetLabel`, `FreeDiskSpaces`, and `Connections`.

v1.4.0 refuses a load's `Dir` or `Label` that rTorrent would run as a command
(`ErrCommandArgument`), and adds `ErrNotFound`, `NewClient` and `GetVersion`,
`Rtorrent.HTTPClient`, `Torrent.Added` with `ByAdded` sorting, and
`Torrent.TrackerCount`. Socket paths count as sockets before the socket
exists.

Upgrading from v0:

- `Speeds` is gone; use `SpeedsWithError` or `SpeedsContext`, which report
  failures instead of returning zero.
- `Delete` and `ErrUnsafeDataDelete` are gone; use `DeleteMetadata`.
- The process-global `CurrentSorting` variable is gone; pass an
  `rtapi.Sorting` to `Torrents.Sort`.

## Development

```sh
go test ./...
go vet ./...
go build ./...
```
