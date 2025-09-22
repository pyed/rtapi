package rtapi

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	testAddress       = ":5262"
	testDownloadURL   = "http://releases.ubuntu.com/17.04/ubuntu-17.04-desktop-amd64.iso.torrent"
	testDownloadDir   = "/home/Downloads"
	testDownloadLabel = "Software"
)

var (
	downloadReq            = mustBuildDownloadRequest(testDownloadURL)
	downloadWithOptionsReq = mustBuildDownloadWithOptionsRequest(testDownloadURL, testDownloadDir, testDownloadLabel)
	torrentsReq            = mustBuildTorrentsRequest()
	speedsReq              = mustBuildSpeedsRequest()
	statsReq               = mustBuildStatsRequest()
	versionReq             = mustBuildVersionRequest()
)

func mustBuildDownloadRequest(url string) string {
	req, err := buildDownloadRequest(url)
	if err != nil {
		panic(err)
	}
	return req
}

func mustBuildDownloadWithOptionsRequest(link, dir, label string) string {
	req, err := buildDownloadWithOptionsRequest(link, dir, label)
	if err != nil {
		panic(err)
	}
	return req
}

func mustBuildTorrentsRequest() string {
	req, err := buildTorrentsRequest()
	if err != nil {
		panic(err)
	}
	return req
}

func mustBuildSystemMulticallRequest(method string, params ...string) string {
	req, err := buildSystemMulticallRequest(method, params...)
	if err != nil {
		panic(err)
	}
	return req
}

func mustBuildSpeedsRequest() string {
	req, err := buildSpeedsRequest()
	if err != nil {
		panic(err)
	}
	return req
}

func mustBuildStatsRequest() string {
	req, err := buildStatsRequest()
	if err != nil {
		panic(err)
	}
	return req
}

func mustBuildVersionRequest() string {
	req, err := buildVersionRequest()
	if err != nil {
		panic(err)
	}
	return req
}

func TestMain(m *testing.M) {
	listener, err := net.Listen("tcp", testAddress)
	if err != nil {
		log.Fatal(err)
	}

	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Fatal(err)
			}

			go handleRequest(conn)
		}
	}()
	os.Exit(m.Run())
}

var rt *Rtorrent

func TestRtorrent(t *testing.T) {
	var err error
	rt, err = NewRtorrent(testAddress)
	if err != nil {
		log.Fatal(err)
	}
}

var tr0, _ = url.Parse("http://torrent.debian.com:6969/announce")
var tr1, _ = url.Parse("http://torrent.ubuntu.com:6969/announce")
var tr2, _ = url.Parse("udp://tracker.archlinux.org:6969")

var testCases = Torrents{
	&Torrent{
		Name:      "debian-mac-8.7.1-amd64-netinst.iso",
		Hash:      "1C60CBECF4C632EDC7AB546623454B33A295CCEA",
		DownRate:  0,
		UpRate:    0,
		Size:      996 * 262144,
		Completed: 792 * 262144,
		Percent:   "79.5%",
		ETA:       0,
		Ratio:     1.29,
		UpTotal:   267827281,
		State:     Error,
		Age:       1492000001,
		Message:   `Tracker: [Failure reason "Requested download is .......... difficult to install. --linus."]`,
		Tracker:   tr0,
		Path:      "/Users/abdulelah/rtorrent/download/debian-mac-8.7.1-amd64-netinst.iso",
	},
	&Torrent{
		Name:      "ubuntu-17.04-server-amd64.iso",
		Hash:      "8856B93099408AE0EBB8CD7BC7BDB9A7F80AD648",
		DownRate:  0,
		UpRate:    0,
		Size:      1370 * 524288,
		Completed: 1370 * 524288,
		Percent:   "100%",
		ETA:       0,
		Ratio:     0,
		UpTotal:   0,
		State:     Seeding,
		Age:       1492032019,
		Message:   "",
		Tracker:   tr1,
		Path:      "/Users/abdulelah/rtorrent/download/ubuntu-17.04-server-amd64.iso",
	},
	&Torrent{
		Name:      "archlinux-2017.04.01-x86_64.iso",
		Hash:      "02CA77A6A047FD37F04337437D18F82E61861084",
		DownRate:  997035,
		UpRate:    0,
		Size:      956 * 524288,
		Completed: 115 * 524288,
		Percent:   "12.0%",
		ETA:       442,
		Ratio:     0,
		UpTotal:   0,
		State:     Leeching,
		Age:       1492031149,
		Message:   "",
		Tracker:   tr2,
		Path:      "/Users/abdulelah/rtorrent/download/archlinux-2017.04.01-x86_64.iso",
	},
}

