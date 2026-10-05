package rtapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testHash = "1C60CBECF4C632EDC7AB546623454B33A295CCEA"

type requestHandler func(string, xmlrpcMethodCall) string

func testClient(t *testing.T, handler requestHandler) *Rtorrent {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var handlers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer conn.Close()
				payload, err := readSCGIRequest(conn)
				if err != nil {
					t.Errorf("read SCGI request: %v", err)
					return
				}
				var call xmlrpcMethodCall
				if err := xml.Unmarshal([]byte(payload), &call); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				if response := handler(payload, call); response != "" {
					if _, err := io.WriteString(conn, response); err != nil {
						t.Errorf("write response: %v", err)
					}
				}
			}()
		}
	}()

	t.Cleanup(func() {
		listener.Close()
		<-done
		handlers.Wait()
	})
	return &Rtorrent{network: "tcp", address: listener.Addr().String(), Timeout: time.Second}
}

func readSCGIRequest(r io.Reader) (string, error) {
	reader := bufio.NewReader(r)
	lengthText, err := reader.ReadString(':')
	if err != nil {
		return "", err
	}
	headerLength, err := strconv.Atoi(strings.TrimSuffix(lengthText, ":"))
	if err != nil {
		return "", err
	}
	headers := make([]byte, headerLength)
	if _, err := io.ReadFull(reader, headers); err != nil {
		return "", err
	}
	comma, err := reader.ReadByte()
	if err != nil || comma != ',' {
		return "", fmt.Errorf("invalid SCGI netstring terminator")
	}

	parts := bytes.Split(headers, []byte{0})
	contentLength := -1
	for i := 0; i+1 < len(parts); i += 2 {
		if string(parts[i]) == "CONTENT_LENGTH" {
			contentLength, err = strconv.Atoi(string(parts[i+1]))
			if err != nil {
				return "", err
			}
			break
		}
	}
	if contentLength < 0 {
		return "", fmt.Errorf("missing CONTENT_LENGTH")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return "", err
	}
	return string(payload), nil
}

func response(value string) string {
	return "Status: 200 OK\r\nContent-Type: text/xml\r\n\r\n" +
		`<?xml version="1.0"?><methodResponse><params><param><value>` + value +
		`</value></param></params></methodResponse>`
}

func arrayResponse(values ...string) string {
	var body strings.Builder
	body.WriteString("<array><data>")
	for _, value := range values {
		body.WriteString("<value>")
		body.WriteString(value)
		body.WriteString("</value>")
	}
	body.WriteString("</data></array>")
	return response(body.String())
}

func result(value string) string {
	return "<array><data><value>" + value + "</value></data></array>"
}

func fault(code int, message string) string {
	return fmt.Sprintf(`<struct>
<member><name>faultCode</name><value><i4>%d</i4></value></member>
<member><name>faultString</name><value><string>%s</string></value></member>
</struct>`, code, message)
}

func topLevelFault(code int, message string) string {
	return fmt.Sprintf(`Status: 200 OK

<methodResponse><fault><value>%s</value></fault></methodResponse>`, fault(code, message))
}

func nestedMethod(call xmlrpcMethodCall) string {
	if len(call.Params) == 0 || call.Params[0].Value.Array == nil || len(call.Params[0].Value.Array.Values) == 0 {
		return ""
	}
	callStruct := call.Params[0].Value.Array.Values[0].Struct
	if callStruct == nil {
		return ""
	}
	for _, member := range callStruct.Members {
		if member.Name == "methodName" && member.Value.String != nil {
			return *member.Value.String
		}
	}
	return ""
}

func intValue(n uint64) string { return fmt.Sprintf("<i8>%d</i8>", n) }
func stringValue(s string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(s))
	return "<string>" + escaped.String() + "</string>"
}

func versionResponse() string {
	return arrayResponse(result(stringValue("0.9.8")), result(stringValue("0.13.8")))
}

func TestNewRtorrentAndVersion(t *testing.T) {
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		if got := nestedMethod(call); got != "system.client_version" {
			t.Errorf("unexpected method: %q", got)
		}
		return versionResponse()
	})

	actual, err := NewRtorrent(client.address)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Version != "0.9.8/0.13.8" || actual.Timeout != DefaultTimeout {
		t.Fatalf("unexpected client: %#v", actual)
	}
	if _, err := NewRtorrent("  "); err == nil {
		t.Fatal("expected empty address error")
	}
}

