package rtapi

// Written for 'pyed/rtelegram'.

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"strconv"
)

type state int8

const (
	Started state = iota
	Paused
	Checking
	Error
)

// Torrent represents a single torrent.
type Torrent struct {
	ID        int
	Name      string
	Hash      string
	DownRate  int
	UpRate    int
	DownTotal int
	UpTotal   int
	Size      int
	SizeDone  int
	Percent   string
	Ratio     float64
	State     state
	Message   string
	Tracker   string
	Path      string
}

// Torrents is a slice of *Torrent.
type Torrents []*Torrent

// rtorrent holds the network and address e.g.'tcp|localhost:5000' or 'unix|path/to/socket'.
type rtorrent struct {
	network, address string
}

// Rtorrent takes the address, defined in .rtorrent.rc
func Rtorrent(address string) *rtorrent {
	network := "tcp"

	if _, err := os.Stat(address); err == nil {
		network = "unix"
	}

	return &rtorrent{network, address}
}

// Torrents returns a slice that contains all the torrents.
func (r *rtorrent) Torrents() (Torrents, error) {
	data := encode(torrentsXML)
	conn, err := r.send(data)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	torrents := make(Torrents, 0)

	// Fuck XML. http://foaas.com/XML/Everyone
	scanner := bufio.NewScanner(conn)
	var id int
	for scanner.Scan() {
		if scanner.Text() == "<value><array><data>" {
			torrent := new(Torrent)

			id++
			torrent.ID = id

			scanner.Scan()
			txt := scanner.Text()
			torrent.Name = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			torrent.Hash = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			torrent.DownRate = pInt(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.UpRate = pInt(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.DownTotal = pInt(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.UpTotal = pInt(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.Size = pInt(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.SizeDone = pInt(txt[11 : len(txt)-13])

			torrent.Percent = calcPercent(torrent.Size, torrent.SizeDone)

			scanner.Scan()
			txt = scanner.Text()
			torrent.Ratio = toRatio(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.Message = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			torrent.Path = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			dState := txt[11 : len(txt)-13]

			scanner.Scan()
			txt = scanner.Text()
			dIsActive := txt[11 : len(txt)-13]

			scanner.Scan()
			txt = scanner.Text()
			dIsOpen := txt[11 : len(txt)-13]

			scanner.Scan()
			txt = scanner.Text()
			dIsHashChecking := txt[11 : len(txt)-13]

			// figure out the State
			switch {
			case len(torrent.Message) != 0 && torrent.Message != "Tracker: [Tried all trackers.]":
				torrent.State = Error
			case dIsHashChecking != "0":
				torrent.State = Checking
			case (dState == "0" || dIsActive == "0") && dIsOpen != "0":
				torrent.State = Paused
			default:
				torrent.State = Started
			}

			torrents = append(torrents, torrent)
		}
	}

	// set the Tracker field
	r.getTrackers(torrents)
	return torrents, nil
}

// Download takes URL to a .torrent file to start downloading it.
func (r *rtorrent) Download(url string) error {
	data := encode(fmt.Sprintf(downloadXML, url))
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Stop takes a *Torrent or more to 'd.stop' it/them.
func (r *rtorrent) Stop(ts ...*Torrent) error {
	header, body := xmlCon("d.stop")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := 0; i < len(ts); i++ {
		xml.WriteString(ts[i].Hash)
		if i != len(ts)-1 {
			xml.WriteString(body)
		}
	}
	xml.WriteString(footer)

	data := encode(xml.String())
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Start takes a *Torrent or more to 'd.start' it/them.
func (r *rtorrent) Start(ts ...*Torrent) error {
	header, body := xmlCon("d.start")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := 0; i < len(ts); i++ {
		xml.WriteString(ts[i].Hash)
		if i != len(ts)-1 {
			xml.WriteString(body)
		}
	}
	xml.WriteString(footer)

	data := encode(xml.String())
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Check takes a *Torrent or more to 'd.check_hash' it/them.
func (r *rtorrent) Check(ts ...*Torrent) error {
	header, body := xmlCon("d.check_hash")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := 0; i < len(ts); i++ {
		xml.WriteString(ts[i].Hash)
		if i != len(ts)-1 {
			xml.WriteString(body)
		}
	}
	xml.WriteString(footer)

	data := encode(xml.String())
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Delete takes a *Torrent or more to 'd.erase' it/them.
func (r *rtorrent) Delete(ts ...*Torrent) error {
	header, body := xmlCon("d.erase")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := 0; i < len(ts); i++ {
		xml.WriteString(ts[i].Hash)
		if i != len(ts)-1 {
			xml.WriteString(body)
		}
	}
	xml.WriteString(footer)

	data := encode(xml.String())
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Speeds returns current Down/Up rates.
func (r *rtorrent) Speeds() (down, up int) {
	data := encode(speedsXML)
	conn, err := r.send(data)
	if err != nil {
		down, up = -1, -1
		return
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == "<value><array><data>" {
			scanner.Scan()
			txt := scanner.Text()
			down = pInt(txt[11 : len(txt)-13])

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			up = pInt(txt[11 : len(txt)-13])
			return
		}
	}
	return
}

// Version returns a string represnts rtorrent/libtorrent versions.
func (r *rtorrent) Version() string {
	data := encode(versionXML)
	conn, err := r.send(data)
	if err != nil {
		return "-1/-1"
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == "<value><array><data>" {
			scanner.Scan()
			txt := scanner.Text()
			clientVer := txt[15 : len(txt)-17]

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			libraryVer := txt[15 : len(txt)-17]

			return fmt.Sprintf("%s/%s", clientVer, libraryVer)
		}
	}

	return "-1/-1"
}

// getTrackers takes Torrents and fill their tracker fields.
func (r *rtorrent) getTrackers(ts Torrents) error {
	header, body := xmlCon("t.get_url")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := 0; i < len(ts); i++ {
		xml.WriteString(ts[i].Hash)
		if i != len(ts)-1 {
			xml.WriteString(body)
		}
	}
	xml.WriteString(footer)

	data := encode(xml.String())
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for i := 0; scanner.Scan(); {
		if scanner.Text() == "<value><array><data>" {
			scanner.Scan()
			txt := scanner.Text()
			ts[i].Tracker = txt[15 : len(txt)-17]
			i++
		}
	}

	return nil
}

// calcPercent takes the size and the size done to calculate the percenage.
func calcPercent(size, done int) string {
	if size == done {
		return "100%" // Dodge "100.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(done)/float64(size)*100)
}

// send takes scgi formated data and returns net.Conn
func (r *rtorrent) send(data []byte) (net.Conn, error) {
	conn, err := net.Dial(r.network, r.address)
	if err != nil {
		return nil, err
	}

	_, err = conn.Write(data)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// encode puts the data in scgi format.
func encode(data string) []byte {
	headers := fmt.Sprintf("CONTENT_LENGTH%c%d%cSCGI%c1%c", 0, len(data), 0, 0, 0)
	headers = fmt.Sprintf("%d:%s,", len(headers), headers)
	return []byte(headers + data)

}

// pInt wraps strconv.Atoi
func pInt(str string) int {
	i, err := strconv.Atoi(str)
	if err != nil {
		return -1
	}
	return i
}

// round function, used by toRatio.
func round(v float64, decimals int) float64 {
	var pow float64 = 1
	for i := 0; i < decimals; i++ {
		pow *= 10
	}
	return float64(int((v*pow)+0.5)) / pow
}

// toRatio takes care of setting the ratio value.
func toRatio(ratio string) float64 {
	f, err := strconv.ParseFloat(ratio, 64)
	if err != nil {
		return -1.0
	}

	return round(f/1000, 2)
}

// xmlCon takes a method name and constructs a header, body, for that method with 'system.multicall'
func xmlCon(method string) (h string, b string) {
	h = fmt.Sprintf(header, method)
	b = fmt.Sprintf(body, method)
	return
}

// XML constants
const (
	torrentsXML = `<?xml version='1.0'?>
<methodCall>
<methodName>d.multicall</methodName>
<params>
<param>
<value><string>main</string></value>
</param>
<param>
<value><string>d.get_name=</string></value>
</param>
<param>
<value><string>d.get_hash=</string></value>
</param>
<param>
<value><string>d.get_down_rate=</string></value>
</param>
<param>
<value><string>d.get_up_rate=</string></value>
</param>
<param>
<value><string>d.get_down_total=</string></value>
</param>
<param>
<value><string>d.get_up_total=</string></value>
</param>
<param>
<value><string>d.get_size_bytes=</string></value>
</param>
<param>
<value><string>d.get_bytes_done=</string></value>
</param>
<param>
<value><string>d.get_ratio=</string></value>
</param>
<param>
<value><string>d.get_message=</string></value>
</param>
<param>
<value><string>d.get_base_path=</string></value>
</param>
<param>
<value><string>d.get_state=</string></value>
</param>
<param>
<value><string>d.is_active=</string></value>
</param>
<param>
<value><string>d.is_open=</string></value>
</param>
<param>
<value><string>d.is_hash_checking=</string></value>
</param>
</params>
</methodCall>`

	downloadXML = `<?xml version='1.0'?>
<methodCall>
<methodName>load_start</methodName>
<params>
<param>
<value><string>%s</string></value>
</param>
</params>
</methodCall>`

	header = `<?xml version='1.0'?>
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
<string>%s</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>`

	body = `</string>
</value>
<value>
<i4>0</i4>
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
<string>%s</string>
</value>
</member>
<member>
<name>params</name>
<value>
<array>
<data>
<value>
<string>`

	footer = `</string>
</value>
<value>
<i4>0</i4>
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

	speedsXML = `<?xml version='1.0'?>
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
<string>get_down_rate</string>
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
<string>get_up_rate</string>
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

	versionXML = `<?xml version='1.0'?>
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
)
