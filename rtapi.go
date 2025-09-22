package rtapi

// Written for 'pyed/rtelegram'.

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"os"
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

type xmlrpcMethodResponse struct {
	Params []xmlrpcParam `xml:"params>param"`
}

type xmlrpcParam struct {
	Value xmlrpcValue `xml:"value"`
}

type xmlrpcValue struct {
	String  *string       `xml:"string,omitempty"`
	Array   *xmlrpcArray  `xml:"array,omitempty"`
	Struct  *xmlrpcStruct `xml:"struct,omitempty"`
	Int     *int64        `xml:"int,omitempty"`
	I4      *int64        `xml:"i4,omitempty"`
	I8      *int64        `xml:"i8,omitempty"`
	Double  *float64      `xml:"double,omitempty"`
	Boolean *bool         `xml:"boolean,omitempty"`
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

func decodeMethodResponse(r io.Reader) (*xmlrpcMethodResponse, error) {
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("rtapi: read response: %w", err)
	}

	start := bytes.IndexByte(payload, '<')
	if start == -1 {
		return nil, fmt.Errorf("rtapi: xml response not found")
	}

	payload = payload[start:]

	var resp xmlrpcMethodResponse
	if err := xml.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("rtapi: decode xmlrpc response: %w", err)
	}

	return &resp, nil
}

func (resp *xmlrpcMethodResponse) arrayParam() ([]xmlrpcValue, error) {
	if resp == nil || len(resp.Params) == 0 {
		return nil, fmt.Errorf("rtapi: xmlrpc response missing params")
	}

	array := resp.Params[0].Value.Array
	if array == nil {
		return nil, fmt.Errorf("rtapi: expected array value in response param")
	}

	return array.Values, nil
}

func (v xmlrpcValue) arrayValues() ([]xmlrpcValue, error) {
	if v.Array == nil {
		return nil, fmt.Errorf("rtapi: expected array value")
	}
	return v.Array.Values, nil
}

func (v xmlrpcValue) firstArrayValue() (xmlrpcValue, error) {
	values, err := v.arrayValues()
	if err != nil {
		return xmlrpcValue{}, err
	}
	if len(values) == 0 {
		return xmlrpcValue{}, fmt.Errorf("rtapi: expected value in array")
	}
	return values[0], nil
}

func (v xmlrpcValue) stringValue() (string, error) {
	if v.String != nil {
		return *v.String, nil
	}
	return "", fmt.Errorf("rtapi: expected string value")
}

func (v xmlrpcValue) int64Value() (int64, error) {
	switch {
	case v.I8 != nil:
		return *v.I8, nil
	case v.I4 != nil:
		return *v.I4, nil
	case v.Int != nil:
		return *v.Int, nil
	}
	return 0, fmt.Errorf("rtapi: expected integer value")
}

func (v xmlrpcValue) uint64Value() (uint64, error) {
	n, err := v.int64Value()
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("rtapi: expected non-negative integer value")
	}
	return uint64(n), nil
}

func (r *Rtorrent) execute(req string) (*xmlrpcMethodResponse, error) {
	data := encode(req)
	conn, err := r.send(data)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return decodeMethodResponse(conn)
}