func TestBuildRequestsUseExactCountersAndEscapedOptions(t *testing.T) {
	req, err := buildTorrentsRequest()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"d.size_bytes=", "d.completed_bytes=", "d.up.total="} {
		if !strings.Contains(req, field) {
			t.Errorf("request missing %q", field)
		}
	}
	for _, obsolete := range []string{"d.size_chunks=", "d.chunk_size=", "d.completed_chunks="} {
		if strings.Contains(req, obsolete) {
			t.Errorf("request still contains %q", obsolete)
		}
	}

	req, err = buildLoadRequest("load.start_verbose", newStringParam("https://example.invalid/a.torrent"), `/tmp/a";bad`, `x";bad`)
	if err != nil {
		t.Fatal(err)
	}
	var call xmlrpcMethodCall
	if err := xml.Unmarshal([]byte(req), &call); err != nil {
		t.Fatal(err)
	}
	if got, want := *call.Params[2].Value.String, "d.directory.set="+strconv.Quote(`/tmp/a";bad`); got != want {
		t.Fatalf("directory command = %q, want %q", got, want)
	}
	if got, want := *call.Params[3].Value.String, "d.custom1.set="+strconv.Quote(`x";bad`); got != want {
		t.Fatalf("label command = %q, want %q", got, want)
	}
}

func TestLoadsChooseMethodAndOnlyGivenCommands(t *testing.T) {
	data := []byte("d4:infod4:name4:testee")
	tests := []struct {
		name   string
		load   func(*Rtorrent) error
		method string
		params int
	}{
		{"Download", func(r *Rtorrent) error { return r.Download("magnet:?xt=urn:btih:" + testHash) }, "load.start_verbose", 2},
		{"options without directory or label", func(r *Rtorrent) error {
			return r.DownloadWithOptions(&DotTorrentWithOptions{Link: "https://example.invalid/a.torrent"})
		}, "load.start_verbose", 2},
		{"stopped with directory and label", func(r *Rtorrent) error {
			return r.DownloadWithOptions(&DotTorrentWithOptions{Link: "https://example.invalid/a.torrent", Dir: "/d", Label: "l", Stopped: true})
		}, "load.verbose", 4},
		{"raw without options", func(r *Rtorrent) error { return r.DownloadRaw(data, nil) }, "load.raw_start", 2},
		{"raw stopped with label", func(r *Rtorrent) error {
			return r.DownloadRaw(data, &DotTorrentWithOptions{Label: "l", Stopped: true})
		}, "load.raw", 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []xmlrpcMethodCall
			client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
				calls = append(calls, call)
				return response(intValue(0))
			})
			if err := test.load(client); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0].MethodName != test.method || len(calls[0].Params) != test.params {
				t.Fatalf("calls = %+v, want one %s with %d params", calls, test.method, test.params)
			}
		})
	}
}

func TestParseTorrentUsesExactCountersAndCeilingETA(t *testing.T) {
	fragment := `<value><array><data>
<value><string>one-byte</string></value><value><string>` + testHash + `</string></value>
<value><i8>2</i8></value><value><i8>0</i8></value>
<value><i8>1</i8></value><value><i8>0</i8></value>
<value><i8>1295</i8></value><value><i8>5000000000</i8></value>
<value><i8>12</i8></value><value>implicit message</value><value><string>/remote/path</string></value>
<value><i8>0</i8></value><value><string>leech</string></value><value><i8>0</i8></value>
<value><i8>0</i8></value><value><string>label</string></value>
<value><string>/remote/dir</string></value><value><i8>1</i8></value>
<value><i8>1700000000</i8></value><value><i8>1690000000</i8></value>
<value><i8>1</i8></value>
</data></array></value>`
	var value xmlrpcValue
	if err := xml.Unmarshal([]byte(fragment), &value); err != nil {
		t.Fatal(err)
	}
	torrent, err := parseTorrent(value)
	if err != nil {
		t.Fatal(err)
	}
	if torrent.Size != 1 || torrent.Completed != 0 || torrent.UpTotal != 5_000_000_000 {
		t.Fatalf("inexact counters: %#v", torrent)
	}
	if torrent.Ratio != 1.3 || torrent.ETA != 1 || torrent.Message != "implicit message" {
		t.Fatalf("unexpected derived values: %#v", torrent)
	}
	if torrent.Directory != "/remote/dir" || !torrent.MultiFile || torrent.Finished != 1700000000 {
		t.Fatalf("unexpected data location: %#v", torrent)
	}
	if torrent.Age != 12 || torrent.Started != 1690000000 || !torrent.Private {
		t.Fatalf("Age = %d, Started = %d, Private = %v; want 12, 1690000000, and true", torrent.Age, torrent.Started, torrent.Private)
	}
}

