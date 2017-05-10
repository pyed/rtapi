package rtapi

import (
	"bytes"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"
)

const testAddress = ":5262"

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
	rt.Download("http://releases.ubuntu.com/17.04/ubuntu-17.04-desktop-amd64.iso.torrent")
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
		ThrottleUp:   2048,
		ThrottleDown: 3072,
		TotalUp:      49023,
		TotalDown:    10938487,
		Port:         "6980",
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
	torrentsReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>d.multicall2</methodName>
<params>
<param>
<value><string></string></value>
</param>
<param>
<value><string>main</string></value>
</param>
<param>
<value><string>d.name=</string></value>
</param>
<param>
<value><string>d.hash=</string></value>
</param>
<param>
<value><string>d.down.rate=</string></value>
</param>
<param>
<value><string>d.up.rate=</string></value>
</param>
<param>
<value><string>d.size_chunks=</string></value>
</param>
<param>
<value><string>d.chunk_size=</string></value>
</param>
<param>
<value><string>d.completed_chunks=</string></value>
</param>
<param>
<value><string>d.ratio=</string></value>
</param>
<param>
<value><string>d.creation_date=</string></value>
</param>
<param>
<value><string>d.message=</string></value>
</param>
<param>
<value><string>d.base_path=</string></value>
</param>
<param>
<value><string>d.is_active=</string></value>
</param>
<param>
<value><string>d.connection_current=</string></value>
</param>
<param>
<value><string>d.complete=</string></value>
</param>
<param>
<value><string>d.hashing=</string></value>
</param>
</params>
</methodCall>`

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
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`

	downloadReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>load.start</methodName>
<params>
<param>
<value><string></string></value>
</param>
<param>
<value><string>http://releases.ubuntu.com/17.04/ubuntu-17.04-desktop-amd64.iso.torrent</string></value>
</param>
</params>
</methodCall>`

	stopReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>system.multicall</methodName>
<params>
<param>
<value>
<array>
<data>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>d.stop</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>1C60CBECF4C632EDC7AB546623454B33A295CCEA</string>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
</data>
</array>
</value>
</param>
</params>
</methodCall>`

	startReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>system.multicall</methodName>
<params>
<param>
<value>
<array>
<data>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>d.start</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>1C60CBECF4C632EDC7AB546623454B33A295CCEA</string>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
</data>
</array>
</value>
</param>
</params>
</methodCall>`

	checkReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>system.multicall</methodName>
<params>
<param>
<value>
<array>
<data>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>d.check_hash</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>1C60CBECF4C632EDC7AB546623454B33A295CCEA</string>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
</data>
</array>
</value>
</param>
</params>
</methodCall>`

	deleteReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>system.multicall</methodName>
<params>
<param>
<value>
<array>
<data>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>d.erase</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>1C60CBECF4C632EDC7AB546623454B33A295CCEA</string>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
</data>
</array>
</value>
</param>
</params>
</methodCall>`

	trackersReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>system.multicall</methodName>
<params>
<param>
<value>
<array>
<data>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>t.url</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>1C60CBECF4C632EDC7AB546623454B33A295CCEA:t0</string>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>t.url</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>8856B93099408AE0EBB8CD7BC7BDB9A7F80AD648:t0</string>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>t.url</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>02CA77A6A047FD37F04337437D18F82E61861084:t0</string>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
</data>
</array>
</value>
</param>
</params>
</methodCall>`

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

	speedsReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>system.multicall</methodName>
<params>
<param>
<value>
<array>
<data>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>throttle.global_down.rate</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string/>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>throttle.global_up.rate</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string/>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
</data>
</array>
</value>
</param>
</params>
</methodCall>`

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

	statsReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>system.multicall</methodName>
<params>
<param>
<value>
<array>
<data>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>throttle.up.max</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string/>
</value>
<value>
<string/>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>throttle.down.max</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string/>
</value>
<value>
<string/>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>throttle.global_up.total</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
</data>
</array>
</value>
</member>
</struct>
</value>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>throttle.global_down.total</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
</data>
</array>
</value>
</member>
</struct>
</value>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>network.listen.port</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
</data>
</array>
</value>
</member>
</struct>
</value>
</data>
</array>
</value>
</param>
</params>
</methodCall>`

	statsResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 550

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><i8>2048</i8></value>
</data></array></value>
<value><array><data>
<value><i8>3072</i8></value>
</data></array></value>
<value><array><data>
<value><i8>49023</i8></value>
</data></array></value>
<value><array><data>
<value><i8>10938487</i8></value>
</data></array></value>
<value><array><data>
<value><i8>6980</i8></value>
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`

	versionReq = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>system.multicall</methodName>
<params>
<param>
<value>
<array>
<data>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>system.client_version</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string/>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>system.library_version</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string/>
</value>
</data>
</array>
</value>
</member>
</struct>
</value>
</data>
</array>
</value>
</param>
</params>
</methodCall>`

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