// Torrents returns a slice that contains all the torrents.
func (r *Rtorrent) Torrents() (Torrents, error) {
	req, err := buildTorrentsRequest()
	if err != nil {
		return nil, err
	}

	resp, err := r.execute(req)
	if err != nil {
		return nil, err
	}

	values, err := resp.arrayParam()
	if err != nil {
		return nil, err
	}

	torrents := make(Torrents, 0, len(values))
	for _, torrentValue := range values {
		torrent, err := parseTorrent(torrentValue)
		if err != nil {
			return nil, err
		}
		torrents = append(torrents, torrent)
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

func parseTorrent(value xmlrpcValue) (*Torrent, error) {
	fields, err := value.arrayValues()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent: %w", err)
	}

	const expectedFields = 16
	if len(fields) < expectedFields {
		return nil, fmt.Errorf("rtapi: expected %d torrent fields, got %d", expectedFields, len(fields))
	}

	t := new(Torrent)

	if t.Name, err = fields[0].stringValue(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent name: %w", err)
	}
	if t.Hash, err = fields[1].stringValue(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent hash: %w", err)
	}

	if t.DownRate, err = fields[2].uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent down rate: %w", err)
	}
	if t.UpRate, err = fields[3].uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent up rate: %w", err)
	}

	sizeChunks, err := fields[4].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent size chunks: %w", err)
	}
	chunkSize, err := fields[5].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent chunk size: %w", err)
	}
	completedChunks, err := fields[6].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent completed chunks: %w", err)
	}
	ratioRaw, err := fields[7].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent ratio: %w", err)
	}

	t.Size = sizeChunks * chunkSize
	t.Completed = completedChunks * chunkSize
	t.Percent, t.ETA = calcPercentAndETA(t.Size, t.Completed, t.DownRate)
	t.Ratio = round(float64(ratioRaw)/1000, 2)
	t.UpTotal = uint64(round(float64(t.Completed)*(float64(ratioRaw)/1000), 1))

	if t.Age, err = fields[8].uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent age: %w", err)
	}
	if t.Message, err = fields[9].stringValue(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent message: %w", err)
	}
	if t.Path, err = fields[10].stringValue(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent path: %w", err)
	}

	isActive, err := fields[11].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent active flag: %w", err)
	}
	connectionCurrent, err := fields[12].stringValue()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent connection: %w", err)
	}
	complete, err := fields[13].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent complete flag: %w", err)
	}
	hashing, err := fields[14].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent hashing flag: %w", err)
	}
	if t.Label, err = fields[15].stringValue(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent label: %w", err)
	}

	switch {
	case isActive == 1 && len(t.Message) != 0:
		t.State = Error
	case hashing != 0:
		t.State = Hashing
	case isActive == 1 && complete == 1:
		t.State = Seeding
	case isActive == 1 && connectionCurrent == "leech":
		t.State = Leeching
	case complete == 1:
		t.State = Complete
	default:
		t.State = Stopped
	}

	return t, nil
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

	resp, err := r.execute(req)
	if err != nil {
		return 0, 0
	}

	values, err := resp.arrayParam()
	if err != nil {
		return 0, 0
	}

	if len(values) < 2 {
		return 0, 0
	}

	downVal, err := values[0].firstArrayValue()
	if err != nil {
		return 0, 0
	}
	upVal, err := values[1].firstArrayValue()
	if err != nil {
		return 0, 0
	}

	down, err = downVal.uint64Value()
	if err != nil {
		return 0, 0
	}
	up, err = upVal.uint64Value()
	if err != nil {
		return 0, 0
	}

	return down, up
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

	resp, err := r.execute(req)
	if err != nil {
		return nil, err
	}

	values, err := resp.arrayParam()
	if err != nil {
		return nil, err
	}

	if len(values) < 6 {
		return nil, fmt.Errorf("rtapi: expected 6 stats values, got %d", len(values))
	}

	throttleUpVal, err := values[0].firstArrayValue()
	if err != nil {
		return nil, err
	}
	if st.ThrottleUp, err = throttleUpVal.uint64Value(); err != nil {
		return nil, err
	}

	throttleDownVal, err := values[1].firstArrayValue()
	if err != nil {
		return nil, err
	}
	if st.ThrottleDown, err = throttleDownVal.uint64Value(); err != nil {
		return nil, err
	}

	totalUpVal, err := values[2].firstArrayValue()
	if err != nil {
		return nil, err
	}
	if st.TotalUp, err = totalUpVal.uint64Value(); err != nil {
		return nil, err
	}

	totalDownVal, err := values[3].firstArrayValue()
	if err != nil {
		return nil, err
	}
	if st.TotalDown, err = totalDownVal.uint64Value(); err != nil {
		return nil, err
	}

	portVal, err := values[4].firstArrayValue()
	if err != nil {
		return nil, err
	}
	port, err := portVal.uint64Value()
	if err != nil {
		return nil, err
	}
	st.Port = fmt.Sprintf("%d", port)

	directoryVal, err := values[5].firstArrayValue()
	if err != nil {
		return nil, err
	}
	if st.Directory, err = directoryVal.stringValue(); err != nil {
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

	resp, err := r.execute(req)
	if err != nil {
		return "", err
	}

	values, err := resp.arrayParam()
	if err != nil {
		return "", err
	}

	if len(values) < 2 {
		return "", fmt.Errorf("rtapi: expected 2 version values, got %d", len(values))
	}

	clientVal, err := values[0].firstArrayValue()
	if err != nil {
		return "", err
	}
	clientVer, err := clientVal.stringValue()
	if err != nil {
		return "", err
	}

	libVal, err := values[1].firstArrayValue()
	if err != nil {
		return "", err
	}
	libraryVer, err := libVal.stringValue()
	if err != nil {
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

	resp, err := r.execute(req)
	if err != nil {
		return err
	}

	values, err := resp.arrayParam()
	if err != nil {
		return err
	}

	if len(values) != len(ts) {
		return fmt.Errorf("rtapi: received %d trackers for %d torrents", len(values), len(ts))
	}

	for i, trackerValue := range values {
		trackerValues, err := trackerValue.arrayValues()
		if err != nil {
			return err
		}

		var trackerStr string
		if len(trackerValues) > 0 {
			trackerStr, err = trackerValues[0].stringValue()
			if err != nil {
				return err
			}
		}

		trackerURL, err := url.Parse(trackerStr)
		if err != nil {
			return fmt.Errorf("rtapi: parse tracker url: %w", err)
		}
		ts[i].Tracker = trackerURL
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

// round function.
func round(v float64, decimals int) float64 {
	var pow float64 = 1
	for i := 0; i < decimals; i++ {
		pow *= 10
	}
	return float64(int((v*pow)+0.5)) / pow
}