func TestCalcPercentAndETA(t *testing.T) {
	tests := []struct {
		size, done, rate uint64
		percent          string
		eta              uint64
	}{
		{100, 100, 0, "100%", 0},
		{100, 50, 23, "50.0%", 3},
		{100, 99, 2, "99.0%", 1},
		{0, 0, 0, "100%", 0},
		{100, 150, 10, "100%", 0},
	}
	for _, test := range tests {
		percent, eta := calcPercentAndETA(test.size, test.done, test.rate)
		if percent != test.percent || eta != test.eta {
			t.Errorf("calcPercentAndETA(%d,%d,%d) = %q,%d; want %q,%d",
				test.size, test.done, test.rate, percent, eta, test.percent, test.eta)
		}
	}
}

func TestMutationsAcknowledgeResponsesAndFaults(t *testing.T) {
	torrent := &Torrent{Hash: testHash}
	for _, method := range []string{"d.stop", "d.start", "d.check_hash", "d.erase"} {
		t.Run(method, func(t *testing.T) {
			client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
				if got := nestedMethod(call); got != method {
					t.Errorf("got method %q, want %q", got, method)
				}
				return arrayResponse(result(intValue(0)))
			})
			var err error
			switch method {
			case "d.stop":
				err = client.Stop(torrent)
			case "d.start":
				err = client.Start(torrent)
			case "d.check_hash":
				err = client.Check(torrent)
			case "d.erase":
				err = client.DeleteMetadata(torrent)
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}

	client := testClient(t, func(_ string, _ xmlrpcMethodCall) string {
		return arrayResponse(result(intValue(0)), fault(-501, "second torrent rejected"))
	})
	err := client.Start(torrent, &Torrent{Hash: strings.Repeat("A", 40)})
	var rpcFault *XMLRPCFault
	if !errors.As(err, &rpcFault) || rpcFault.Code != -501 || !strings.Contains(err.Error(), "item 1") {
		t.Fatalf("expected indexed multicall fault, got %v", err)
	}

	truncated := testClient(t, func(_ string, _ xmlrpcMethodCall) string {
		return arrayResponse(result(intValue(0)))
	})
	err = truncated.Start(torrent, &Torrent{Hash: strings.Repeat("A", 40)})
	if err == nil || !strings.Contains(err.Error(), "expected 2 XML-RPC multicall results, got 1") {
		t.Fatalf("expected truncated multicall error, got %v", err)
	}

	client = testClient(t, func(_ string, _ xmlrpcMethodCall) string {
		return topLevelFault(-503, "load rejected")
	})
	err = client.Download("https://example.invalid/a.torrent")
	if !errors.As(err, &rpcFault) || rpcFault.Code != -503 {
		t.Fatalf("expected top-level fault, got %v", err)
	}

	unacknowledged := testClient(t, func(_ string, _ xmlrpcMethodCall) string { return "" })
	if err := unacknowledged.Stop(torrent); err == nil {
		t.Fatal("mutation succeeded without an XML-RPC acknowledgement")
	}
}

func TestMutationValidation(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, func(_ string, _ xmlrpcMethodCall) string {
		requests.Add(1)
		return arrayResponse()
	})

	if err := client.Start(); err != nil {
		t.Fatalf("empty mutation should be a no-op: %v", err)
	}
	if err := client.Stop(nil); err == nil {
		t.Fatal("expected nil torrent error")
	}
	if err := client.Check(&Torrent{}); err == nil {
		t.Fatal("expected empty hash error")
	}
	if err := client.DownloadWithOptions(nil); err == nil {
		t.Fatal("expected nil options error")
	}
	if requests.Load() != 0 {
		t.Fatalf("validation issued %d requests", requests.Load())
	}
}

