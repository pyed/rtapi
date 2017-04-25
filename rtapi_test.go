package rtapi

import (
	"bytes"
	"log"
	"net"
	"os"
	"strings"
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

var rt *rtorrent

func TestRtorrent(t *testing.T) {
	rt = Rtorrent(testAddress)
}

func TestTorrents(t *testing.T) {
	torrents, err := rt.Torrents()
	if err != nil {
		t.Fatal(err)
	}

	testCases := Torrents{
		&Torrent{
			ID:        1,
			Name:      "debian-mac-8.7.1-amd64-netinst.iso",
			Hash:      "1C60CBECF4C632EDC7AB546623454B33A295CCEA",
			DownRate:  0,
			UpRate:    0,
			Size:      996 * 262144,
			Completed: 792 * 262144,
			Percent:   "79.5%",
			ETA:       0,
			Ratio:     0,
			UpTotal:   0,
			State:     Error,
			Message:   `Tracker: [Failure reason "Requested download is .......... difficult to install. --linus."]`,
			Tracker:   "http://torrent.debian.com:6969/announce",
			Path:      "/Users/abdulelah/rtorrent/download/debian-mac-8.7.1-amd64-netinst.iso",
		},
		&Torrent{
			ID:        2,
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
			Message:   "",
			Tracker:   "http://torrent.ubuntu.com:6969/announce",
			Path:      "/Users/abdulelah/rtorrent/download/ubuntu-17.04-server-amd64.iso",
		},
		&Torrent{
			ID:        3,
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
			Message:   "",
			Tracker:   "udp://tracker.archlinux.org:6969",
			Path:      "/Users/abdulelah/rtorrent/download/archlinux-2017.04.01-x86_64.iso",
		},
	}

	if len(torrents) != len(testCases) {
		t.Fatalf("Expected %d torrents, got: %d", len(testCases), len(torrents))
	}

	for i := range torrents {
		if *torrents[i] != *testCases[i] {
			t.Errorf("Expected torrents[%d] and testCases[%d] to be equal, got: \n%v\n%v", i, i, torrents[i], testCases[i])
		}
	}
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

func TestVersion(t *testing.T) {
	expectedVersion := "0.9.6/0.13.6"

	version := rt.Version()

	if version != expectedVersion {
		t.Errorf("Expected the version to be %s, got: %s", expectedVersion, version)
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

	buf := make([]byte, 256)
	conn.Read(buf)

	req := string(buf)

	switch {
	case strings.Contains(req, "main"): // Torrents()
		if _, err := conn.Write([]byte(torrentsResp)); err != nil {
			log.Fatal(err)
		}
	case strings.Contains(req, "t.url"): // getTrackers()
		if _, err := conn.Write([]byte(trackersResp)); err != nil {
			log.Fatal(err)
		}
	case strings.Contains(req, "throttle.global"): // Speeds()
		if _, err := conn.Write([]byte(speedsResp)); err != nil {
			log.Fatal(err)
		}
	case strings.Contains(req, "system.client_version"): // Version()
		if _, err := conn.Write([]byte(versionResp)); err != nil {
			log.Fatal(err)
		}

	default:
		log.Print("Unkown request:")
		log.Fatal(req)
	}
}

func BenchmarkTorrents(b *testing.B) {
	rt := Rtorrent(testAddress)
	for i := 0; i < b.N; i++ {
		rt.Torrents()
	}
}

const (
	torrentsResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 2092

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
<value><i8>0</i8></value>
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
