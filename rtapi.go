package rtapi

// Written for 'pyed/rtelegram'.

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
)

const (
	Leeching = "Leeching"
	Seeding  = "Seeding"
	Complete = "Complete"
	Stopped  = "Stopped"
	Hashing  = "Hashing"
	Error    = "Error"
)

// Torrent represents a single torrent.
type Torrent struct {
	Name      string
	Hash      string
	DownRate  uint64
	UpRate    uint64
	Size      uint64
	Completed uint64
	Percent   string
	ETA       uint64
	Ratio     float64
	Age       uint64
	AgeLoad   uint64
	UpTotal   uint64
	State     string
	Message   string
	Tracker   *url.URL
	Path      string
}

// Torrents is a slice of *Torrent.
type Torrents []*Torrent

// Rtorrent holds the network and address e.g.'tcp|localhost:5000' or 'unix|path/to/socket'.
type Rtorrent struct {
	network, address, Version string
}

// NewRtorrent takes the address, defined in .rtorrent.rc
func NewRtorrent(address string) (*Rtorrent, error) {
	network := "tcp"

	if _, err := os.Stat(address); err == nil {
		network = "unix"
	}

	rt := &Rtorrent{network: network, address: address}

	ver, err := rt.getVersion()
	if err != nil {
		return nil, err
	}

	rt.Version = ver
	return rt, nil
}

