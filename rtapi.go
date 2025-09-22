package rtapi

// Written for 'pyed/rtelegram'.

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"html"
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

type xmlrpcMethodCall struct {
	XMLName    xml.Name      `xml:"methodCall"`
	MethodName string        `xml:"methodName"`
	Params     []xmlrpcParam `xml:"params>param"`
}

type xmlrpcParam struct {
	Value xmlrpcValue `xml:"value"`
}

type xmlrpcValue struct {
	String *string       `xml:"string,omitempty"`
	Array  *xmlrpcArray  `xml:"array,omitempty"`
	Struct *xmlrpcStruct `xml:"struct,omitempty"`
}

type xmlrpcArray struct {
	Values []xmlrpcValue `xml:"data>value"`
}

type xmlrpcStruct struct {
	Members []xmlrpcMember `xml:"member"`
}

type xmlrpcMember struct {
	Name  string      `xml:"name"`
	Value xmlrpcValue `xml:"value"`
}

func newStringParam(val string) xmlrpcParam {
	return xmlrpcParam{Value: newStringValue(val)}
}

func newStringValue(val string) xmlrpcValue {
	v := val
	return xmlrpcValue{String: &v}
}

func newArrayValue(values ...xmlrpcValue) xmlrpcValue {
	return xmlrpcValue{Array: &xmlrpcArray{Values: values}}
}

func newStructValue(members ...xmlrpcMember) xmlrpcValue {
	return xmlrpcValue{Struct: &xmlrpcStruct{Members: members}}
}

func newStringMember(name, val string) xmlrpcMember {
	return xmlrpcMember{Name: name, Value: newStringValue(val)}
}

func newArrayMember(name string, values ...xmlrpcValue) xmlrpcMember {
	return xmlrpcMember{Name: name, Value: newArrayValue(values...)}
}

func newMethodCall(method string, params ...string) xmlrpcValue {
	values := make([]xmlrpcValue, 0, len(params))
	for _, param := range params {
		values = append(values, newStringValue(param))
	}

	return newStructValue(
		newStringMember("methodName", method),
		newArrayMember("params", values...),
	)
}

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

func buildTorrentsRequest() (string, error) {
	fields := []string{
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

	params := make([]xmlrpcParam, 0, len(fields))
	for _, field := range fields {
		params = append(params, newStringParam(field))
	}

	request := xmlrpcMethodCall{
		MethodName: "d.multicall2",
		Params:     params,
	}

	return marshalMethodCall(request)
}

func buildDownloadRequest(link string) (string, error) {
	request := xmlrpcMethodCall{
		MethodName: "load.start",
		Params: []xmlrpcParam{
			newStringParam(""),
			newStringParam(link),
		},
	}

	return marshalMethodCall(request)
}

func buildDownloadWithOptionsRequest(link, dir, label string) (string, error) {
	directory := fmt.Sprintf("d.directory.set=\"%s\"", dir)
	customLabel := fmt.Sprintf("d.custom1.set=%s", label)

	request := xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params: []xmlrpcParam{
			{
				Value: newArrayValue(
					newMethodCall("load.start", "", link, directory, customLabel),
				),
			},
		},
	}

	return marshalMethodCall(request)
}

func buildSystemMulticallRequest(method string, params ...string) (string, error) {
	calls := make([]xmlrpcValue, 0, len(params))
	for _, param := range params {
		calls = append(calls, newMethodCall(method, param))
	}

	request := xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params: []xmlrpcParam{
			{
				Value: newArrayValue(calls...),
			},
		},
	}

	return marshalMethodCall(request)
}

func buildSpeedsRequest() (string, error) {
	request := xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params: []xmlrpcParam{
			{
				Value: newArrayValue(
					newMethodCall("throttle.global_down.rate", ""),
					newMethodCall("throttle.global_up.rate", ""),
				),
			},
		},
	}

	return marshalMethodCall(request)
}

func buildStatsRequest() (string, error) {
	request := xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params: []xmlrpcParam{
			{
				Value: newArrayValue(
					newMethodCall("throttle.up.max", "", ""),
					newMethodCall("throttle.down.max", "", ""),
					newMethodCall("throttle.global_up.total"),
					newMethodCall("throttle.global_down.total"),
					newMethodCall("network.listen.port"),
					newMethodCall("directory.default"),
				),
			},
		},
	}

	return marshalMethodCall(request)
}

func buildVersionRequest() (string, error) {
	request := xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params: []xmlrpcParam{
			{
				Value: newArrayValue(
					newMethodCall("system.client_version", ""),
					newMethodCall("system.library_version", ""),
				),
			},
		},
	}

	return marshalMethodCall(request)
}

func marshalMethodCall(request xmlrpcMethodCall) (string, error) {
	payload, err := xml.Marshal(request)
	if err != nil {
		return "", err
	}

	return xml.Header + string(payload), nil
}

func isArrayDataStart(line string) bool {
	return strings.HasPrefix(line, "<value><array><data>")
}

