package rtapi

// Written for 'pyed/rtelegram'.

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	UpTotal   uint64
	State     string
	Message   string
	Tracker   *url.URL
	Path      string
	Label     string // ruTorrent lables
}

// Torrents is a slice of *Torrent.
type Torrents []*Torrent

// DotTorrentWithOptions is used when adding .torrent file with options.        ;
// the options get passed via the "Caption" when sending a file via telegram ;
// telegram, e.g d=/dir/to/downloads l=Software, will save the added torrent ;
// torrent to the specified direcotry, and will assigne the label "Software" ;
// to it, labels are saved to "d.custom1", which is used by ruTorrent.       ;
type DotTorrentWithOptions struct {
	Link  string
	Name  string
	Dir   string
	Label string
}

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

	body, err := readXMLBody(conn)
	if err != nil {
		return nil, err
	}
	rows, err := parseMulticall(body)
	if err != nil {
		return nil, err
	}

	torrents := make(Torrents, 0, len(rows))
	for _, row := range rows {
		if len(row) != 16 {
			return nil, fmt.Errorf("rtapi: expected 16 torrent fields, got %d", len(row))
		}

		name, err := row[0].StringValue()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent name: %w", err)
		}
		hash, err := row[1].StringValue()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent hash: %w", err)
		}
		downRate, err := row[2].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent down rate: %w", err)
		}
		upRate, err := row[3].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent up rate: %w", err)
		}
		sizeChunks, err := row[4].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent size chunks: %w", err)
		}
		chunkSize, err := row[5].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent chunk size: %w", err)
		}
		completedChunks, err := row[6].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent completed chunks: %w", err)
		}
		ratioRaw, err := row[7].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent ratio: %w", err)
		}
		age, err := row[8].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent age: %w", err)
		}
		message, err := row[9].StringValue()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent message: %w", err)
		}
		path, err := row[10].StringValue()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent path: %w", err)
		}
		isActive, err := row[11].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent active flag: %w", err)
		}
		connectionCurrent, err := row[12].StringValue()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent connection state: %w", err)
		}
		complete, err := row[13].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent complete flag: %w", err)
		}
		hashing, err := row[14].Uint64Value()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent hashing flag: %w", err)
		}
		label, err := row[15].StringValue()
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent label: %w", err)
		}

		torrent := &Torrent{
			Name:      html.UnescapeString(name),
			Hash:      hash,
			DownRate:  downRate,
			UpRate:    upRate,
			Size:      sizeChunks * chunkSize,
			Completed: completedChunks * chunkSize,
			Age:       age,
			Message:   message,
			Path:      html.UnescapeString(path),
			Label:     html.UnescapeString(label),
		}

		torrent.Percent, torrent.ETA = calcPercentAndETA(torrent.Size, torrent.Completed, torrent.DownRate)
		torrent.Ratio = round(float64(ratioRaw)/1000, 2)
		torrent.UpTotal = uint64(round(float64(torrent.Completed)*(float64(ratioRaw)/1000), 1))

		switch {
		case isActive == 1 && len(torrent.Message) != 0:
			torrent.State = Error
		case hashing != 0:
			torrent.State = Hashing
		case isActive == 1 && complete == 1:
			torrent.State = Seeding
		case isActive == 1 && connectionCurrent == "leech":
			torrent.State = Leeching
		case complete == 1:
			torrent.State = Complete
		default:
			torrent.State = Stopped
		}

		torrents = append(torrents, torrent)
	}

	if err := r.getTrackers(torrents); err != nil {
		return nil, err
	}

	if CurrentSorting != DefaultSorting {
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

// DownloadWithOptions takes *DotTorrentWithOptions downloading it.
func (r *Rtorrent) DownloadWithOptions(tFile *DotTorrentWithOptions) error {
	// if tFile.Dir is empty, set to default
	if tFile.Dir == "" {
		stats, err := r.Stats()
		if err != nil {
			return err
		}
		tFile.Dir = stats.Directory
	}
	data := encode(fmt.Sprintf(donwloadXMLwithOptions, tFile.Link, tFile.Dir, tFile.Label))
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

	body, err := readXMLBody(conn)
	if err != nil {
		return
	}

	values, err := parseScalarMulticall(body)
	if err != nil {
		return
	}

	if len(values) > 0 {
		if v, err := values[0].Uint64Value(); err == nil {
			down = v
		}
	}
	if len(values) > 1 {
		if v, err := values[1].Uint64Value(); err == nil {
			up = v
		}
	}
	return
}

type stats struct {
	ThrottleUp, ThrottleDown, TotalUp, TotalDown uint64
	Port, Directory                              string
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

	body, err := readXMLBody(conn)
	if err != nil {
		return nil, err
	}

	values, err := parseScalarMulticall(body)
	if err != nil {
		return nil, err
	}

	if len(values) != 6 {
		return nil, fmt.Errorf("rtapi: expected 6 stats values, got %d", len(values))
	}

	if st.ThrottleUp, err = values[0].Uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse throttle up: %w", err)
	}
	if st.ThrottleDown, err = values[1].Uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse throttle down: %w", err)
	}
	if st.TotalUp, err = values[2].Uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse total up: %w", err)
	}
	if st.TotalDown, err = values[3].Uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse total down: %w", err)
	}
	portVal, err := values[4].Uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse port: %w", err)
	}
	st.Port = strconv.FormatUint(portVal, 10)
	if st.Directory, err = values[5].StringValue(); err != nil {
		return nil, fmt.Errorf("rtapi: parse directory: %w", err)
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

	body, err := readXMLBody(conn)
	if err != nil {
		return "", err
	}

	values, err := parseScalarMulticall(body)
	if err != nil {
		return "", err
	}
	if len(values) < 2 {
		return "", fmt.Errorf("rtapi: expected 2 version values, got %d", len(values))
	}

	clientVer, err := values[0].StringValue()
	if err != nil {
		return "", fmt.Errorf("rtapi: parse client version: %w", err)
	}
	libraryVer, err := values[1].StringValue()
	if err != nil {
		return "", fmt.Errorf("rtapi: parse library version: %w", err)
	}

	return fmt.Sprintf("%s/%s", clientVer, libraryVer), nil

}

