package rtapi

// Written for 'pyed/rtelegram'.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// DefaultTimeout bounds each connection, request, and response lifecycle.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxResponseSize bounds one SCGI response while allowing callers
	// with unusually large libraries to choose a larger explicit limit.
	DefaultMaxResponseSize int64 = 16 << 20
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
	Age       uint64 // when rTorrent loaded the torrent, in Unix seconds; reset when rTorrent restarts, unlike Started
	UpTotal   uint64
	State     string
	Message   string
	Tracker   *url.URL
	Path      string // d.base_path; empty until rTorrent opens the torrent
	Label     string // d.custom1, where ruTorrent keeps its label, percent-encoded
	// Directory is reported even for torrents rTorrent has not opened: it is
	// the data directory of a multi-file torrent, or the directory containing
	// a single-file torrent's file.
	Directory string
	MultiFile bool
	Finished  uint64 // when the torrent completed, in Unix seconds; 0 until it has
	Started   uint64 // when the torrent first started, in Unix seconds; 0 until it has
}

// Torrents is a slice of *Torrent.
type Torrents []*Torrent

type xmlrpcMethodCall struct {
	XMLName    xml.Name      `xml:"methodCall"`
	MethodName string        `xml:"methodName"`
	Params     []xmlrpcParam `xml:"params>param"`
}

type xmlrpcMethodResponse struct {
	XMLName xml.Name         `xml:"methodResponse"`
	Params  []xmlrpcParam    `xml:"params>param"`
	Fault   *xmlrpcFaultBody `xml:"fault"`
}

type xmlrpcFaultBody struct {
	Value xmlrpcValue `xml:"value"`
}

type xmlrpcParam struct {
	Value xmlrpcValue `xml:"value"`
}

