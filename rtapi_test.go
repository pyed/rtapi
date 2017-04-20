package rtapi

import (
	"bytes"
	"log"
	"net"
	"os"
	"strings"
	"testing"
)

var rt *rtorrent

func TestRtorrent(t *testing.T) {
	rt = Rtorrent("localhost:5262")
}

func TestTorrents(t *testing.T) {
	torrents, err := rt.Torrents()
	if err != nil {
		t.Fatal(err)
	}

	testCases := Torrents{
		&Torrent{
			ID:        1,
			Name:      "Ubuntu.iso",
			Hash:      "1C0F867862B481278C0D57A8779D3708D3032AEB",
			DownRate:  234,
			UpRate:    1001,
			DownTotal: 0,
			UpTotal:   0,
			Size:      110636640,
			SizeDone:  110636640,
			Percent:   "100%",
			Ratio:     0,
			State:     Started,
			Message:   "",
			Tracker:   "https://please.track.me/announce",
			Path:      "/home/pyed/rtorrent/download/Ubuntu.iso",
		},
		&Torrent{
			ID:        2,
			Name:      "Debian.iso",
			Hash:      "98D4E447467D6DC965023F719258EA740C2DEF45",
			DownRate:  0,
			UpRate:    0,
			DownTotal: 0,
			UpTotal:   13068963447,
			Size:      4286318720,
			SizeDone:  3281318720,
			Percent:   "76.6%",
			Ratio:     3.05,
			State:     Error,
			Message:   `Tracker: [Failure reason "torrent is too hard to install. -- Linus"]`,
			Tracker:   "https://tracker.thetracking.org/announce.php",
			Path:      "/home/pyed/rtorrent/download/Debian.iso",
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
	expectedDown := 72
	expectedUp := 9321

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

func TestCalcPercent(t *testing.T) {
	testCases := []struct {
		size, done      int
		expectedPercent string
	}{
		{100, 100, "100%"},
		{100, 50, "50.0%"},
		{100, 0, "0.0%"},

		{2345, 23, "1.0%"},
		{23463453, 1, "0.0%"},
		{5234, 2234, "42.7%"},
	}

	for i, test := range testCases {
		percent := calcPercent(test.size, test.done)
		if percent != test.expectedPercent {
			t.Errorf("Case %d: Expected %s to be the percent of (%d, %d): got %s",
				i, test.expectedPercent, test.size, test.done, percent)
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

func TestToRatio(t *testing.T) {
	testCases := []struct {
		input    string
		expected float64
	}{
		{"0", 0},
		{"3004", 3.00},
		{"9292", 9.29},
		{"11", 0.01},
	}

	for i, test := range testCases {
		ratio := toRatio(test.input)
		if ratio != test.expected {
			t.Errorf("Case %d: Expected %.2f out of %s, got: %.2f",
				i, test.expected, test.input, ratio)
		}
	}
}

func TestMain(m *testing.M) {
	listener, err := net.Listen("tcp", "localhost:5262")
	if err != nil {
		log.Print("Maybe 5262 port in use ?")
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
	case strings.Contains(req, "t.get_url"): // getTrackers()
		if _, err := conn.Write([]byte(trackersResp)); err != nil {
			log.Fatal(err)
		}
	case strings.Contains(req, "get_down_rate"): // Speeds()
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

const (
	torrentsResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 1373

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><string>Ubuntu.iso</string></value>
<value><string>1C0F867862B481278C0D57A8779D3708D3032AEB</string></value>
<value><i8>234</i8></value>
<value><i8>1001</i8></value>
<value><i8>0</i8></value>
<value><i8>0</i8></value>
<value><i8>110636640</i8></value>
<value><i8>110636640</i8></value>
<value><i8>0</i8></value>
<value><string></string></value>
<value><string>/home/pyed/rtorrent/download/Ubuntu.iso</string></value>
<value><i8>1</i8></value>
<value><i8>1</i8></value>
<value><i8>1</i8></value>
<value><i8>0</i8></value>
</data></array></value>
<value><array><data>
<value><string>Debian.iso</string></value>
<value><string>98D4E447467D6DC965023F719258EA740C2DEF45</string></value>
<value><i8>0</i8></value>
<value><i8>0</i8></value>
<value><i8>0</i8></value>
<value><i8>13068963447</i8></value>
<value><i8>4286318720</i8></value>
<value><i8>3281318720</i8></value>
<value><i8>3048</i8></value>
<value><string>Tracker: [Failure reason "torrent is too hard to install. -- Linus"]</string></value>
<value><string>/home/pyed/rtorrent/download/Debian.iso</string></value>
<value><i8>1</i8></value>
<value><i8>1</i8></value>
<value><i8>1</i8></value>
<value><i8>0</i8></value>
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`

	trackersResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 419

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><string>https://please.track.me/announce</string></value>
</data></array></value>
<value><array><data>
<value><string>https://tracker.thetracking.org/announce.php</string></value>
</data></array></value>
</data></array></value></param>
</params>
</methodResponse>`

	speedsResp = `Status: 200 OK
Content-Type: text/xml
Content-Length: 312

<?xml version="1.0" encoding="UTF-8"?>
<methodResponse>
<params>
<param><value><array><data>
<value><array><data>
<value><i8>72</i8></value>
</data></array></value>
<value><array><data>
<value><i8>9321</i8></value>
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
