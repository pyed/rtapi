package rtapi

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestListFetchesTrackersOnlyWhenAsked(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		mu.Lock()
		methods = append(methods, call.MethodName)
		mu.Unlock()
		switch call.MethodName {
		case "d.multicall2":
			return arrayResponse(torrentRow("tracked", testHash), torrentRow("other", strings.Repeat("B", 40)))
		case "system.multicall":
			return arrayResponse(result(stringValue("udp://tracker.invalid:80")), result(stringValue("")))
		}
		t.Errorf("unexpected method %q", call.MethodName)
		return topLevelFault(-1, "unexpected")
	})
	called := func() []string {
		mu.Lock()
		defer mu.Unlock()
		defer func() { methods = nil }()
		return methods
	}

	torrents, err := client.List(ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(torrents) != 2 || torrents[0].Name != "tracked" || torrents[0].Tracker != nil {
		t.Fatalf("List = %+v", torrents)
	}
	if got := called(); len(got) != 1 {
		t.Fatalf("List without trackers made calls %v", got)
	}

	if err := client.Trackers(torrents); err != nil {
		t.Fatal(err)
	}
	if torrents[0].Tracker == nil || torrents[0].Tracker.Host != "tracker.invalid:80" || torrents[1].Tracker != nil {
		t.Fatalf("Trackers set %v and %v", torrents[0].Tracker, torrents[1].Tracker)
	}
	if got := called(); len(got) != 1 || got[0] != "system.multicall" {
		t.Fatalf("Trackers made calls %v", got)
	}

	if torrents, err = client.List(ListOptions{Trackers: true}); err != nil || torrents[0].Tracker == nil {
		t.Fatalf("List with trackers = %+v, %v", torrents, err)
	}
	if got := called(); len(got) != 2 {
		t.Fatalf("List with trackers made calls %v", got)
	}

	if err := client.Trackers(Torrents{torrents[0], nil}); err == nil {
		t.Fatal("Trackers accepted a nil torrent")
	}
	if err := client.Trackers(Torrents{{Name: "no hash"}}); err == nil {
		t.Fatal("Trackers accepted a torrent without a hash")
	}
	if got := called(); len(got) != 0 {
		t.Fatalf("invalid Trackers calls reached rTorrent: %v", got)
	}
}

func TestHashesAsksOnlyForHashes(t *testing.T) {
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		var params []string
		for _, param := range call.Params {
			params = append(params, *param.Value.String)
		}
		if call.MethodName != "d.multicall2" || strings.Join(params, " ") != " main d.hash=" {
			t.Errorf("unexpected call %s %q", call.MethodName, params)
			return topLevelFault(-1, "unexpected")
		}
		return arrayResponse(result(stringValue(testHash)), result(stringValue(strings.Repeat("B", 40))))
	})
	hashes, err := client.Hashes()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(hashes, ",") != testHash+","+strings.Repeat("B", 40) {
		t.Fatalf("Hashes = %v", hashes)
	}

	malformed := testClient(t, func(string, xmlrpcMethodCall) string {
		return arrayResponse("<array><data></data></array>")
	})
	if _, err := malformed.Hashes(); err == nil {
		t.Fatal("Hashes accepted a row without a hash")
	}
}

func TestListSwitchesToDMulticallWhenRTorrentDropsTheAlias(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		mu.Lock()
		methods = append(methods, call.MethodName)
		mu.Unlock()
		if call.MethodName == "d.multicall2" {
			return topLevelFault(noSuchMethod, "method 'd.multicall2' not defined")
		}
		if call.MethodName != "d.multicall" || len(call.Params) < 2 || *call.Params[1].Value.String != "main" {
			t.Errorf("unexpected call %+v", call)
		}
		return arrayResponse(result(stringValue(testHash)))
	})
	for range 2 {
		if hashes, err := client.Hashes(); err != nil || len(hashes) != 1 {
			t.Fatalf("Hashes = %v, %v", hashes, err)
		}
	}
	if got := strings.Join(methods, " "); got != "d.multicall2 d.multicall d.multicall" {
		t.Fatalf("methods = %s; want one d.multicall2 and then only d.multicall", got)
	}

	// Other faults are errors, and do not switch.
	other := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		if call.MethodName != "d.multicall2" {
			t.Errorf("switched to %s after an unrelated fault", call.MethodName)
		}
		return topLevelFault(-503, "permission denied")
	})
	var fault *XMLRPCFault
	if _, err := other.List(ListOptions{}); !errors.As(err, &fault) || fault.Code != -503 {
		t.Fatalf("List = %v, want the -503 fault", err)
	}
	if _, err := other.List(ListOptions{}); err == nil {
		t.Fatal("second List succeeded")
	}
}