type xmlrpcValue struct {
	String  *string       `xml:"string,omitempty"`
	Base64  *string       `xml:"base64,omitempty"`
	Array   *xmlrpcArray  `xml:"array,omitempty"`
	Struct  *xmlrpcStruct `xml:"struct,omitempty"`
	Int     *int64        `xml:"int,omitempty"`
	I4      *int64        `xml:"i4,omitempty"`
	I8      *int64        `xml:"i8,omitempty"`
	Double  *float64      `xml:"double,omitempty"`
	Boolean *bool         `xml:"boolean,omitempty"`
	Text    string        `xml:",chardata"`
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

func newBase64Param(val []byte) xmlrpcParam {
	v := base64.StdEncoding.EncodeToString(val)
	return xmlrpcParam{Value: xmlrpcValue{Base64: &v}}
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

func newIntValue(val int64) xmlrpcValue {
	return xmlrpcValue{I8: &val}
}

// newMethodCallValues is newMethodCall for parameters that are not all strings.
func newMethodCallValues(method string, params ...xmlrpcValue) xmlrpcValue {
	return newStructValue(
		newStringMember("methodName", method),
		newArrayMember("params", params...),
	)
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

// DotTorrentWithOptions controls how a torrent is loaded. An empty Dir or
// Label leaves rTorrent's default, and Stopped loads the torrent without
// starting it. Link is used by DownloadWithOptions; Name is caller metadata.
type DotTorrentWithOptions struct {
	Link    string
	Name    string
	Dir     string
	Label   string
	Stopped bool
}

// Rtorrent is a client for one rTorrent instance, reached over SCGI (a Unix
// socket or host:port) or over HTTP(S) XML-RPC.
type Rtorrent struct {
	network, address string
	// endpoint and its credentials are set for http and https addresses.
	endpoint           *url.URL
	username, password string
	Version            string
	// Timeout bounds each request, from dialing to reading the response.
	// Non-positive values use DefaultTimeout.
	Timeout time.Duration
	// MaxResponseSize bounds response bytes. Non-positive values use DefaultMaxResponseSize.
	MaxResponseSize int64
	// renamedMulticall is set, atomically, once rTorrent has answered that it
	// has no d.multicall2.
	renamedMulticall int32
}

// NewRtorrent connects to rTorrent at address: the path of an SCGI Unix
// socket, an SCGI host:port, or an http:// or https:// XML-RPC URL such as
// https://user:password@seedbox.example/RPC2. Credentials in a URL are sent
// with HTTP basic authentication.
func NewRtorrent(address string) (*Rtorrent, error) {
	return NewRtorrentContext(context.Background(), address)
}

// NewRtorrentContext is NewRtorrent with a context for the version request.
func NewRtorrentContext(ctx context.Context, address string) (*Rtorrent, error) {
	if strings.TrimSpace(address) == "" {
		return nil, fmt.Errorf("rtapi: address must not be empty")
	}

	rt := &Rtorrent{
		network:         "tcp",
		address:         address,
		Timeout:         DefaultTimeout,
		MaxResponseSize: DefaultMaxResponseSize,
	}
	if endpoint, err := url.Parse(address); err == nil &&
		(strings.EqualFold(endpoint.Scheme, "http") || strings.EqualFold(endpoint.Scheme, "https")) {
		if endpoint.Host == "" {
			return nil, fmt.Errorf("rtapi: %s address has no host", endpoint.Scheme)
		}
		if endpoint.User != nil {
			rt.username = endpoint.User.Username()
			rt.password, _ = endpoint.User.Password()
			endpoint.User = nil // keep credentials out of errors
		}
		rt.endpoint = endpoint
		rt.network, rt.address = "http", endpoint.String()
	} else if _, err := os.Stat(address); err == nil {
		rt.network = "unix"
	}

	ver, err := rt.getVersion(ctx)
	if err != nil {
		return nil, err
	}

	rt.Version = ver
	return rt, nil
}

// torrentFields are the d.* getters parseTorrent reads, in order.
var torrentFields = []string{
	"d.name",
	"d.hash",
	"d.down.rate",
	"d.up.rate",
	"d.size_bytes",
	"d.completed_bytes",
	"d.ratio",
	"d.up.total",
	"d.load_date",
	"d.message",
	"d.base_path",
	"d.is_active",
	"d.connection_current",
	"d.complete",
	"d.hashing",
	"d.custom1",
	"d.directory",
	"d.is_multi_file",
	"d.timestamp.finished",
	"d.timestamp.started",
}

func buildTorrentsRequest() (string, error) {
	return buildDownloadMulticall("d.multicall2", torrentFields)
}

// buildDownloadMulticall builds a call of method, d.multicall2 or d.multicall,
// for fields of every torrent.
func buildDownloadMulticall(method string, fields []string) (string, error) {
	params := []xmlrpcParam{newStringParam(""), newStringParam("main")}
	for _, field := range fields {
		params = append(params, newStringParam(field+"="))
	}
	return marshalMethodCall(xmlrpcMethodCall{MethodName: method, Params: params})
}

// buildLoadRequest builds a load call for source, followed by the commands
// that set the new torrent's directory and label when they are given.
func buildLoadRequest(method string, source xmlrpcParam, dir, label string) (string, error) {
	params := []xmlrpcParam{newStringParam(""), source}
	if dir != "" {
		params = append(params, newStringParam("d.directory.set="+strconv.Quote(dir)))
	}
	if label != "" {
		params = append(params, newStringParam("d.custom1.set="+strconv.Quote(label)))
	}
	return marshalMethodCall(xmlrpcMethodCall{MethodName: method, Params: params})
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
					newMethodCall("system.pid"),
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

// marshalMethodCall writes request as encoding/xml would, without its
// reflection: a torrent list's tracker request has a call per torrent.
func marshalMethodCall(request xmlrpcMethodCall) (string, error) {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString("<methodCall><methodName>")
	escapeText(&b, request.MethodName)
	b.WriteString("</methodName>")
	b.WriteString("<params>")
	for _, param := range request.Params {
		b.WriteString("<param>")
		writeValue(&b, param.Value)
		b.WriteString("</param>")
	}
	b.WriteString("</params>")
	b.WriteString("</methodCall>")
	return b.String(), nil
}

func writeValue(b *bytes.Buffer, v xmlrpcValue) {
	b.WriteString("<value>")
	writeText := func(element, text string) {
		b.WriteString("<" + element + ">")
		escapeText(b, text)
		b.WriteString("</" + element + ">")
	}
	if v.String != nil {
		writeText("string", *v.String)
	}
	if v.Base64 != nil {
		writeText("base64", *v.Base64)
	}
	if v.Array != nil {
		b.WriteString("<array><data>")
		for _, item := range v.Array.Values {
			writeValue(b, item)
		}
		b.WriteString("</data></array>")
	}
	if v.Struct != nil {
		b.WriteString("<struct>")
		for _, member := range v.Struct.Members {
			b.WriteString("<member>")
			writeText("name", member.Name)
			writeValue(b, member.Value)
			b.WriteString("</member>")
		}
		b.WriteString("</struct>")
	}
	for _, number := range []struct {
		element string
		value   *int64
	}{{"int", v.Int}, {"i4", v.I4}, {"i8", v.I8}} {
		if number.value != nil {
			writeText(number.element, strconv.FormatInt(*number.value, 10))
		}
	}
	if v.Double != nil {
		writeText("double", strconv.FormatFloat(*v.Double, 'g', -1, 64))
	}
	if v.Boolean != nil {
		writeText("boolean", strconv.FormatBool(*v.Boolean))
	}
	escapeText(b, v.Text)
	b.WriteString("</value>")
}

// escapeText writes s escaped as xml.EscapeText escapes it, without copying
// the common text that needs no escaping.
func escapeText(b *bytes.Buffer, s string) {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7f || c == '"' || c == '\'' || c == '&' || c == '<' || c == '>' {
			xml.EscapeText(b, []byte(s))
			return
		}
	}
	b.WriteString(s)
}

func decodeMethodResponse(r io.Reader, limit int64) (*xmlrpcMethodResponse, error) {
	return decodeSizedResponse(r, limit, -1)
}

// decodeSizedResponse is decodeMethodResponse for a response of length bytes,
// or of unknown length when length is negative.
func decodeSizedResponse(r io.Reader, limit, length int64) (*xmlrpcMethodResponse, error) {
	if limit <= 0 {
		limit = DefaultMaxResponseSize
	}
	payload, err := readResponse(r, limit, length)
	if err != nil {
		return nil, fmt.Errorf("rtapi: read response: %w", err)
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("rtapi: response exceeds %d bytes", limit)
	}

	start := bytes.Index(payload, []byte("<methodResponse"))
	if start == -1 {
		return nil, fmt.Errorf("rtapi: XML-RPC methodResponse not found")
	}

	payload = payload[start:]

	resp, ok := decodeFast(payload)
	if !ok {
		resp = new(xmlrpcMethodResponse)
		if err := xml.Unmarshal(payload, resp); err != nil {
			return nil, fmt.Errorf("rtapi: decode XML-RPC response: %w", err)
		}
	}
	if resp.Fault != nil {
		fault, ok, err := faultFromValue(resp.Fault.Value)
		if err != nil {
			return nil, fmt.Errorf("rtapi: decode XML-RPC fault: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("rtapi: malformed XML-RPC fault")
		}
		return nil, fault
	}
	if len(resp.Params) == 0 {
		return nil, fmt.Errorf("rtapi: XML-RPC response missing params")
	}

	return resp, nil
}

// readResponse reads r to its end, or to one byte past limit. A response
// that states its length, in length or in the Content-Length of the header
// rTorrent puts before SCGI responses, is read into a buffer allocated once at
// that size rather than grown as it fills; a large library's torrent list is
// megabytes.
func readResponse(r io.Reader, limit, length int64) ([]byte, error) {
	readLimit := limit
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	r = io.LimitReader(r, readLimit)
	var buf []byte
	if length >= 0 && length < readLimit {
		// One byte more than stated lets the read that finds the end fit.
		buf = make([]byte, 0, length+1)
	} else {
		head := make([]byte, 512)
		n, err := r.Read(head)
		head = head[:n]
		total, ok := scgiResponseLength(head)
		if !ok || total >= readLimit {
			if err != nil {
				return head, ignoreEOF(err)
			}
			return io.ReadAll(io.MultiReader(bytes.NewReader(head), r))
		}
		buf = append(make([]byte, 0, max(total+1, int64(n))), head...)
		if err != nil {
			return buf, ignoreEOF(err)
		}
	}
	for {
		if len(buf) == cap(buf) {
			// The stated length was wrong.
			buf = slices.Grow(buf, cap(buf))
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err != nil {
			return buf, ignoreEOF(err)
		}
	}
}

func ignoreEOF(err error) error {
	if err == io.EOF {
		return nil
	}
	return err
}

// scgiResponseLength returns the length of an SCGI response, header included,
// from the Content-Length in its header, which starts data.
func scgiResponseLength(data []byte) (int64, bool) {
	end := bytes.Index(data, []byte("\r\n\r\n"))
	if end < 0 {
		return 0, false
	}
	for line := range strings.SplitSeq(string(data[:end]), "\r\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		length, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || length < 0 || length > math.MaxInt64-int64(end)-4 {
			return 0, false
		}
		return int64(end) + 4 + length, true
	}
	return 0, false
}

// XMLRPCFault is an error returned by rTorrent.
type XMLRPCFault struct {
	Code    int64
	Message string
}

func (f *XMLRPCFault) Error() string {
	return fmt.Sprintf("rtapi: XML-RPC faultCode %d: faultString %q", f.Code, f.Message)
}

func faultFromValue(v xmlrpcValue) (*XMLRPCFault, bool, error) {
	if v.Struct == nil {
		return nil, false, nil
	}

	var fault XMLRPCFault
	var hasCode, hasMessage bool
	for _, member := range v.Struct.Members {
		switch member.Name {
		case "faultCode":
			code, err := member.Value.int64Value()
			if err != nil {
				return nil, true, fmt.Errorf("faultCode: %w", err)
			}
			fault.Code, hasCode = code, true
		case "faultString":
			message, err := member.Value.stringValue()
			if err != nil {
				return nil, true, fmt.Errorf("faultString: %w", err)
			}
			fault.Message, hasMessage = message, true
		}
	}

	if !hasCode && !hasMessage {
		return nil, false, nil
	}
	if !hasCode || !hasMessage {
		return nil, true, fmt.Errorf("fault is missing faultCode or faultString")
	}
	return &fault, true, nil
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
	if v.Base64 == nil && v.Array == nil && v.Struct == nil && v.Int == nil &&
		v.I4 == nil && v.I8 == nil && v.Double == nil && v.Boolean == nil {
		return v.Text, nil
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

func (r *Rtorrent) execute(ctx context.Context, req string) (*xmlrpcMethodResponse, error) {
	if r == nil {
		return nil, errors.New("rtapi: nil rTorrent client")
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.ReadCloser
	var err error
	length := int64(-1)
	if r.endpoint != nil {
		body, length, err = r.post(ctx, req)
	} else {
		body, err = r.send(ctx, encode(req))
	}
	if err != nil {
		return nil, contextError(ctx, fmt.Errorf("rtapi: send request: %w", err))
	}
	defer body.Close()

	resp, err := decodeSizedResponse(body, r.MaxResponseSize, length)
	if err != nil {
		return nil, contextError(ctx, err)
	}
	return resp, nil
}

// contextError adds the context's error to a request that was cancelled or
// timed out, so callers can match it with errors.Is.
func contextError(ctx context.Context, err error) error {
	ctxErr := ctx.Err()
	if ctxErr == nil && errors.Is(err, os.ErrDeadlineExceeded) {
		// Connection deadlines come from ctx, but their timer can fire before
		// the context's own.
		ctxErr = context.DeadlineExceeded
	}
	if ctxErr != nil {
		return fmt.Errorf("%w (%w)", err, ctxErr)
	}
	return err
}

func (r *Rtorrent) executeMulticall(ctx context.Context, req string, expected int) (*xmlrpcMethodResponse, error) {
	resp, err := r.execute(ctx, req)
	if err != nil {
		return nil, err
	}
	values, err := resp.arrayParam()
	if err != nil {
		return nil, err
	}
	for i, value := range values {
		fault, ok, err := faultFromValue(value)
		if err != nil {
			return nil, fmt.Errorf("rtapi: decode XML-RPC multicall item %d: %w", i, err)
		}
		if ok {
			return nil, fmt.Errorf("rtapi: XML-RPC multicall item %d: %w", i, fault)
		}
	}
	if len(values) != expected {
		return nil, fmt.Errorf("rtapi: expected %d XML-RPC multicall results, got %d", expected, len(values))
	}
	return resp, nil
}

// Torrents returns every torrent, with its tracker. List is cheaper when the
// trackers are not needed.
func (r *Rtorrent) Torrents() (Torrents, error) {
	return r.TorrentsContext(context.Background())
}

// TorrentsContext is Torrents with a context.
func (r *Rtorrent) TorrentsContext(ctx context.Context) (Torrents, error) {
	return r.ListContext(ctx, ListOptions{Trackers: true})
}

// ListOptions chooses what List fetches besides each torrent's own details.
type ListOptions struct {
	// Trackers sets each torrent's Tracker, as Torrents does. That takes a
	// second request with a call per torrent, which is slow for large
	// libraries.
	Trackers bool
}

// List returns every torrent, leaving Tracker unset unless options ask for it.
func (r *Rtorrent) List(options ListOptions) (Torrents, error) {
	return r.ListContext(context.Background(), options)
}

// ListContext is List with a context.
func (r *Rtorrent) ListContext(ctx context.Context, options ListOptions) (Torrents, error) {
	rows, err := r.downloadMulticall(ctx, torrentFields)
	if err != nil {
		return nil, err
	}
	torrents := make(Torrents, 0, len(rows))
	for _, row := range rows {
		torrent, err := parseTorrent(row)
		if err != nil {
			return nil, err
		}
		torrents = append(torrents, torrent)
	}
	if options.Trackers {
		if err := r.getTrackers(ctx, torrents); err != nil {
			return nil, err
		}
	}
	return torrents, nil
}

// Trackers sets the Tracker of each of ts, for torrents listed without them.
func (r *Rtorrent) Trackers(ts Torrents) error {
	return r.TrackersContext(context.Background(), ts)
}

// TrackersContext is Trackers with a context.
func (r *Rtorrent) TrackersContext(ctx context.Context, ts Torrents) error {
	if _, err := torrentHashes(ts); err != nil {
		return err
	}
	return r.getTrackers(ctx, ts)
}

// Hashes returns the info-hash of every torrent, which is much cheaper than
// listing them for finding out which are loaded.
func (r *Rtorrent) Hashes() ([]string, error) {
	return r.HashesContext(context.Background())
}

// HashesContext is Hashes with a context.
func (r *Rtorrent) HashesContext(ctx context.Context) ([]string, error) {
	rows, err := r.downloadMulticall(ctx, []string{"d.hash"})
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(rows))
	for i, row := range rows {
		hash, err := row.firstArrayValue()
		if err == nil {
			hashes[i], err = hash.stringValue()
		}
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent hash: %w", err)
		}
	}
	return hashes, nil
}

// A Transfer is how much of a torrent's data rTorrent has uploaded and
// downloaded, in bytes. It counts the torrent's own data, not the protocol
// messages exchanged with peers, which rTorrent's global totals include.
// rTorrent keeps both counts across restarts (from 0.9.8; 0.9.6 keeps only
// Up), and they start over if the torrent is removed and added again.
type Transfer struct {
	Hash     string
	Up, Down uint64
}

// Transfers returns every torrent's Transfer, which is much cheaper than
// listing them.
func (r *Rtorrent) Transfers() ([]Transfer, error) {
	return r.TransfersContext(context.Background())
}

// TransfersContext is Transfers with a context.
func (r *Rtorrent) TransfersContext(ctx context.Context) ([]Transfer, error) {
	rows, err := r.downloadMulticall(ctx, []string{"d.hash", "d.up.total", "d.down.total"})
	if err != nil {
		return nil, err
	}
	transfers := make([]Transfer, len(rows))
	for i, row := range rows {
		fields, err := row.arrayValues()
		if err == nil && len(fields) < 3 {
			err = fmt.Errorf("expected 3 fields, got %d", len(fields))
		}
		if err == nil {
			transfers[i].Hash, err = fields[0].stringValue()
		}
		if err == nil {
			transfers[i].Up, err = fields[1].uint64Value()
		}
		if err == nil {
			transfers[i].Down, err = fields[2].uint64Value()
		}
		if err != nil {
			return nil, fmt.Errorf("rtapi: parse torrent transfer: %w", err)
		}
	}
	return transfers, nil
}

// downloadMulticall returns fields of every torrent, a row per torrent.
// rTorrent plans to drop d.multicall2, an alias of d.multicall since 0.16, so
// a client whose rTorrent answers that it has no d.multicall2 switches.
func (r *Rtorrent) downloadMulticall(ctx context.Context, fields []string) ([]xmlrpcValue, error) {
	method := "d.multicall2"
	if atomic.LoadInt32(&r.renamedMulticall) != 0 {
		method = "d.multicall"
	}
	req, err := buildDownloadMulticall(method, fields)
	if err != nil {
		return nil, err
	}
	resp, err := r.execute(ctx, req)
	var fault *XMLRPCFault
	if method == "d.multicall2" && errors.As(err, &fault) && fault.Code == noSuchMethod {
		atomic.StoreInt32(&r.renamedMulticall, 1)
		return r.downloadMulticall(ctx, fields)
	}
	if err != nil {
		return nil, err
	}
	return resp.arrayParam()
}

// noSuchMethod is the fault code of a call to a method rTorrent does not have.
const noSuchMethod = -506

func parseTorrent(value xmlrpcValue) (*Torrent, error) {
	fields, err := value.arrayValues()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent: %w", err)
	}

	expectedFields := len(torrentFields)
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

	if t.Size, err = fields[4].uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent size: %w", err)
	}
	if t.Completed, err = fields[5].uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent completed bytes: %w", err)
	}
	ratioRaw, err := fields[6].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent ratio: %w", err)
	}
	if t.UpTotal, err = fields[7].uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent uploaded bytes: %w", err)
	}
	t.Percent, t.ETA = calcPercentAndETA(t.Size, t.Completed, t.DownRate)
	t.Ratio = math.Round(float64(ratioRaw)/10) / 100

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
	if t.Directory, err = fields[16].stringValue(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent directory: %w", err)
	}
	multiFile, err := fields[17].uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent multi-file flag: %w", err)
	}
	t.MultiFile = multiFile != 0
	if t.Finished, err = fields[18].uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent finished time: %w", err)
	}
	if t.Started, err = fields[19].uint64Value(); err != nil {
		return nil, fmt.Errorf("rtapi: parse torrent start time: %w", err)
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
	return r.GetTorrentContext(context.Background(), hash)
}