var (
	stopReq     = mustBuildSystemMulticallRequest("d.stop", testCases[0].Hash)
	startReq    = mustBuildSystemMulticallRequest("d.start", testCases[0].Hash)
	checkReq    = mustBuildSystemMulticallRequest("d.check_hash", testCases[0].Hash)
	deleteReq   = mustBuildSystemMulticallRequest("d.erase", testCases[0].Hash)
	trackersReq = mustBuildSystemMulticallRequest(
		"t.url",
		testCases[0].Hash+":t0",
		testCases[1].Hash+":t0",
		testCases[2].Hash+":t0",
	)
)

func TestBuildTorrentsRequest(t *testing.T) {
	req, err := buildTorrentsRequest()
	if err != nil {
		t.Fatalf("buildTorrentsRequest() returned error: %v", err)
	}

	if !strings.HasPrefix(req, xml.Header) {
		t.Fatalf("expected XML header prefix in %q", req)
	}

	var call xmlrpcMethodCall
	body := strings.TrimPrefix(req, xml.Header)
	if err := xml.Unmarshal([]byte(body), &call); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if call.MethodName != "d.multicall2" {
		t.Fatalf("expected method name d.multicall2, got %s", call.MethodName)
	}

	expectedFields := []string{
		"",
		"main",
		"d.name=",
		"d.hash=",
		"d.down.rate=",
		"d.up.rate=",
		"d.size_chunks=",
		"d.chunk_size=",
		"d.completed_chunks=",
		"d.ratio=",
		"d.load_date=",
		"d.message=",
		"d.base_path=",
		"d.is_active=",
		"d.connection_current=",
		"d.complete=",
		"d.hashing=",
		"d.custom1=",
	}

	if len(call.Params) != len(expectedFields) {
		t.Fatalf("expected %d params, got %d", len(expectedFields), len(call.Params))
	}

	for i, param := range call.Params {
		if param.Value.String == nil {
			t.Fatalf("unexpected nil string param at index %d", i)
		}
		if *param.Value.String != expectedFields[i] {
			t.Fatalf("unexpected param %d: expected %q, got %q", i, expectedFields[i], *param.Value.String)
		}
	}
}

func TestBuildDownloadRequest(t *testing.T) {
	req, err := buildDownloadRequest(testDownloadURL)
	if err != nil {
		t.Fatalf("buildDownloadRequest() returned error: %v", err)
	}

	if !strings.HasPrefix(req, xml.Header) {
		t.Fatalf("expected XML header prefix in %q", req)
	}

	var call xmlrpcMethodCall
	body := strings.TrimPrefix(req, xml.Header)
	if err := xml.Unmarshal([]byte(body), &call); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if call.MethodName != "load.start" {
		t.Fatalf("expected method name load.start, got %s", call.MethodName)
	}

	expectedValues := []string{"", testDownloadURL}
	if len(call.Params) != len(expectedValues) {
		t.Fatalf("expected %d params, got %d", len(expectedValues), len(call.Params))
	}

	for i, param := range call.Params {
		if param.Value.String == nil {
			t.Fatalf("unexpected nil string param at index %d", i)
		}
		if *param.Value.String != expectedValues[i] {
			t.Fatalf("unexpected param %d: expected %q, got %q", i, expectedValues[i], *param.Value.String)
		}
	}
}