// Torrents returns a slice that contains all the torrents.
func (r *Rtorrent) Torrents() (Torrents, error) {
	data := encode(torrentsXML)
	conn, err := r.send(data)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	torrents := make(Torrents, 0)

	// Fuck XML. http://foaas.com/XML/Everyone
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == startTAG {
			torrent := new(Torrent)

			scanner.Scan()
			txt := scanner.Text()
			torrent.Name = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			torrent.Hash = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			torrent.DownRate = pUint(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.UpRate = pUint(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			dSizeChunks := pUint(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			dChunkSize := pUint(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			dCompletedChunks := pUint(txt[11 : len(txt)-13])

			torrent.Size = dSizeChunks * dChunkSize
			torrent.Completed = dCompletedChunks * dChunkSize

			torrent.Percent, torrent.ETA = calcPercentAndETA(torrent.Size, torrent.Completed, torrent.DownRate)

			scanner.Scan()
			txt = scanner.Text()
			ratio := pUint(txt[11 : len(txt)-13])
			torrent.Ratio = round(float64(ratio)/1000, 2)

			torrent.UpTotal = uint64(round(float64(torrent.Completed)*(float64(ratio)/1000), 1))

			scanner.Scan()
			txt = scanner.Text()
			torrent.Age = pUint(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.AgeLoad = pUint(txt[11 : len(txt)-13])

			scanner.Scan()
			txt = scanner.Text()
			torrent.Message = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			torrent.Path = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			dIsActive := txt[11 : len(txt)-13]

			scanner.Scan()
			txt = scanner.Text()
			dConnectionCurrent := txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			dComplete := txt[11 : len(txt)-13]

			scanner.Scan()
			txt = scanner.Text()
			dHashing := txt[11 : len(txt)-13]

			// figure out the State
			switch {
			case dIsActive == "1" && len(torrent.Message) != 0:
				torrent.State = Error
			case dHashing != "0":
				torrent.State = Hashing
			case dIsActive == "1" && dComplete == "1":
				torrent.State = Seeding
			case dIsActive == "1" && dConnectionCurrent == "leech":
				torrent.State = Leeching
			case dComplete == "1":
				torrent.State = Complete
			default: // dIsActive == "0"
				torrent.State = Stopped
			}

			torrents = append(torrents, torrent)
		}
	}

	// set the Tracker field
	r.getTrackers(torrents)

	if CurrentSorting != DefaultSorting { // torrents are already sorted by ID
		torrents.Sort(CurrentSorting)
	}
	return torrents, nil
}

// GetTorrent takes a hash and returns *Torrent
func (r *Rtorrent) GetTorrent(hash string) (*Torrent, error) {
	torrents, err := r.Torrents()
	if err != nil {
		return nil, err
	}

	for i := range torrents {
		if torrents[i].Hash == hash {
			return torrents[i], nil
		}
	}
	return nil, fmt.Errorf("Error: No torrent with hash: %s", hash)
}

// Download takes URL to a .torrent file to start downloading it.
func (r *Rtorrent) Download(url string) error {
	data := encode(fmt.Sprintf(downloadXML, url))
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Stop takes a *Torrent or more to 'd.stop' it/them.
func (r *Rtorrent) Stop(ts ...*Torrent) error {
	header, body := xmlCon("d.stop")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := range ts {
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
func (r *Rtorrent) Start(ts ...*Torrent) error {
	header, body := xmlCon("d.start")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := range ts {
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
func (r *Rtorrent) Check(ts ...*Torrent) error {
	header, body := xmlCon("d.check_hash")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := range ts {
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

// Delete takes *Torrent or more to 'd.erase' it/them, if withData is true, local data will get deleted too.
func (r *Rtorrent) Delete(withData bool, ts ...*Torrent) error {
	header, body := xmlCon("d.erase")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := range ts {
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

	if withData {
		for i := range ts {
			if e := os.RemoveAll(ts[i].Path); e != nil {
				err = e
			}
		}
	}

	return err
}

// Speeds returns current Down/Up rates.
func (r *Rtorrent) Speeds() (down, up uint64) {
	data := encode(speedsXML)
	conn, err := r.send(data)
	if err != nil {
		down, up = 0, 0
		return
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == startTAG {
			scanner.Scan()
			txt := scanner.Text()
			down = pUint(txt[11 : len(txt)-13])

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			up = pUint(txt[11 : len(txt)-13])
			return
		}
	}
	return
}

type stats struct {
	ThrottleUp, ThrottleDown, TotalUp, TotalDown uint64
	Port                                         string
}

// Stats returns *stats filled with the proper info.
func (r *Rtorrent) Stats() (*stats, error) {
	st := new(stats)
	data := encode(statsXML)
	conn, err := r.send(data)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == startTAG {
			scanner.Scan()
			txt := scanner.Text()
			st.ThrottleUp = pUint(txt[11 : len(txt)-13])

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			st.ThrottleDown = pUint(txt[11 : len(txt)-13])

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			st.TotalUp = pUint(txt[11 : len(txt)-13])

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			st.TotalDown = pUint(txt[11 : len(txt)-13])

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			st.Port = txt[11 : len(txt)-13]

		}
	}
	return st, nil
}

// getVersion returns a string represnts rtorrent/libtorrent versions.
func (r *Rtorrent) getVersion() (string, error) {
	data := encode(versionXML)
	conn, err := r.send(data)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	var clientVer, libraryVer string
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == startTAG {
			scanner.Scan()
			txt := scanner.Text()
			clientVer = txt[15 : len(txt)-17]

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			libraryVer = txt[15 : len(txt)-17]

			break

		}
	}
	return fmt.Sprintf("%s/%s", clientVer, libraryVer), nil

}

// getTrackers takes Torrents and fill their tracker fields.
func (r *Rtorrent) getTrackers(ts Torrents) error {
	header, body := xmlCon("t.url")

	xml := new(bytes.Buffer)
	xml.WriteString(header)

	for i := range ts {
		xml.WriteString(ts[i].Hash + ":t0")
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
		if scanner.Text() == startTAG {
			scanner.Scan()
			txt := scanner.Text()
			ts[i].Tracker, _ = url.Parse(txt[15 : len(txt)-17])
			i++
		}
	}

	return nil
}

// calcPercentAndETA takes size, size done, down rate to calculate the percenage + ETA.
func calcPercentAndETA(size, done, downrate uint64) (string, uint64) {
	var ETA uint64
	if size == done {
		return "100%", ETA // Dodge "100.0%"
	}
	percentage := fmt.Sprintf("%.1f%%", float64(done)/float64(size)*100)

	if downrate > 0 {
		ETA = (size - done) / downrate
	}
	return percentage, ETA
}

// send takes scgi formated data and returns net.Conn
func (r *Rtorrent) send(data []byte) (net.Conn, error) {
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

// pUint wraps strconv.ParseUint
func pUint(str string) uint64 {
	u, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return 0
	}
	return u
}

// round function.
func round(v float64, decimals int) float64 {
	var pow float64 = 1
	for i := 0; i < decimals; i++ {
		pow *= 10
	}
	return float64(int((v*pow)+0.5)) / pow
}

// xmlCon takes a method name and constructs a header, body, for that method with 'system.multicall'
func xmlCon(method string) (h string, b string) {
	h = fmt.Sprintf(header, method)
	b = fmt.Sprintf(body, method)
	return
}

// XML constants
const (
	torrentsXML = `<?xml version="1.0" encoding="UTF-8"?>
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
<value><string>d.load_date=</string></value>
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

	downloadXML = `<?xml version="1.0" encoding="UTF-8"?>
<methodCall>
<methodName>load.start</methodName>
<params>
<param>
<value><string></string></value>
</param>
<param>
<value><string>%s</string></value>
</param>
</params>
</methodCall>`

	header = `<?xml version="1.0" encoding="UTF-8"?>
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

	speedsXML = `<?xml version="1.0" encoding="UTF-8"?>
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

	statsXML = `<?xml version="1.0" encoding="UTF-8"?>
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

	versionXML = `<?xml version="1.0" encoding="UTF-8"?>
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

	startTAG = "<value><array><data>"
)