// GetTorrentContext is GetTorrent with a context.
func (r *Rtorrent) GetTorrentContext(ctx context.Context, hash string) (*Torrent, error) {
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("rtapi: torrent hash must not be empty")
	}
	// Ask for this torrent's fields and tracker alone, rather than listing
	// every torrent.
	calls := make([]xmlrpcValue, 0, len(torrentFields)+1)
	for _, field := range torrentFields {
		calls = append(calls, newMethodCall(field, hash))
	}
	calls = append(calls, newMethodCall("t.url", hash+":t0"))
	req, err := marshalMethodCall(xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params:     []xmlrpcParam{{Value: newArrayValue(calls...)}},
	})
	if err != nil {
		return nil, err
	}

	resp, err := r.execute(ctx, req)
	if err != nil {
		return nil, err
	}
	values, err := resp.arrayParam()
	if err != nil {
		return nil, err
	}
	if len(values) != len(calls) {
		return nil, fmt.Errorf("rtapi: expected %d XML-RPC multicall results, got %d", len(calls), len(values))
	}

	fields := make([]xmlrpcValue, len(torrentFields))
	for i, field := range torrentFields {
		if fault, ok, err := faultFromValue(values[i]); err != nil {
			return nil, fmt.Errorf("rtapi: decode %s fault: %w", field, err)
		} else if ok {
			if isMissingTarget(fault) {
				return nil, fmt.Errorf("rtapi: no torrent with hash %q", hash)
			}
			return nil, fmt.Errorf("rtapi: get %s: %w", field, fault)
		}
		if fields[i], err = values[i].firstArrayValue(); err != nil {
			return nil, fmt.Errorf("rtapi: parse %s: %w", field, err)
		}
	}
	torrent, err := parseTorrent(xmlrpcValue{Array: &xmlrpcArray{Values: fields}})
	if err != nil {
		return nil, err
	}
	if torrent.Tracker, err = parseTracker(values[len(torrentFields)]); err != nil {
		return nil, fmt.Errorf("rtapi: get tracker: %w", err)
	}
	return torrent, nil
}