func TestBuildDownloadWithOptionsRequest(t *testing.T) {
	req, err := buildDownloadWithOptionsRequest(testDownloadURL, testDownloadDir, testDownloadLabel)
	if err != nil {
		t.Fatalf("buildDownloadWithOptionsRequest() returned error: %v", err)
	}

	if !strings.HasPrefix(req, xml.Header) {
		t.Fatalf("expected XML header prefix in %q", req)
	}

	var call xmlrpcMethodCall
	body := strings.TrimPrefix(req, xml.Header)
	if err := xml.Unmarshal([]byte(body), &call); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if call.MethodName != "system.multicall" {
		t.Fatalf("expected method name system.multicall, got %s", call.MethodName)
	}

	if len(call.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(call.Params))
	}

	array := call.Params[0].Value.Array
	if array == nil {
		t.Fatalf("expected array value in first param")
	}

	if len(array.Values) != 1 {
		t.Fatalf("expected array with 1 value, got %d", len(array.Values))
	}

	callStruct := array.Values[0].Struct
	if callStruct == nil {
		t.Fatalf("expected struct value in call array entry")
	}

	if len(callStruct.Members) != 2 {
		t.Fatalf("expected 2 struct members, got %d", len(callStruct.Members))
	}

	methodMember := callStruct.Members[0]
	if methodMember.Name != "methodName" {
		t.Fatalf("expected first member name methodName, got %s", methodMember.Name)
	}
	if methodMember.Value.String == nil || *methodMember.Value.String != "load.start" {
		t.Fatalf("expected methodName value load.start, got %#v", methodMember.Value.String)
	}

	paramsMember := callStruct.Members[1]
	if paramsMember.Name != "params" {
		t.Fatalf("expected second member name params, got %s", paramsMember.Name)
	}

	paramsArray := paramsMember.Value.Array
	if paramsArray == nil {
		t.Fatalf("expected params member to contain array value")
	}

	expectedValues := []string{
		"",
		testDownloadURL,
		fmt.Sprintf("d.directory.set=\"%s\"", testDownloadDir),
		fmt.Sprintf("d.custom1.set=%s", testDownloadLabel),
	}

	if len(paramsArray.Values) != len(expectedValues) {
		t.Fatalf("expected %d params values, got %d", len(expectedValues), len(paramsArray.Values))
	}

	for i, value := range paramsArray.Values {
		if value.String == nil {
			t.Fatalf("unexpected nil string value at index %d", i)
		}
		if *value.String != expectedValues[i] {
			t.Fatalf("unexpected params value %d: expected %q, got %q", i, expectedValues[i], *value.String)
		}
	}
}

func TestBuildSpeedsRequest(t *testing.T) {
	req, err := buildSpeedsRequest()
	if err != nil {
		t.Fatalf("buildSpeedsRequest() returned error: %v", err)
	}

	if !strings.HasPrefix(req, xml.Header) {
		t.Fatalf("expected XML header prefix in %q", req)
	}

	var call xmlrpcMethodCall
	body := strings.TrimPrefix(req, xml.Header)
	if err := xml.Unmarshal([]byte(body), &call); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if call.MethodName != "system.multicall" {
		t.Fatalf("expected method name system.multicall, got %s", call.MethodName)
	}

	if len(call.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(call.Params))
	}

	array := call.Params[0].Value.Array
	if array == nil {
		t.Fatalf("expected array value in first param")
	}

	expected := []struct {
		method string
		params []string
	}{
		{"throttle.global_down.rate", []string{""}},
		{"throttle.global_up.rate", []string{""}},
	}

	if len(array.Values) != len(expected) {
		t.Fatalf("expected %d method calls, got %d", len(expected), len(array.Values))
	}

	for i, value := range array.Values {
		callStruct := value.Struct
		if callStruct == nil {
			t.Fatalf("expected struct value at index %d", i)
		}

		if len(callStruct.Members) != 2 {
			t.Fatalf("expected 2 struct members at index %d, got %d", i, len(callStruct.Members))
		}

		methodMember := callStruct.Members[0]
		if methodMember.Name != "methodName" {
			t.Fatalf("expected methodName member at index %d, got %s", i, methodMember.Name)
		}
		if methodMember.Value.String == nil || *methodMember.Value.String != expected[i].method {
			t.Fatalf("unexpected method name at index %d: got %#v", i, methodMember.Value.String)
		}

		paramsMember := callStruct.Members[1]
		if paramsMember.Name != "params" {
			t.Fatalf("expected params member at index %d, got %s", i, paramsMember.Name)
		}

		paramsArray := paramsMember.Value.Array
		if paramsArray == nil {
			t.Fatalf("expected params array at index %d", i)
		}

		if len(paramsArray.Values) != len(expected[i].params) {
			t.Fatalf("expected %d params at index %d, got %d", len(expected[i].params), i, len(paramsArray.Values))
		}

		for j, paramValue := range paramsArray.Values {
			if paramValue.String == nil {
				t.Fatalf("expected string param at index %d for call %d", j, i)
			}
			if *paramValue.String != expected[i].params[j] {
				t.Fatalf("unexpected param %d for call %d: expected %q, got %q", j, i, expected[i].params[j], *paramValue.String)
			}
		}
	}
}

