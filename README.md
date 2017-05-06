# rtapi
Raw rTorrent XML-RPC in Go, Written for [rtelegram](https://github.com/pyed/rtelegram)

## Requirements
* [`rTorrent`](https://github.com/rakshasa/rtorrent) compiled with `--with-xmlrpc-c`.
* `scgi_port = localhost:5000` in your `.rtorrent.rc`

## How to get
`go get github.com/pyed/rtapi`

## How to use
``` go
package main

import (
	"fmt"

	"github.com/pyed/rtapi"
)

func main() {
	rt, err := rtapi.NewRtorrent("localhost:5000") // Or /path/to/socket for "scgi_local".
	if err != nil {
		// ...
	}

	// Get torrents
	torrents, err := rt.Torrents()
	if err != nil {
		// ...
	}

	fmt.Println("Number of torrents:", len(torrents))

	for _, t := range torrents {
		fmt.Println(t.Name)
	}
}
```