func TestDownloadOptionsAreImmutable(t *testing.T) {
	options := &DotTorrentWithOptions{Link: "https://example.invalid/a.torrent", Name: "a", Label: "software"}
	original := *options
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		if call.MethodName != "load.start_verbose" {
			t.Errorf("unexpected call: %s", call.MethodName)
		}
		return response(intValue(0))
	})
	if err := client.DownloadWithOptions(options); err != nil {
		t.Fatal(err)
	}
	if *options != original {
		t.Fatalf("options mutated: got %#v, want %#v", options, original)
	}
}

func TestDownloadRawUsesBase64AndNeverSendsLink(t *testing.T) {
	data := []byte("d4:infod4:name4:testee")
	secret := "https://api.telegram.org/botSECRET/getFile"
	options := &DotTorrentWithOptions{Link: secret, Name: "test", Dir: "/downloads", Label: "incoming"}
	original := *options
	client := testClient(t, func(payload string, call xmlrpcMethodCall) string {
		if call.MethodName != "load.raw_start" {
			t.Errorf("unexpected method %q", call.MethodName)
		}
		if strings.Contains(payload, secret) || len(call.Params) != 4 || call.Params[1].Value.Base64 == nil {
			t.Errorf("raw request leaked link or had wrong shape: %s", payload)
		} else if decoded, err := base64.StdEncoding.DecodeString(*call.Params[1].Value.Base64); err != nil || !bytes.Equal(decoded, data) {
			t.Errorf("base64 payload mismatch: %q, %v", decoded, err)
		}
		return response(intValue(0))
	})
	if err := client.DownloadRaw(data, options); err != nil {
		t.Fatal(err)
	}
	if *options != original {
		t.Fatalf("options mutated: got %#v, want %#v", options, original)
	}
	if err := client.DownloadRaw(nil, nil); err == nil {
		t.Fatal("expected empty raw data error")
	}
}

func TestSpeedsWithError(t *testing.T) {
	client := testClient(t, func(_ string, _ xmlrpcMethodCall) string {
		return arrayResponse(result(intValue(336650)), result(intValue(593)))
	})
	down, up, err := client.SpeedsWithError()
	if err != nil || down != 336650 || up != 593 {
		t.Fatalf("SpeedsWithError = %d,%d,%v", down, up, err)
	}

	failing := testClient(t, func(_ string, _ xmlrpcMethodCall) string {
		return topLevelFault(-500, "telemetry unavailable")
	})
	if _, _, err := failing.SpeedsWithError(); err == nil {
		t.Fatal("expected speed error")
	}
}

func TestTrackerlessTorrentRemainsListable(t *testing.T) {
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		switch call.MethodName {
		case "d.multicall2":
			return arrayResponse(torrentRow("tracked", testHash), torrentRow("trackerless", strings.Repeat("B", 40)))
		case "system.multicall":
			return arrayResponse(result(stringValue("udp://tracker.invalid:80")), fault(-501, "Could not find info-hash."))
		default:
			t.Errorf("unexpected method %q", call.MethodName)
			return topLevelFault(-1, "unexpected")
		}
	})

	torrents, err := client.Torrents()
	if err != nil {
		t.Fatal(err)
	}
	if len(torrents) != 2 || torrents[0].Name != "tracked" {
		t.Fatalf("torrent rows changed order or were lost: %#v", torrents)
	}
	if torrents[0].Tracker == nil || torrents[1].Tracker != nil {
		t.Fatalf("unexpected trackers: %#v, %#v", torrents[0].Tracker, torrents[1].Tracker)
	}

	failing := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		if call.MethodName == "d.multicall2" {
			return arrayResponse(torrentRow("one", testHash))
		}
		return arrayResponse(fault(-500, "permission denied"))
	})
	if _, err := failing.Torrents(); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected tracker fault was hidden: %v", err)
	}
}

func TestTrackerMissingRequiresFaultCode(t *testing.T) {
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		if call.MethodName == "d.multicall2" {
			return arrayResponse(torrentRow("one", testHash))
		}
		return arrayResponse(fault(-500, "no tracker"))
	})

	if _, err := client.Torrents(); err == nil || !strings.Contains(err.Error(), "no tracker") {
		t.Fatalf("non--501 tracker fault was hidden: %v", err)
	}
}