// chunkReader returns its data a few bytes at a time.
type chunkReader struct {
	data []byte
	size int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p[:min(len(p), r.size)], r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestReadResponseSizesBufferFromStatedLength(t *testing.T) {
	body := wrapParams("<string>" + strings.Repeat("x", 100_000) + "</string>")
	header := "Status: 200 OK\r\nContent-Type: text/xml\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n"
	scgi := header + body

	for _, test := range []struct {
		name   string
		reader io.Reader
		length int64
		want   string
		// exact means the buffer was allocated once at the stated size.
		exact bool
	}{
		{"SCGI header", strings.NewReader(scgi), -1, scgi, true},
		{"HTTP length", strings.NewReader(body), int64(len(body)), body, true},
		{"no header", strings.NewReader(body), -1, body, false},
		{"header split across reads", &chunkReader{[]byte(scgi), 10}, -1, scgi, false},
		{"understated length", strings.NewReader(body), 10, body, false},
		{"overstated length", strings.NewReader("short"), 1 << 20, "short", false},
		{"lying SCGI header", strings.NewReader("Content-Length: 99999999999\r\n\r\nshort"), -1, "Content-Length: 99999999999\r\n\r\nshort", false},
	} {
		got, err := readResponse(test.reader, 1<<20, test.length)
		if err != nil || !bytes.Equal(got, []byte(test.want)) {
			t.Errorf("%s: readResponse = %d bytes, %v; want %d bytes", test.name, len(got), err, len(test.want))
			continue
		}
		// The allocator rounds large buffers up to whole 8 KiB pages.
		if test.exact && (cap(got) <= len(test.want) || cap(got) > len(test.want)+8192) {
			t.Errorf("%s: buffer capacity %d for %d bytes; want one allocation of the stated size", test.name, cap(got), len(got))
		}
	}

	// The limit still applies when the stated length is larger.
	got, err := readResponse(strings.NewReader(scgi), 100, -1)
	if err != nil || len(got) != 101 || cap(got) > 4096 {
		t.Fatalf("limited readResponse = %d bytes (capacity %d), %v; want 101", len(got), cap(got), err)
	}
	if _, err := decodeMethodResponse(strings.NewReader(scgi), 100); err == nil || !strings.Contains(err.Error(), "exceeds 100 bytes") {
		t.Fatalf("decodeMethodResponse over the limit = %v", err)
	}
}

// Times come from the fields that keep them: when a torrent finished and
// first started, which survive an rTorrent restart, and when it was loaded,
// which does not. Whether it is private comes from its own field too.
func TestListReadsEachTimeFromItsField(t *testing.T) {
	times := map[string]string{
		"d.load_date=":          intValue(12),
		"d.timestamp.finished=": intValue(1700000005),
		"d.timestamp.started=":  intValue(1690000005),
		"d.is_private=":         intValue(1),
	}
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		values := torrentValues("timed", testHash)
		var row strings.Builder
		row.WriteString("<array><data>")
		for i, param := range call.Params[2:] {
			value := values[i]
			if timed, ok := times[*param.Value.String]; ok {
				value = timed
			}
			row.WriteString("<value>" + value + "</value>")
		}
		row.WriteString("</data></array>")
		return arrayResponse(row.String())
	})
	torrents, err := client.List(ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := torrents[0]; got.Age != 12 || got.Finished != 1700000005 || got.Started != 1690000005 || !got.Private {
		t.Fatalf("Age, Finished, Started, Private = %d, %d, %d, %v", got.Age, got.Finished, got.Started, got.Private)
	}
}

// A public torrent reads as one, whatever the fields around d.is_private say.
func TestListReadsPublicTorrents(t *testing.T) {
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		return arrayResponse(torrentRow("public", testHash))
	})
	torrents, err := client.List(ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if torrents[0].Private || torrents[0].Started == 0 || !torrents[0].MultiFile {
		t.Fatalf("Private, Started, MultiFile = %v, %d, %v; want false, set, and true", torrents[0].Private, torrents[0].Started, torrents[0].MultiFile)
	}
}