func TestBuildStatsRequest(t *testing.T) {
	req, err := buildStatsRequest()
	if err != nil {
		t.Fatalf("buildStatsRequest() returned error: %v", err)
	}

	if !strings.HasPrefix(req, xml.Header) {
		t.Fatalf("expected XML header prefix in %q", req)
	}

	var call xmlrpcMethodCall
	body := strings.TrimPrefix(req, xml.Header)
	if err := xml.Unmarshal([]byte(body), &call); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if call.MethodName != "system.multicall" {
		t.Fatalf("expected method name system.multicall, got %s", call.MethodName)
	}

	if len(call.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(call.Params))
	}

	array := call.Params[0].Value.Array
	if array == nil {
		t.Fatalf("expected array value in first param")
	}

	expected := []struct {
		method string
		params []string
	}{
		{"throttle.up.max", []string{"", ""}},
		{"throttle.down.max", []string{"", ""}},
		{"throttle.global_up.total", nil},
		{"throttle.global_down.total", nil},
		{"network.listen.port", nil},
		{"directory.default", nil},
	}

	if len(array.Values) != len(expected) {
		t.Fatalf("expected %d method calls, got %d", len(expected), len(array.Values))
	}

	for i, value := range array.Values {
		callStruct := value.Struct
		if callStruct == nil {
			t.Fatalf("expected struct value at index %d", i)
		}

		if len(callStruct.Members) != 2 {
			t.Fatalf("expected 2 struct members at index %d, got %d", i, len(callStruct.Members))
		}

		methodMember := callStruct.Members[0]
		if methodMember.Name != "methodName" {
			t.Fatalf("expected methodName member at index %d, got %s", i, methodMember.Name)
		}
		if methodMember.Value.String == nil || *methodMember.Value.String != expected[i].method {
			t.Fatalf("unexpected method name at index %d: got %#v", i, methodMember.Value.String)
		}

		paramsMember := callStruct.Members[1]
		if paramsMember.Name != "params" {
			t.Fatalf("expected params member at index %d, got %s", i, paramsMember.Name)
		}

		paramsArray := paramsMember.Value.Array
		if paramsArray == nil {
			t.Fatalf("expected params array at index %d", i)
		}

		expectedParams := expected[i].params
		if len(paramsArray.Values) != len(expectedParams) {
			t.Fatalf("expected %d params at index %d, got %d", len(expectedParams), i, len(paramsArray.Values))
		}

		for j, paramValue := range paramsArray.Values {
			if paramValue.String == nil {
				t.Fatalf("expected string param at index %d for call %d", j, i)
			}
			if *paramValue.String != expectedParams[j] {
				t.Fatalf("unexpected param %d for call %d: expected %q, got %q", j, i, expectedParams[j], *paramValue.String)
			}
		}
	}
}