// call sends one method call and returns its result.
func (r *Rtorrent) call(ctx context.Context, method string, params ...xmlrpcValue) (xmlrpcValue, error) {
	call := xmlrpcMethodCall{MethodName: method}
	for _, param := range params {
		call.Params = append(call.Params, xmlrpcParam{Value: param})
	}
	req, err := marshalMethodCall(call)
	if err != nil {
		return xmlrpcValue{}, err
	}
	resp, err := r.execute(ctx, req)
	if err != nil {
		return xmlrpcValue{}, err
	}
	return resp.Params[0].Value, nil
}

// FreeDiskSpace returns the free space, in bytes, on the filesystem holding a
// torrent's data. rTorrent learns where that is when it opens the torrent,
// as it does to start it, and reports 0 for torrents it has not opened.
func (r *Rtorrent) FreeDiskSpace(hash string) (uint64, error) {
	return r.FreeDiskSpaceContext(context.Background(), hash)
}

// FreeDiskSpaceContext is FreeDiskSpace with a context.
func (r *Rtorrent) FreeDiskSpaceContext(ctx context.Context, hash string) (uint64, error) {
	if strings.TrimSpace(hash) == "" {
		return 0, fmt.Errorf("rtapi: torrent hash must not be empty")
	}
	value, err := r.call(ctx, "d.free_diskspace", newStringValue(hash))
	if err != nil {
		return 0, fmt.Errorf("rtapi: free disk space: %w", err)
	}
	free, err := value.uint64Value()
	if err != nil {
		return 0, fmt.Errorf("rtapi: parse free disk space: %w", err)
	}
	return free, nil
}