// Transfers asks for each torrent's own totals, which a fake that answers
// fields by name checks.
func TestTransfersReadEachTotalFromItsField(t *testing.T) {
	hashes := []string{testHash, strings.Repeat("B", 40)}
	fields := map[string]func(i int) string{
		"d.hash=":       func(i int) string { return stringValue(hashes[i]) },
		"d.up.total=":   func(i int) string { return intValue(uint64(100 + i)) },
		"d.down.total=": func(i int) string { return intValue(uint64(1000 + i)) },
	}
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		var rows []string
		for i := range hashes {
			var row strings.Builder
			row.WriteString("<array><data>")
			for _, param := range call.Params[2:] {
				field, ok := fields[*param.Value.String]
				if !ok {
					t.Errorf("asked for %s", *param.Value.String)
					return topLevelFault(-506, "no such method")
				}
				row.WriteString("<value>" + field(i) + "</value>")
			}
			row.WriteString("</data></array>")
			rows = append(rows, row.String())
		}
		return arrayResponse(rows...)
	})
	transfers, err := client.Transfers()
	if err != nil {
		t.Fatal(err)
	}
	want := []Transfer{{testHash, 100, 1000}, {strings.Repeat("B", 40), 101, 1001}}
	if !slices.Equal(transfers, want) {
		t.Fatalf("Transfers = %+v, want %+v", transfers, want)
	}

	short := testClient(t, func(string, xmlrpcMethodCall) string {
		return arrayResponse("<array><data><value>" + stringValue(testHash) + "</value></data></array>")
	})
	if _, err := short.Transfers(); err == nil || !strings.Contains(err.Error(), "expected 3 fields") {
		t.Fatalf("a short row gave %v", err)
	}
}

// Connections nests p.multicall in the torrent list, which the fake checks by
// name, and counts each torrent's peers by p.is_incoming.
func TestConnectionsCountIncomingAndOutgoingPeers(t *testing.T) {
	// row is one torrent's row: its one field lists a [flag] per peer.
	row := func(flags ...uint64) string {
		var peers strings.Builder
		for _, flag := range flags {
			peers.WriteString("<value>" + result(intValue(flag)) + "</value>")
		}
		return "<array><data><value><array><data>" + peers.String() + "</data></array></value></data></array>"
	}
	rows := []string{row(1, 0, 1), row(), row(0)}
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		if fields := call.Params[2:]; len(fields) != 1 || *fields[0].Value.String != "p.multicall=,p.is_incoming=" {
			t.Errorf("asked for %d fields, the first %q", len(fields), *fields[0].Value.String)
		}
		return arrayResponse(rows...)
	})
	if count, err := client.Connections(); err != nil || count != (Connections{Incoming: 2, Outgoing: 2}) {
		t.Fatalf("Connections = %+v, %v", count, err)
	}
	rows = []string{"<array><data><value>" + intValue(1) + "</value></data></array>"}
	if _, err := client.Connections(); err == nil {
		t.Fatal("a row without peers was accepted")
	}
}

func TestFreeDiskSpacesAskForEachTorrentInOneRequest(t *testing.T) {
	requests := 0
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		requests++
		var results []string
		for i, nested := range nestedCalls(call) {
			if nested.method != "d.free_diskspace" || len(nested.params) != 1 {
				t.Errorf("call %d = %+v", i, nested)
			}
			results = append(results, result(intValue(uint64(i+1)<<30)))
		}
		return arrayResponse(results...)
	})
	free, err := client.FreeDiskSpaces(testHash, strings.Repeat("B", 40))
	if err != nil || !slices.Equal(free, []uint64{1 << 30, 2 << 30}) || requests != 1 {
		t.Fatalf("FreeDiskSpaces = %v, %v after %d requests", free, err, requests)
	}
	if free, err := client.FreeDiskSpaces(); err != nil || free != nil || requests != 1 {
		t.Fatalf("no hashes gave %v, %v after %d requests", free, err, requests)
	}
	if _, err := client.FreeDiskSpaces(" "); err == nil {
		t.Fatal("an empty hash was accepted")
	}
}