// torrentValues are the field values of a complete, stopped torrent, in
// torrentFields order.
func torrentValues(name, hash string) []string {
	return []string{
		stringValue(name), stringValue(hash), intValue(0), intValue(0),
		intValue(1), intValue(1), intValue(0), intValue(0), intValue(1),
		stringValue(""), stringValue("/remote/" + name), intValue(0),
		stringValue("leech"), intValue(1), intValue(0), stringValue(""),
		stringValue("/remote/" + name), intValue(1), intValue(1700000000),
		intValue(1690000000), intValue(0),
	}
}

type nestedCall struct {
	method string
	params []string
}

func nestedCalls(call xmlrpcMethodCall) []nestedCall {
	if call.MethodName != "system.multicall" || len(call.Params) == 0 || call.Params[0].Value.Array == nil {
		return nil
	}
	var calls []nestedCall
	for _, value := range call.Params[0].Value.Array.Values {
		var nested nestedCall
		for _, member := range value.Struct.Members {
			switch member.Name {
			case "methodName":
				nested.method = *member.Value.String
			case "params":
				for _, param := range member.Value.Array.Values {
					if param.I8 != nil {
						nested.params = append(nested.params, strconv.FormatInt(*param.I8, 10))
					} else {
						nested.params = append(nested.params, *param.String)
					}
				}
			}
		}
		calls = append(calls, nested)
	}
	return calls
}

func TestGetTorrentRequestsOnlyThatTorrent(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		requests.Add(1)
		calls := nestedCalls(call)
		if len(calls) != len(torrentFields)+1 {
			t.Errorf("got %d calls, want %d", len(calls), len(torrentFields)+1)
			return topLevelFault(-1, "unexpected")
		}
		values := torrentValues("wanted", testHash)
		results := make([]string, len(calls))
		for i, nested := range calls {
			switch {
			case i < len(torrentFields) && nested.method == torrentFields[i] && nested.params[0] == testHash:
				results[i] = result(values[i])
			case i < len(torrentFields) && nested.params[0] == strings.Repeat("F", 40):
				results[i] = fault(-501, "Could not find info-hash.")
			case i < len(torrentFields) && nested.params[0] == strings.Repeat("E", 40):
				results[i] = fault(-503, "permission denied")
			case i == len(torrentFields) && nested.method == "t.url" && nested.params[0] == testHash+":t0":
				results[i] = result(stringValue("udp://tracker.invalid:80"))
			case i == len(torrentFields):
				results[i] = fault(-501, "Could not find info-hash.")
			default:
				t.Errorf("unexpected call %d: %+v", i, nested)
			}
		}
		return arrayResponse(results...)
	})

	torrent, err := client.GetTorrent(testHash)
	if err != nil {
		t.Fatal(err)
	}
	if torrent.Name != "wanted" || torrent.Hash != testHash || torrent.Tracker == nil || torrent.Tracker.Host != "tracker.invalid:80" {
		t.Fatalf("torrent = %+v", torrent)
	}
	if requests.Load() != 1 {
		t.Fatalf("GetTorrent made %d requests", requests.Load())
	}

	if _, err := client.GetTorrent(strings.Repeat("F", 40)); err == nil || !strings.Contains(err.Error(), "no torrent with hash") {
		t.Fatalf("expected a missing-torrent error, got %v", err)
	}
	var rpcFault *XMLRPCFault
	if _, err := client.GetTorrent(strings.Repeat("E", 40)); !errors.As(err, &rpcFault) || rpcFault.Code != -503 {
		t.Fatalf("expected the field fault, got %v", err)
	}
}

func torrentRow(name, hash string) string {
	fields := torrentValues(name, hash)
	var body strings.Builder
	body.WriteString("<array><data>")
	for _, field := range fields {
		body.WriteString("<value>")
		body.WriteString(field)
		body.WriteString("</value>")
	}
	body.WriteString("</data></array>")
	return body.String()
}