// Torrents returns a slice that contains all the torrents.
func (r *Rtorrent) Torrents() (Torrents, error) {
	req, err := buildTorrentsRequest()
	if err != nil {
		return nil, err
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	torrents := make(Torrents, 0)

	// Fuck XML. http://foaas.com/XML/Everyone
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		if isArrayDataStart(scanner.Text()) {
			torrent := new(Torrent)

			scanner.Scan()
			txt := scanner.Text()
			torrent.Name = html.UnescapeString(txt[15 : len(txt)-17])

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
			torrent.Message = txt[15 : len(txt)-17]

			scanner.Scan()
			txt = scanner.Text()
			torrent.Path = html.UnescapeString(txt[15 : len(txt)-17])

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

			scanner.Scan()
			txt = scanner.Text()
			torrent.Label = txt[15 : len(txt)-17]

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

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// set the Tracker field
	if err := r.getTrackers(torrents); err != nil {
		return nil, err
	}

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
	req, err := buildDownloadRequest(url)
	if err != nil {
		return err
	}

	data := encode(req)
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
	req, err := buildDownloadWithOptionsRequest(tFile.Link, tFile.Dir, tFile.Label)
	if err != nil {
		return err
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Stop takes a *Torrent or more to 'd.stop' it/them.
func (r *Rtorrent) Stop(ts ...*Torrent) error {
	hashes := make([]string, len(ts))
	for i := range ts {
		hashes[i] = ts[i].Hash
	}

	req, err := buildSystemMulticallRequest("d.stop", hashes...)
	if err != nil {
		return err
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Start takes a *Torrent or more to 'd.start' it/them.
func (r *Rtorrent) Start(ts ...*Torrent) error {
	hashes := make([]string, len(ts))
	for i := range ts {
		hashes[i] = ts[i].Hash
	}

	req, err := buildSystemMulticallRequest("d.start", hashes...)
	if err != nil {
		return err
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Check takes a *Torrent or more to 'd.check_hash' it/them.
func (r *Rtorrent) Check(ts ...*Torrent) error {
	hashes := make([]string, len(ts))
	for i := range ts {
		hashes[i] = ts[i].Hash
	}

	req, err := buildSystemMulticallRequest("d.check_hash", hashes...)
	if err != nil {
		return err
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Delete takes *Torrent or more to 'd.erase' it/them, if withData is true, local data will get deleted too.
func (r *Rtorrent) Delete(withData bool, ts ...*Torrent) error {
	hashes := make([]string, len(ts))
	for i := range ts {
		hashes[i] = ts[i].Hash
	}

	req, err := buildSystemMulticallRequest("d.erase", hashes...)
	if err != nil {
		return err
	}

	data := encode(req)
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
	req, err := buildSpeedsRequest()
	if err != nil {
		return 0, 0
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		down, up = 0, 0
		return
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if isArrayDataStart(scanner.Text()) {
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
	if err := scanner.Err(); err != nil {
		down, up = 0, 0
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
	req, err := buildStatsRequest()
	if err != nil {
		return nil, err
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if isArrayDataStart(scanner.Text()) {
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

			scanner.Scan() // </data></array></value>
			scanner.Scan() // <value><array><data>

			scanner.Scan()
			txt = scanner.Text()
			st.Directory = txt[15 : len(txt)-17]

		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return st, nil
}

// getVersion returns a string represnts rtorrent/libtorrent versions.
func (r *Rtorrent) getVersion() (string, error) {
	req, err := buildVersionRequest()
	if err != nil {
		return "", err
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	var clientVer, libraryVer string
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if isArrayDataStart(scanner.Text()) {
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
	if err := scanner.Err(); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/%s", clientVer, libraryVer), nil

}

// getTrackers takes Torrents and fill their tracker fields.
func (r *Rtorrent) getTrackers(ts Torrents) error {
	if len(ts) == 0 {
		return nil
	}

	keys := make([]string, len(ts))
	for i := range ts {
		keys[i] = ts[i].Hash + ":t0"
	}

	req, err := buildSystemMulticallRequest("t.url", keys...)
	if err != nil {
		return err
	}

	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return err
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	i := 0
	for scanner.Scan() {
		if !isArrayDataStart(scanner.Text()) {
			continue
		}
		if i >= len(ts) {
			return fmt.Errorf("rtapi: received more trackers than torrents")
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return fmt.Errorf("rtapi: unexpected tracker response format")
		}
		txt := scanner.Text()
		var trackerStr string
		switch {
		case strings.HasPrefix(txt, "<value><string>") && strings.HasSuffix(txt, "</string></value>"):
			trackerStr = txt[15 : len(txt)-17]
		case txt == "<value><string/></value>":
			trackerStr = ""
		default:
			return fmt.Errorf("rtapi: invalid tracker response %q", txt)
		}
		trackerURL, err := url.Parse(trackerStr)
		if err != nil {
			return fmt.Errorf("rtapi: parse tracker url: %w", err)
		}
		ts[i].Tracker = trackerURL
		i++
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if i != len(ts) {
		return fmt.Errorf("rtapi: received %d trackers for %d torrents", i, len(ts))
	}

	return nil
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