// getTrackers takes Torrents and fill their tracker fields.
func (r *Rtorrent) getTrackers(ts Torrents) error {
	if len(ts) == 0 {
		return nil
	}

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

	respBody, err := readXMLBody(conn)
	if err != nil {
		return err
	}

	rows, err := parseMulticall(respBody)
	if err != nil {
		return err
	}
	if len(rows) != len(ts) {
		return fmt.Errorf("rtapi: received %d trackers for %d torrents", len(rows), len(ts))
	}

	for i, row := range rows {
		if len(row) == 0 {
			return fmt.Errorf("rtapi: empty tracker response for torrent %d", i)
		}
		trackerStr, err := row[0].StringValue()
		if err != nil {
			return fmt.Errorf("rtapi: parse tracker string: %w", err)
		}
		trackerURL, err := url.Parse(trackerStr)
		if err != nil {
			return fmt.Errorf("rtapi: parse tracker url: %w", err)
		}
		ts[i].Tracker = trackerURL
	}

	return nil
}

func parseMulticall(data []byte) ([][]xmlValue, error) {
	var resp xmlResponse
	if err := xml.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("rtapi: decode xml response: %w", err)
	}
	if len(resp.Params) == 0 {
		return nil, fmt.Errorf("rtapi: xmlrpc response missing params")
	}

	outer, err := resp.Params[0].Value.ArrayValues()
	if err != nil {
		return nil, fmt.Errorf("rtapi: expected array response: %w", err)
	}

	rows := make([][]xmlValue, len(outer))
	for i, val := range outer {
		inner, err := val.ArrayValues()
		if err != nil {
			return nil, fmt.Errorf("rtapi: expected array entry at index %d: %w", i, err)
		}
		rows[i] = inner
	}

	return rows, nil
}

func parseScalarMulticall(data []byte) ([]xmlValue, error) {
	rows, err := parseMulticall(data)
	if err != nil {
		return nil, err
	}

	values := make([]xmlValue, len(rows))
	for i, row := range rows {
		if len(row) == 0 {
			return nil, fmt.Errorf("rtapi: empty response row %d", i)
		}
		values[i] = row[0]
	}

	return values, nil
}