// Download takes URL to a .torrent file to start downloading it.
func (r *Rtorrent) Download(url string) error {
	return r.DownloadContext(context.Background(), url)
}

// DownloadContext is Download with a context.
func (r *Rtorrent) DownloadContext(ctx context.Context, url string) error {
	return r.DownloadWithOptionsContext(ctx, &DotTorrentWithOptions{Link: url})
}

// DownloadWithOptions takes *DotTorrentWithOptions downloading it.
func (r *Rtorrent) DownloadWithOptions(tFile *DotTorrentWithOptions) error {
	return r.DownloadWithOptionsContext(context.Background(), tFile)
}

// DownloadWithOptionsContext is DownloadWithOptions with a context.
func (r *Rtorrent) DownloadWithOptionsContext(ctx context.Context, tFile *DotTorrentWithOptions) error {
	if tFile == nil {
		return fmt.Errorf("rtapi: torrent options must not be nil")
	}
	if strings.TrimSpace(tFile.Link) == "" {
		return fmt.Errorf("rtapi: download URL must not be empty")
	}

	// The verbose loads make rTorrent log why a link failed to load.
	method := "load.start_verbose"
	if tFile.Stopped {
		method = "load.verbose"
	}
	req, err := buildLoadRequest(method, newStringParam(tFile.Link), tFile.Dir, tFile.Label)
	if err != nil {
		return fmt.Errorf("rtapi: build download request: %w", err)
	}
	if _, err := r.execute(ctx, req); err != nil {
		return fmt.Errorf("rtapi: download: %w", err)
	}
	return nil
}