func TestResponseBoundsTimeoutAndImplicitString(t *testing.T) {
	if _, err := decodeMethodResponse(strings.NewReader(strings.Repeat("x", 33)), 32); err == nil || !strings.Contains(err.Error(), "exceeds 32 bytes") {
		t.Fatalf("expected response size error, got %v", err)
	}

	resp, err := decodeMethodResponse(strings.NewReader(response("implicit string")), DefaultMaxResponseSize)
	if err != nil {
		t.Fatal(err)
	}
	value, err := resp.Params[0].Value.stringValue()
	if err != nil || value != "implicit string" {
		t.Fatalf("implicit string = %q, %v", value, err)
	}

	bounded := testClient(t, func(_ string, _ xmlrpcMethodCall) string { return response(intValue(0)) })
	bounded.MaxResponseSize = 32
	if err := bounded.Download("https://example.invalid/a.torrent"); err == nil || !strings.Contains(err.Error(), "exceeds 32 bytes") {
		t.Fatalf("client response limit was not enforced: %v", err)
	}
	bounded.MaxResponseSize = 1 << 20
	if err := bounded.Download("https://example.invalid/a.torrent"); err != nil {
		t.Fatalf("explicit larger response limit was ignored: %v", err)
	}

	client := testClient(t, func(_ string, _ xmlrpcMethodCall) string {
		time.Sleep(100 * time.Millisecond)
		return response(intValue(0))
	})
	client.Timeout = 20 * time.Millisecond
	started := time.Now()
	err = client.Download("https://example.invalid/a.torrent")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("expected bounded timeout, got %v after %s", err, time.Since(started))
	}
}

// httpRTorrent serves XML-RPC over HTTP at /RPC2 for the given credentials,
// answering the version handshake, the torrent list, and trackers.
func httpRTorrent(t *testing.T, user, password string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPassword, ok := r.BasicAuth()
		if !ok || gotUser != user || gotPassword != password {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/RPC2" || r.Header.Get("Content-Type") != "text/xml" {
			t.Errorf("unexpected request: %s %s %q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var call xmlrpcMethodCall
		if err := xml.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Errorf("decode request: %v", err)
		}
		var scgi string
		switch {
		case call.MethodName == "d.multicall2":
			scgi = arrayResponse(torrentRow("over-http", testHash))
		case nestedMethod(call) == "system.client_version":
			scgi = versionResponse()
		default:
			scgi = arrayResponse(result(stringValue("https://tracker.invalid/announce")))
		}
		// HTTP responses carry the XML without SCGI's status lines.
		io.WriteString(w, scgi[strings.Index(scgi, "<?xml"):])
	}))
	t.Cleanup(server.Close)
	return server
}

func TestHTTPTransportPostsXMLRPCWithBasicAuth(t *testing.T) {
	const password = "p@ss:word/1"
	server := httpRTorrent(t, "alice", password)
	address, _ := url.Parse(server.URL + "/RPC2")
	address.User = url.UserPassword("alice", password)

	client, err := NewRtorrent(address.String())
	if err != nil {
		t.Fatal(err)
	}
	if client.Version != "0.9.8/0.13.8" {
		t.Fatalf("version = %q", client.Version)
	}
	torrents, err := client.Torrents()
	if err != nil || len(torrents) != 1 || torrents[0].Name != "over-http" || torrents[0].Tracker == nil {
		t.Fatalf("torrents = %v, %v", torrents, err)
	}

	leaks := func(err error, password string) bool {
		return strings.Contains(err.Error(), password) || strings.Contains(err.Error(), url.QueryEscape(password))
	}
	address.User = url.UserPassword("alice", "wrong-"+password)
	_, err = NewRtorrent(address.String())
	if err == nil || !strings.Contains(err.Error(), "401") || leaks(err, password) {
		t.Fatalf("expected a 401 without the password, got %v", err)
	}

	server.Close()
	address.User = url.UserPassword("alice", password)
	_, err = NewRtorrent(address.String())
	if err == nil || leaks(err, password) {
		t.Fatalf("expected a connection error without the password, got %v", err)
	}
}