func readXMLBody(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)

	for {
		b, err := br.Peek(1)
		if err != nil {
			return nil, err
		}
		switch b[0] {
		case '<':
			return io.ReadAll(br)
		case ' ', '\t', '\r', '\n':
			if _, err := br.ReadByte(); err != nil {
				return nil, err
			}
			continue
		}
		break
	}

	headers := make(map[string]string)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if sep := strings.Index(line, ":"); sep >= 0 {
			key := strings.ToLower(strings.TrimSpace(line[:sep]))
			value := strings.TrimSpace(line[sep+1:])
			headers[key] = value
		}
	}

	if lengthStr, ok := headers["content-length"]; ok {
		length, err := strconv.Atoi(lengthStr)
		if err != nil {
			return nil, fmt.Errorf("rtapi: invalid content length %q: %w", lengthStr, err)
		}
		body := make([]byte, length)
		n, err := io.ReadFull(br, body)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return nil, err
		}
		result := body[:n]
		if n == len(body) {
			rest, readErr := io.ReadAll(br)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return nil, readErr
			}
			if len(rest) > 0 {
				result = append(result, rest...)
			}
		}
		return result, nil
	}

	return io.ReadAll(br)
}

type xmlResponse struct {
	Params []xmlParam `xml:"params>param"`
}

type xmlParam struct {
	Value xmlValue `xml:"value"`
}

type xmlArray struct {
	Values []xmlValue `xml:"data>value"`
}

type xmlValue struct {
	Array   *xmlArray  `xml:"array"`
	String  *xmlScalar `xml:"string"`
	Int     *xmlScalar `xml:"int"`
	I4      *xmlScalar `xml:"i4"`
	I8      *xmlScalar `xml:"i8"`
	Double  *xmlScalar `xml:"double"`
	Boolean *xmlScalar `xml:"boolean"`
}

func (v xmlValue) ArrayValues() ([]xmlValue, error) {
	if v.Array == nil {
		return nil, fmt.Errorf("rtapi: value is not an array")
	}
	return v.Array.Values, nil
}

func (v xmlValue) StringValue() (string, error) {
	if v.String == nil {
		return "", fmt.Errorf("rtapi: value is not a string")
	}
	return v.String.Text(), nil
}

func (v xmlValue) Int64Value() (int64, error) {
	switch {
	case v.I8 != nil:
		return v.I8.Int64Value()
	case v.I4 != nil:
		return v.I4.Int64Value()
	case v.Int != nil:
		return v.Int.Int64Value()
	case v.Boolean != nil:
		b, err := v.Boolean.BoolValue()
		if err != nil {
			return 0, err
		}
		if b {
			return 1, nil
		}
		return 0, nil
	case v.String != nil:
		return v.String.Int64Value()
	}
	return 0, fmt.Errorf("rtapi: value is not an integer")
}

func (v xmlValue) Uint64Value() (uint64, error) {
	i, err := v.Int64Value()
	if err != nil {
		return 0, err
	}
	if i < 0 {
		return 0, fmt.Errorf("rtapi: negative value %d", i)
	}
	return uint64(i), nil
}

type xmlScalar struct {
	Raw string `xml:",chardata"`
}

func (s *xmlScalar) Text() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.Raw)
}

func (s *xmlScalar) Int64Value() (int64, error) {
	if s == nil {
		return 0, fmt.Errorf("rtapi: empty integer value")
	}
	if s.Text() == "" {
		return 0, nil
	}
	return strconv.ParseInt(s.Text(), 10, 64)
}

func (s *xmlScalar) BoolValue() (bool, error) {
	if s == nil {
		return false, fmt.Errorf("rtapi: empty boolean value")
	}
	switch strings.TrimSpace(s.Raw) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, fmt.Errorf("rtapi: invalid boolean %q", s.Raw)
	}
}

// calcPercentAndETA takes size, size done, down rate to calculate the percenage + ETA.
func calcPercentAndETA(size, done, downrate uint64) (string, uint64) {
	if size == 0 || done >= size {
		return "100%", 0
	}

	percentage := float64(done) / float64(size) * 100
	rounded := math.Round(percentage*10) / 10
	if rounded >= 100 {
		rounded = 99.9
	}

	var ETA uint64
	if downrate > 0 {
		ETA = (size - done) / downrate
	}

	return fmt.Sprintf("%.1f%%", rounded), ETA
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
<param>
<value><string>d.custom1=</string></value>
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

	donwloadXMLwithOptions = `<?xml version="1.0" encoding="UTF-8"?>
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
<string>load.start</string>
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
<string>%s</string>
</value>
<value>
<string>d.directory.set="%s"</string>
</value>
<value>
<string>d.custom1.set=%s</string>
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
<value>
<struct>
<member>
<name>methodName</name>
<value>
<string>directory.default</string>
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
)