// DownloadRaw loads torrent metadata without requiring rTorrent to fetch a URL.
func (r *Rtorrent) DownloadRaw(data []byte, options *DotTorrentWithOptions) error {
	return r.DownloadRawContext(context.Background(), data, options)
}

// DownloadRawContext is DownloadRaw with a context.
func (r *Rtorrent) DownloadRawContext(ctx context.Context, data []byte, options *DotTorrentWithOptions) error {
	if len(data) == 0 {
		return fmt.Errorf("rtapi: torrent data must not be empty")
	}
	if options == nil {
		options = &DotTorrentWithOptions{}
	}

	// rTorrent 0.9.6 has no load.raw_start_verbose, so raw loads stay quiet.
	method := "load.raw_start"
	if options.Stopped {
		method = "load.raw"
	}
	req, err := buildLoadRequest(method, newBase64Param(data), options.Dir, options.Label)
	if err != nil {
		return fmt.Errorf("rtapi: build raw download request: %w", err)
	}
	if _, err := r.execute(ctx, req); err != nil {
		return fmt.Errorf("rtapi: raw download: %w", err)
	}
	return nil
}

func torrentHashes(ts []*Torrent) ([]string, error) {
	hashes := make([]string, len(ts))
	for i, torrent := range ts {
		if torrent == nil {
			return nil, fmt.Errorf("rtapi: torrent %d must not be nil", i)
		}
		if strings.TrimSpace(torrent.Hash) == "" {
			return nil, fmt.Errorf("rtapi: torrent %d hash must not be empty", i)
		}
		hashes[i] = torrent.Hash
	}
	return hashes, nil
}

func (r *Rtorrent) mutate(ctx context.Context, method string, ts ...*Torrent) error {
	hashes, err := torrentHashes(ts)
	if err != nil {
		return err
	}
	if len(hashes) == 0 {
		return nil
	}
	req, err := buildSystemMulticallRequest(method, hashes...)
	if err != nil {
		return fmt.Errorf("rtapi: build %s request: %w", method, err)
	}
	if _, err := r.executeMulticall(ctx, req, len(hashes)); err != nil {
		return fmt.Errorf("rtapi: %s: %w", method, err)
	}
	return nil
}

// Stop takes a *Torrent or more to 'd.stop' it/them.
func (r *Rtorrent) Stop(ts ...*Torrent) error {
	return r.StopContext(context.Background(), ts...)
}