func TestBuildVersionRequest(t *testing.T) {
	req, err := buildVersionRequest()
	if err != nil {
		t.Fatalf("buildVersionRequest() returned error: %v", err)
	}

	if !strings.HasPrefix(req, xml.Header) {
		t.Fatalf("expected XML header prefix in %q", req)
	}

	var call xmlrpcMethodCall
	body := strings.TrimPrefix(req, xml.Header)
	if err := xml.Unmarshal([]byte(body), &call); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if call.MethodName != "system.multicall" {
		t.Fatalf("expected method name system.multicall, got %s", call.MethodName)
	}

	if len(call.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(call.Params))
	}

	array := call.Params[0].Value.Array
	if array == nil {
		t.Fatalf("expected array value in first param")
	}

	expected := []struct {
		method string
		params []string
	}{
		{"system.client_version", []string{""}},
		{"system.library_version", []string{""}},
	}

	if len(array.Values) != len(expected) {
		t.Fatalf("expected %d method calls, got %d", len(expected), len(array.Values))
	}

	for i, value := range array.Values {
		callStruct := value.Struct
		if callStruct == nil {
			t.Fatalf("expected struct value at index %d", i)
		}

		if len(callStruct.Members) != 2 {
			t.Fatalf("expected 2 struct members at index %d, got %d", i, len(callStruct.Members))
		}

		methodMember := callStruct.Members[0]
		if methodMember.Name != "methodName" {
			t.Fatalf("expected methodName member at index %d, got %s", i, methodMember.Name)
		}
		if methodMember.Value.String == nil || *methodMember.Value.String != expected[i].method {
			t.Fatalf("unexpected method name at index %d: got %#v", i, methodMember.Value.String)
		}

		paramsMember := callStruct.Members[1]
		if paramsMember.Name != "params" {
			t.Fatalf("expected params member at index %d, got %s", i, paramsMember.Name)
		}

		paramsArray := paramsMember.Value.Array
		if paramsArray == nil {
			t.Fatalf("expected params array at index %d", i)
		}

		if len(paramsArray.Values) != len(expected[i].params) {
			t.Fatalf("expected %d params at index %d, got %d", len(expected[i].params), i, len(paramsArray.Values))
		}

		for j, paramValue := range paramsArray.Values {
			if paramValue.String == nil {
				t.Fatalf("expected string param at index %d for call %d", j, i)
			}
			if *paramValue.String != expected[i].params[j] {
				t.Fatalf("unexpected param %d for call %d: expected %q, got %q", j, i, expected[i].params[j], *paramValue.String)
			}
		}
	}
}

func TestTorrents(t *testing.T) {
	torrents, err := rt.Torrents()
	if err != nil {
		t.Fatal(err)
	}

	if len(torrents) != len(testCases) {
		t.Fatalf("Expected %d torrents, got: %d", len(testCases), len(torrents))
	}

	for i := range torrents {
		if !match(torrents[i], testCases[i]) {
			t.Errorf("Expected torrents[%d] and testCases[%d] to be equal, got: \n%v\n%v", i, i, torrents[i], testCases[i])
		}
	}
}

func TestGetTorrent(t *testing.T) {
	arch, err := rt.GetTorrent("02CA77A6A047FD37F04337437D18F82E61861084")
	if err != nil {
		t.Fatal(err)
	}

	if !match(arch, testCases[2]) {
		t.Errorf("Expected 'arch' to match 'testCases[2]', got:\narch:%v\ntestCases[2]:%v", *arch, *testCases[2])
	}
}