func TestAddressSelectsTransport(t *testing.T) {
	for _, address := range []string{"localhost:5000", "127.0.0.1:5000", "rtorrent.invalid:5000"} {
		_, err := NewRtorrentContext(canceledContext(), address)
		if err == nil || !strings.Contains(err.Error(), "dial tcp") {
			t.Errorf("%s: expected an SCGI dial, got %v", address, err)
		}
	}
	if _, err := NewRtorrent("https:///RPC2"); err == nil || !strings.Contains(err.Error(), "no host") {
		t.Fatalf("expected a missing-host error, got %v", err)
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestContextCancellationInterruptsRequests(t *testing.T) {
	release := make(chan struct{})
	var requests atomic.Int32
	client := testClient(t, func(_ string, _ xmlrpcMethodCall) string {
		requests.Add(1)
		<-release
		return ""
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	started := time.Now()
	_, err := client.TorrentsContext(ctx)
	if !errors.Is(err, context.Canceled) || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("expected a prompt cancellation, got %v after %s", err, time.Since(started))
	}

	if err := client.StopContext(ctx, &Torrent{Hash: testHash}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context was ignored: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("a cancelled context still sent requests: %d", got)
	}
}

func TestLargeRawRequestIsWrittenCompletely(t *testing.T) {
	data := bytes.Repeat([]byte("torrent-data"), 100_000)
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		if len(call.Params) < 2 || call.Params[1].Value.Base64 == nil {
			t.Error("missing raw payload")
			return topLevelFault(-1, "missing raw payload")
		}
		decoded, err := base64.StdEncoding.DecodeString(*call.Params[1].Value.Base64)
		if err != nil || !bytes.Equal(decoded, data) {
			t.Errorf("incomplete raw payload: %d bytes, %v", len(decoded), err)
			return topLevelFault(-1, "incomplete raw payload")
		}
		return response(intValue(0))
	})
	if err := client.DownloadRaw(data, nil); err != nil {
		t.Fatal(err)
	}
}

type shortWriter struct {
	bytes.Buffer
	limit int
}

func (w *shortWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit {
		data = data[:w.limit]
	}
	return w.Buffer.Write(data)
}

func TestWriteAllRetriesShortWrites(t *testing.T) {
	writer := &shortWriter{limit: 3}
	data := []byte("complete request")
	written, err := writeAll(writer, data)
	if err != nil || written != len(data) || !bytes.Equal(writer.Bytes(), data) {
		t.Fatalf("writeAll wrote %d bytes (%q): %v", written, writer.Bytes(), err)
	}
}

func TestEncodeUsesByteLength(t *testing.T) {
	request := encode("é")
	payload, err := readSCGIRequest(bytes.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	if payload != "é" {
		t.Fatalf("payload = %q", payload)
	}
}

// Stats asks for each value by name, which a fake that answers by name checks.
func TestStatsReadsEachValueFromItsCall(t *testing.T) {
	answers := map[string]string{
		"throttle.up.max":            intValue(1 << 20),
		"throttle.down.max":          intValue(2 << 20),
		"throttle.global_up.total":   intValue(3 << 30),
		"throttle.global_down.total": intValue(4 << 30),
		"network.listen.port":        intValue(51413),
		"directory.default":          stringValue("/downloads"),
		"system.pid":                 intValue(4242),
	}
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		var results []string
		for _, nested := range nestedCalls(call) {
			answer, ok := answers[nested.method]
			if !ok {
				t.Errorf("asked for %s", nested.method)
				answer = intValue(0)
			}
			results = append(results, result(answer))
		}
		return arrayResponse(results...)
	})
	stats, err := client.Stats()
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{ThrottleUp: 1 << 20, ThrottleDown: 2 << 20, TotalUp: 3 << 30, TotalDown: 4 << 30, Port: "51413", Directory: "/downloads", PID: 4242}
	if *stats != want {
		t.Fatalf("Stats = %+v, want %+v", *stats, want)
	}
}

func TestSetLabelSetsEachTorrentsCustom1(t *testing.T) {
	var requests int
	var calls []nestedCall
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		requests++
		calls = nestedCalls(call)
		results := make([]string, len(calls))
		for i := range results {
			results[i] = result(intValue(0))
		}
		if len(calls) > 2 {
			results[2] = fault(-501, "Could not find info-hash.")
		}
		return arrayResponse(results...)
	})
	other := strings.Repeat("B", 40)
	if err := client.SetLabel("TV%20Shows", &Torrent{Hash: testHash}, &Torrent{Hash: other}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprint([]nestedCall{{"d.custom1.set", []string{testHash, "TV%20Shows"}}, {"d.custom1.set", []string{other, "TV%20Shows"}}})
	if got := fmt.Sprint(calls); got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
	if err := client.SetLabel("x"); err != nil || requests != 1 {
		t.Fatalf("labeling no torrents made %d requests: %v", requests, err)
	}
	if err := client.SetLabel("", &Torrent{Hash: testHash}, &Torrent{Hash: other}, &Torrent{Hash: strings.Repeat("C", 40)}); err == nil {
		t.Fatal("a fault for one torrent was not reported")
	}
}