// StopContext is Stop with a context.
func (r *Rtorrent) StopContext(ctx context.Context, ts ...*Torrent) error {
	return r.mutate(ctx, "d.stop", ts...)
}

// Start takes a *Torrent or more to 'd.start' it/them.
func (r *Rtorrent) Start(ts ...*Torrent) error {
	return r.StartContext(context.Background(), ts...)
}

// StartContext is Start with a context.
func (r *Rtorrent) StartContext(ctx context.Context, ts ...*Torrent) error {
	return r.mutate(ctx, "d.start", ts...)
}

// Check takes a *Torrent or more to 'd.check_hash' it/them.
func (r *Rtorrent) Check(ts ...*Torrent) error {
	return r.CheckContext(context.Background(), ts...)
}

// CheckContext is Check with a context.
func (r *Rtorrent) CheckContext(ctx context.Context, ts ...*Torrent) error {
	return r.mutate(ctx, "d.check_hash", ts...)
}

// DeleteMetadata removes torrent metadata from rTorrent after validating the
// XML-RPC acknowledgement.
func (r *Rtorrent) DeleteMetadata(ts ...*Torrent) error {
	return r.DeleteMetadataContext(context.Background(), ts...)
}

// DeleteMetadataContext is DeleteMetadata with a context.
func (r *Rtorrent) DeleteMetadataContext(ctx context.Context, ts ...*Torrent) error {
	return r.mutate(ctx, "d.erase", ts...)
}

// SetLabel sets the label of torrents, which is d.custom1; an empty label
// removes it. ruTorrent keeps its labels percent-encoded, as JavaScript's
// encodeURIComponent writes them, and decodes them to show them, so labels
// shared with ruTorrent should be set encoded.
func (r *Rtorrent) SetLabel(label string, ts ...*Torrent) error {
	return r.SetLabelContext(context.Background(), label, ts...)
}

// SetLabelContext is SetLabel with a context.
func (r *Rtorrent) SetLabelContext(ctx context.Context, label string, ts ...*Torrent) error {
	hashes, err := torrentHashes(ts)
	if err != nil || len(hashes) == 0 {
		return err
	}
	calls := make([]xmlrpcValue, len(hashes))
	for i, hash := range hashes {
		calls[i] = newMethodCall("d.custom1.set", hash, label)
	}
	req, err := marshalMethodCall(xmlrpcMethodCall{
		MethodName: "system.multicall",
		Params:     []xmlrpcParam{{Value: newArrayValue(calls...)}},
	})
	if err != nil {
		return err
	}
	if _, err := r.executeMulticall(ctx, req, len(hashes)); err != nil {
		return fmt.Errorf("rtapi: set label: %w", err)
	}
	return nil
}

// SpeedsWithError returns current Down/Up rates and preserves transport and RPC failures.
func (r *Rtorrent) SpeedsWithError() (down, up uint64, err error) {
	return r.SpeedsContext(context.Background())
}

// SpeedsContext is SpeedsWithError with a context.
func (r *Rtorrent) SpeedsContext(ctx context.Context) (down, up uint64, err error) {
	req, err := buildSpeedsRequest()
	if err != nil {
		return 0, 0, fmt.Errorf("rtapi: build speeds request: %w", err)
	}

	resp, err := r.executeMulticall(ctx, req, 2)
	if err != nil {
		return 0, 0, fmt.Errorf("rtapi: get speeds: %w", err)
	}

	values, err := resp.arrayParam()
	if err != nil {
		return 0, 0, err
	}

	if len(values) < 2 {
		return 0, 0, fmt.Errorf("rtapi: expected 2 speed values, got %d", len(values))
	}

	downVal, err := values[0].firstArrayValue()
	if err != nil {
		return 0, 0, fmt.Errorf("rtapi: parse download speed: %w", err)
	}
	upVal, err := values[1].firstArrayValue()
	if err != nil {
		return 0, 0, fmt.Errorf("rtapi: parse upload speed: %w", err)
	}

	down, err = downVal.uint64Value()
	if err != nil {
		return 0, 0, fmt.Errorf("rtapi: parse download speed: %w", err)
	}
	up, err = upVal.uint64Value()
	if err != nil {
		return 0, 0, fmt.Errorf("rtapi: parse upload speed: %w", err)
	}

	return down, up, nil
}

// Stats describes rTorrent's aggregate transfer and listener state.
type Stats struct {
	ThrottleUp, ThrottleDown, TotalUp, TotalDown uint64
	Port, Directory                              string
	// PID is rTorrent's process ID, which changes when rTorrent restarts.
	PID int
}

// Stats returns aggregate rTorrent information.
func (r *Rtorrent) Stats() (*Stats, error) {
	return r.StatsContext(context.Background())
}

