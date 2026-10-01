package rtapi

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// benchTorrents is the library size the benchmarks model: a large seedbox.
const benchTorrents = 3000

// benchHash returns a distinct 40-character info-hash for torrent i.
func benchHash(i int) string {
	return fmt.Sprintf("%040X", i*7919+1)
}

// benchListResponse is a d.multicall2 response for n seeding torrents with
// realistic names and paths, laid out the way xmlrpc-c writes it.
func benchListResponse(n int) string {
	var body strings.Builder
	body.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n<methodResponse>\r\n<params>\r\n<param><value><array><data>\r\n")
	for i := range n {
		name := fmt.Sprintf("Some.Linux.Distribution.%d.Release.x86_64.DVD-ISO.Remastered.Edition", i)
		dir := "/srv/torrents/data/completed/" + name
		values := []string{
			stringValue(name), stringValue(benchHash(i)), intValue(0), intValue(uint64(i * 13)),
			intValue(4_700_000_000), intValue(4_700_000_000), intValue(1534), intValue(7_209_800_000),
			intValue(1700000000 + uint64(i)), stringValue(""), stringValue(dir), intValue(1),
			stringValue("seed"), intValue(1), intValue(0), stringValue("linux"),
			stringValue(dir), intValue(1), intValue(1700003600 + uint64(i)),
		}
		body.WriteString("<value><array><data>\r\n")
		for _, value := range values {
			body.WriteString("<value>")
			body.WriteString(value)
			body.WriteString("</value>\r\n")
		}
		body.WriteString("</data></array></value>\r\n")
	}
	body.WriteString("</data></array></value></param>\r\n</params>\r\n</methodResponse>\r\n")
	return withSCGIHeader(body.String())
}

// withSCGIHeader puts the header rTorrent sends before SCGI responses.
func withSCGIHeader(xml string) string {
	return fmt.Sprintf("Status: 200 OK\r\nContent-Type: text/xml\r\nContent-Length: %d\r\n\r\n%s", len(xml), xml)
}

func benchTrackerResponse(n int) string {
	results := make([]string, n)
	for i := range results {
		results[i] = result(stringValue(fmt.Sprintf("https://tracker%d.example.org:443/announce", i%40)))
	}
	return withSCGIHeader(strings.TrimPrefix(arrayResponse(results...), "Status: 200 OK\r\nContent-Type: text/xml\r\n\r\n"))
}

func BenchmarkDecodeTorrentList(b *testing.B) {
	payload := benchListResponse(benchTorrents)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		resp, err := decodeMethodResponse(strings.NewReader(payload), 0)
		if err != nil {
			b.Fatal(err)
		}
		values, err := resp.arrayParam()
		if err != nil {
			b.Fatal(err)
		}
		for _, value := range values {
			if _, err := parseTorrent(value); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// benchClient serves canned responses without decoding requests, so that
// benchmarks measure the client alone.
func benchClient(b *testing.B, respond func(payload string) string) *Rtorrent {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				payload, err := readSCGIRequest(conn)
				if err == nil {
					io.WriteString(conn, respond(payload))
				}
			}()
		}
	}()
	return &Rtorrent{network: "tcp", address: listener.Addr().String(), Timeout: 10 * time.Second}
}

func BenchmarkTorrents(b *testing.B) {
	list, trackers := benchListResponse(benchTorrents), benchTrackerResponse(benchTorrents)
	client := benchClient(b, func(payload string) string {
		if strings.Contains(payload, "d.multicall2") {
			return list
		}
		return trackers
	})
	b.Logf("list response: %d KiB, tracker response: %d KiB", len(list)>>10, len(trackers)>>10)
	b.ReportAllocs()
	for b.Loop() {
		torrents, err := client.TorrentsContext(context.Background())
		if err != nil || len(torrents) != benchTorrents {
			b.Fatal(len(torrents), err)
		}
	}
}

func BenchmarkTrackerRequest(b *testing.B) {
	keys := make([]string, benchTorrents)
	for i := range keys {
		keys[i] = benchHash(i) + ":t0"
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := buildSystemMulticallRequest("t.url", keys...); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkList(b *testing.B) {
	list := benchListResponse(benchTorrents)
	client := benchClient(b, func(string) string { return list })
	b.ReportAllocs()
	for b.Loop() {
		torrents, err := client.ListContext(context.Background(), ListOptions{})
		if err != nil || len(torrents) != benchTorrents {
			b.Fatal(len(torrents), err)
		}
	}
}

func BenchmarkHashes(b *testing.B) {
	results := make([]string, benchTorrents)
	for i := range results {
		results[i] = result(stringValue(benchHash(i)))
	}
	hashes := withSCGIHeader(strings.TrimPrefix(arrayResponse(results...), "Status: 200 OK\r\nContent-Type: text/xml\r\n\r\n"))
	client := benchClient(b, func(string) string { return hashes })
	b.Logf("hashes response: %d KiB", len(hashes)>>10)
	b.ReportAllocs()
	for b.Loop() {
		got, err := client.HashesContext(context.Background())
		if err != nil || len(got) != benchTorrents {
			b.Fatal(len(got), err)
		}
	}
}