func TestDownload(t *testing.T) {
	if err := rt.Download(testDownloadURL); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadWithOptions(t *testing.T) {
	opts := &DotTorrentWithOptions{
		Link:  testDownloadURL,
		Label: testDownloadLabel,
	}

	if err := rt.DownloadWithOptions(opts); err != nil {
		t.Fatal(err)
	}
}

func TestStop(t *testing.T) {
	rt.Stop(testCases[0])
}

func TestStart(t *testing.T) {
	rt.Start(testCases[0])
}

func TestCheck(t *testing.T) {
	rt.Check(testCases[0])
}

func TestDelete(t *testing.T) {
	rt.Delete(false, testCases[0])
}

func TestSpeeds(t *testing.T) {
	var expectedDown uint64 = 336650
	var expectedUp uint64 = 593

	down, up := rt.Speeds()

	if down != expectedDown {
		t.Errorf("Expected down speed of %d, got: %d", expectedDown, down)
	}

	if up != expectedUp {
		t.Errorf("Expected up speed of %d, got: %d", expectedUp, up)
	}
}

func TestStats(t *testing.T) {
	st, err := rt.Stats()
	if err != nil {
		t.Errorf("Expected no error, got: %s", err)
	}

	stTestCase := &stats{
		ThrottleUp:   0,
		ThrottleDown: 0,
		TotalUp:      6841,
		TotalDown:    7476,
		Port:         "6980",
		Directory:    "/home/Downloads",
	}

	if *st != *stTestCase {
		t.Errorf("Expected:\n%#v, got:\n%#v", stTestCase, st)
	}

}

func TestVersion(t *testing.T) {
	expectedVersion := "0.9.6/0.13.6"
	if rt.Version != expectedVersion {
		t.Errorf("Expected Version to be %s, got: %s", expectedVersion, rt.Version)
	}
}

func TestCalcPercentAndETA(t *testing.T) {
	testCases := []struct {
		size, done, downRate uint64
		expectedPercent      string
		expectedETA          uint64
	}{
		{100, 100, 0, "100%", 0},
		{100, 50, 23, "50.0%", 2},
		{100, 0, 1, "0.0%", 100},

		{2345, 23, 200, "1.0%", 11},
		{23463453, 1, 60000, "0.0%", 391},
		{5234, 2234, 999, "42.7%", 3},
		{1, 0, 0, "0.0%", 0},
		{0, 0, 0, "100%", 0},
		{10000, 9999, 50, "99.9%", 0},
		{100, 150, 10, "100%", 0},
		{0, 10, 0, "100%", 0},
	}

	for i, test := range testCases {
		percent, ETA := calcPercentAndETA(test.size, test.done, test.downRate)
		if percent != test.expectedPercent {
			t.Errorf("Case %d: Expected %s to be the percent of (%d, %d): got %s",
				i, test.expectedPercent, test.size, test.done, percent)
		}
		if ETA != test.expectedETA {
			t.Errorf("Case %d: Expected %d to be the ETA, got: %d",
				i, test.expectedETA, ETA)
		}
	}
}

func TestEncode(t *testing.T) {
	input := "Hello World!"
	expectedOut := []byte{50, 53, 58, 67, 79, 78, 84, 69, 78, 84, 95, 76, 69, 78,
		71, 84, 72, 0, 49, 50, 0, 83, 67, 71, 73, 0, 49, 0, 44,
		72, 101, 108, 108, 111, 32, 87, 111, 114, 108, 100, 33}

	actualOut := encode(input)

	if bytes.Compare(actualOut, expectedOut) != 0 {
		t.Errorf("Expected:\n%v\ngot:\n%v", expectedOut, actualOut)
	}
}

func handleRequest(conn net.Conn) {
	defer conn.Close()
	length := getContentLen(conn)
	buf := make([]byte, length)
	conn.Read(buf)

	req := string(buf)

	switch {
	case req == torrentsReq:
		if _, err := conn.Write([]byte(torrentsResp)); err != nil {
			log.Fatal(err)
		}
	case req == trackersReq:
		if _, err := conn.Write([]byte(trackersResp)); err != nil {
			log.Fatal(err)
		}
	case req == downloadReq:
	case req == downloadWithOptionsReq:
	case req == stopReq:
	case req == startReq:
	case req == checkReq:
	case req == deleteReq:
	case req == speedsReq:
		if _, err := conn.Write([]byte(speedsResp)); err != nil {
			log.Fatal(err)
		}
	case req == statsReq:
		if _, err := conn.Write([]byte(statsResp)); err != nil {
			log.Fatal(err)
		}
	case req == versionReq:
		if _, err := conn.Write([]byte(versionResp)); err != nil {
			log.Fatal(err)
		}

	default:
		log.Print("Unkown request:")
		log.Fatal(req)
	}
}

func getContentLen(reader io.Reader) int {
	buf := new(bytes.Buffer)
	for {
		s := make([]byte, 1)
		if _, err := reader.Read(s); err != nil {
			log.Fatal(err)
		}
		if string(s) == "," {
			break
		}
		buf.WriteByte(s[0])
	}
	i, err := strconv.Atoi(buf.String()[18 : buf.Len()-8])
	if err != nil {
		log.Fatal(err)
	}
	return i
}

func BenchmarkTorrents(b *testing.B) {
	rt, err := NewRtorrent(testAddress)
	if err != nil {
		log.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		rt.Torrents()
	}
}

func match(a, b *Torrent) bool {
	if a.Name != b.Name ||
		a.Hash != b.Hash ||
		a.DownRate != b.DownRate ||
		a.UpRate != b.UpRate ||
		a.Size != b.Size ||
		a.Completed != b.Completed ||
		a.Percent != b.Percent ||
		a.ETA != b.ETA ||
		a.Ratio != b.Ratio ||
		a.Age != b.Age ||
		a.UpTotal != b.UpTotal ||
		a.State != b.State ||
		a.Message != b.Message ||
		*a.Tracker != *b.Tracker ||
		a.Path != b.Path {
		return false
	}
	return true
}

const (
	torrentsResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 2203

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><string>debian-mac-8.7.1-amd64-netinst.iso</string></value>
<value><string>1C60CBECF4C632EDC7AB546623454B33A295CCEA</string></value>
<value><i8>0</i8></value>
<value><i8>0</i8></value>
<value><i8>996</i8></value>
<value><i8>262144</i8></value>
<value><i8>792</i8></value>
<value><i8>1290</i8></value>
<value><i8>1492000001</i8></value>
<value><string>Tracker: [Failure reason "Requested download is .......... difficult to install. --linus."]</string></value>
<value><string>/Users/abdulelah/rtorrent/download/debian-mac-8.7.1-amd64-netinst.iso</string></value>
<value><i8>1</i8></value>
<value><string>leech</string></value>
<value><i8>0</i8></value>
<value><i8>0</i8></value>
<value><string></string></value>
</data></array></value>
<value><array><data>
<value><string>ubuntu-17.04-server-amd64.iso</string></value>
<value><string>8856B93099408AE0EBB8CD7BC7BDB9A7F80AD648</string></value>
<value><i8>0</i8></value>
<value><i8>0</i8></value>
<value><i8>1370</i8></value>
<value><i8>524288</i8></value>
<value><i8>1370</i8></value>
<value><i8>0</i8></value>
<value><i8>1492032019</i8></value>
<value><string></string></value>
<value><string>/Users/abdulelah/rtorrent/download/ubuntu-17.04-server-amd64.iso</string></value>
<value><i8>1</i8></value>
<value><string>seed</string></value>
<value><i8>1</i8></value>
<value><i8>0</i8></value>
<value><string></string></value>
</data></array></value>
<value><array><data>
<value><string>archlinux-2017.04.01-x86_64.iso</string></value>
<value><string>02CA77A6A047FD37F04337437D18F82E61861084</string></value>
<value><i8>997035</i8></value>
<value><i8>0</i8></value>
<value><i8>956</i8></value>
<value><i8>524288</i8></value>
<value><i8>115</i8></value>
<value><i8>0</i8></value>
<value><i8>1492031149</i8></value>
<value><string></string></value>
<value><string>/Users/abdulelah/rtorrent/download/archlinux-2017.04.01-x86_64.iso</string></value>
<value><i8>1</i8></value>
<value><string>leech</string></value>
<value><i8>0</i8></value>
<value><i8>0</i8></value>
<value><string></string></value>
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`

	trackersResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 513

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><string>http://torrent.debian.com:6969/announce</string></value>
</data></array></value>
<value><array><data>
<value><string>http://torrent.ubuntu.com:6969/announce</string></value>
</data></array></value>
<value><array><data>
<value><string>udp://tracker.archlinux.org:6969</string></value>
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`

	speedsResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 315

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><i8>336650</i8></value>
</data></array></value>
<value><array><data>
<value><i8>593</i8></value>
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`

	statsResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 635

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><i8>0</i8></value>
</data></array></value>
<value><array><data>
<value><i8>0</i8></value>
</data></array></value>
<value><array><data>
<value><i8>6841</i8></value>
</data></array></value>
<value><array><data>
<value><i8>7476</i8></value>
</data></array></value>
<value><array><data>
<value><i8>6980</i8></value>
</data></array></value>
<value><array><data>
<value><string>/home/Downloads</string></value>
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`

	versionResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 333

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><string>0.9.6</string></value>
</data></array></value>
<value><array><data>
<value><string>0.13.6</string></value>
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`
)