// StatsContext is Stats with a context.
func (r *Rtorrent) StatsContext(ctx context.Context) (*Stats, error) {
	st := new(Stats)
	req, err := buildStatsRequest()
	if err != nil {
		return nil, err
	}

	resp, err := r.executeMulticall(ctx, req, 7)
	if err != nil {
		return nil, err
	}

	values, err := resp.arrayParam()
	if err != nil {
		return nil, err
	}

	if len(values) < 7 {
		return nil, fmt.Errorf("rtapi: expected 7 stats values, got %d", len(values))
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

	pidVal, err := values[6].firstArrayValue()
	if err != nil {
		return nil, err
	}
	pid, err := pidVal.uint64Value()
	if err != nil {
		return nil, fmt.Errorf("rtapi: parse rTorrent's process ID: %w", err)
	}
	st.PID = int(pid)

	return st, nil
}

// getVersion returns a string represnts rtorrent/libtorrent versions.
func (r *Rtorrent) getVersion(ctx context.Context) (string, error) {
	req, err := buildVersionRequest()
	if err != nil {
		return "", err
	}

	resp, err := r.executeMulticall(ctx, req, 2)
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
func (r *Rtorrent) getTrackers(ctx context.Context, ts Torrents) error {
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

	resp, err := r.execute(ctx, req)
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
		if ts[i].Tracker, err = parseTracker(trackerValue); err != nil {
			return fmt.Errorf("rtapi: get tracker %d: %w", i, err)
		}
	}

	return nil
}

// parseTracker reads one t.url result. Trackerless torrents, and torrents
// removed since they were listed, answer with a missing-target fault, which is
// reported as no tracker; every other fault is returned.
func parseTracker(value xmlrpcValue) (*url.URL, error) {
	if fault, ok, err := faultFromValue(value); err != nil {
		return nil, fmt.Errorf("decode fault: %w", err)
	} else if ok {
		message := strings.ToLower(fault.Message)
		if isMissingTarget(fault) || (fault.Code == -501 &&
			(strings.Contains(message, "could not find tracker") || strings.Contains(message, "no tracker"))) {
			return nil, nil
		}
		return nil, fault
	}

	trackerValues, err := value.arrayValues()
	if err != nil {
		return nil, err
	}
	var trackerStr string
	if len(trackerValues) > 0 {
		if trackerStr, err = trackerValues[0].stringValue(); err != nil {
			return nil, err
		}
	}
	if trackerStr == "" {
		return nil, nil
	}
	trackerURL, err := url.Parse(trackerStr)
	if err != nil {
		return nil, fmt.Errorf("parse tracker url: %w", err)
	}
	return trackerURL, nil
}

// isMissingTarget reports rTorrent's fault for a hash it has not loaded.
func isMissingTarget(fault *XMLRPCFault) bool {
	return fault.Code == -501 && strings.Contains(strings.ToLower(fault.Message), "info-hash")
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
		remaining := size - done
		ETA = remaining / downrate
		if remaining%downrate != 0 {
			ETA++
		}
	}

	return fmt.Sprintf("%.1f%%", rounded), ETA
}

// send writes SCGI-formatted data and returns the connection to read the
// response from. Cancelling ctx interrupts any read or write in progress.
func (r *Rtorrent) send(ctx context.Context, data []byte) (io.ReadCloser, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, r.network, r.address)
	if err != nil {
		return nil, fmt.Errorf("dial %s %q: %w", r.network, r.address, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			conn.Close()
			return nil, fmt.Errorf("set connection deadline: %w", err)
		}
	}
	response := &scgiResponse{Conn: conn}
	response.stop = context.AfterFunc(ctx, func() { conn.SetDeadline(time.Now()) })

	written, err := writeAll(conn, data)
	if err != nil {
		response.Close()
		return nil, fmt.Errorf("write SCGI request after %d of %d bytes: %w", written, len(data), err)
	}
	if written != len(data) {
		response.Close()
		return nil, fmt.Errorf("write SCGI request: wrote %d of %d bytes", written, len(data))
	}

	return response, nil
}

// post sends an XML-RPC request over HTTP and returns the response body and
// its length, or -1 when unknown. It uses http.DefaultClient, which honors
// HTTPS_PROXY and, on Unix, SSL_CERT_FILE.
func (r *Rtorrent) post(ctx context.Context, req string) (io.ReadCloser, int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.address, strings.NewReader(req))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Content-Type", "text/xml")
	if r.username != "" || r.password != "" {
		request.SetBasicAuth(r.username, r.password)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, 0, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, 0, fmt.Errorf("POST %s: %s", r.address, response.Status)
	}
	return response.Body, response.ContentLength, nil
}

type scgiResponse struct {
	net.Conn
	stop func() bool
}

func (s *scgiResponse) Close() error {
	s.stop()
	return s.Conn.Close()
}

func writeAll(w io.Writer, data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		n, err := w.Write(data)
		if n < 0 || n > len(data) {
			return written, fmt.Errorf("invalid write count %d", n)
		}
		written += n
		data = data[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrNoProgress
		}
	}
	return written, nil
}

// encode puts the data in scgi format.
func encode(data string) []byte {
	headers := fmt.Sprintf("CONTENT_LENGTH%c%d%cSCGI%c1%c", 0, len(data), 0, 0, 0)
	headers = fmt.Sprintf("%d:%s,", len(headers), headers)
	return append(append(make([]byte, 0, len(headers)+len(data)), headers...), data...)
}
