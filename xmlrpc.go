package rtapi

import (
	"bytes"
	"strconv"
	"unicode/utf8"
)

// maxFastDepth bounds how deeply decodeFast nests values. Deeper responses
// fall back to encoding/xml, which enforces its own limit.
const maxFastDepth = 64

// fastSlab is how many numbers, strings, or arrays share one allocation.
const fastSlab = 256

// decodeFast decodes a methodResponse element into what encoding/xml would
// produce, several times faster and with a fraction of the allocations; a
// large library's torrent list is megabytes of XML. It accepts the strict
// subset of XML that rTorrent writes: elements without attributes, whitespace
// between them, and text with entity and character references. On anything
// else it reports false, and the caller decodes with encoding/xml, which also
// reports the errors in malformed responses.
//
// Values decoded here leave Text empty when they have a typed child; only
// untyped values (implicit strings) read it.
func decodeFast(data []byte) (*xmlrpcMethodResponse, bool) {
	p := &fastParser{data: data}
	var resp xmlrpcMethodResponse
	if !p.methodResponse(&resp) {
		return nil, false
	}
	return &resp, true
}

type fastParser struct {
	data  []byte
	pos   int
	depth int
	// Decoded values point into these slabs, so that each number, string, and
	// array does not need an allocation of its own.
	ints    []int64
	strings []string
	arrays  []xmlrpcArray
	// values holds the elements of the arrays being decoded.
	values []xmlrpcValue
}

func (p *fastParser) methodResponse(resp *xmlrpcMethodResponse) bool {
	if name, empty, ok := p.startTag(); !ok || empty || name != "methodResponse" {
		return false
	}
	// Every params element adds to Params, as with encoding/xml; a second
	// fault would merge into the first, so it falls back.
	var seenFault bool
	for {
		p.skipSpace()
		if p.endTag("methodResponse") {
			return true
		}
		name, empty, ok := p.startTag()
		if !ok || empty {
			return false
		}
		switch {
		case name == "params":
			if !p.params(resp) {
				return false
			}
		case name == "fault" && !seenFault:
			seenFault = true
			resp.Fault = new(xmlrpcFaultBody)
			if !p.valueIn("fault", &resp.Fault.Value) {
				return false
			}
		default:
			return false
		}
	}
}

func (p *fastParser) params(resp *xmlrpcMethodResponse) bool {
	for {
		p.skipSpace()
		if p.endTag("params") {
			return true
		}
		if name, empty, ok := p.startTag(); !ok || empty || name != "param" {
			return false
		}
		var param xmlrpcParam
		if !p.valueIn("param", &param.Value) {
			return false
		}
		resp.Params = append(resp.Params, param)
	}
}

// valueIn reads the one value element inside parent, then parent's end tag.
func (p *fastParser) valueIn(parent string, v *xmlrpcValue) bool {
	p.skipSpace()
	name, empty, ok := p.startTag()
	if !ok || name != "value" || (!empty && !p.value(v)) {
		return false
	}
	p.skipSpace()
	return p.endTag(parent)
}

// value reads the content and end tag of a value element.
func (p *fastParser) value(v *xmlrpcValue) bool {
	if p.depth++; p.depth > maxFastDepth {
		return false
	}
	ok := p.valueContent(v)
	p.depth--
	return ok
}

func (p *fastParser) valueContent(v *xmlrpcValue) bool {
	raw, decode, ok := p.scanText()
	if !ok {
		return false
	}
	if p.endTag("value") {
		// A value without a type element is a string.
		v.Text, ok = textString(raw, decode)
		return ok
	}
	if !isSpace(raw) {
		return false
	}
	name, empty, ok := p.startTag()
	if !ok {
		return false
	}
	switch name {
	case "string", "base64":
		var s string
		if !empty {
			if raw, decode, ok = p.scanText(); !ok || !p.endTag(name) {
				return false
			}
			if s, ok = textString(raw, decode); !ok {
				return false
			}
		}
		if name == "string" {
			v.String = p.newString(s)
		} else {
			v.Base64 = p.newString(s)
		}
	case "i4", "i8", "int", "double", "boolean":
		var text []byte
		if !empty {
			if raw, decode, ok = p.scanText(); !ok || !p.endTag(name) {
				return false
			}
			if text, ok = textBytes(raw, decode); !ok {
				return false
			}
		}
		if !p.scalar(v, name, text) {
			return false
		}
	case "array":
		v.Array = p.newArray()
		if !empty && !p.array(v.Array) {
			return false
		}
	case "struct":
		v.Struct = new(xmlrpcStruct)
		if !empty && !p.members(v.Struct) {
			return false
		}
	default:
		return false
	}
	p.skipSpace()
	return p.endTag("value")
}

// scalar sets v's numeric or boolean field from an element's text, parsed the
// way encoding/xml parses it: empty text is the zero value.
func (p *fastParser) scalar(v *xmlrpcValue, name string, text []byte) bool {
	switch name {
	case "double":
		var f float64
		if len(text) > 0 {
			var err error
			if f, err = strconv.ParseFloat(string(bytes.TrimSpace(text)), 64); err != nil {
				return false
			}
		}
		v.Double = &f
	case "boolean":
		var b bool
		if len(text) > 0 {
			var err error
			if b, err = strconv.ParseBool(string(bytes.TrimSpace(text))); err != nil {
				return false
			}
		}
		v.Boolean = &b
	default:
		var n int64
		if len(text) > 0 {
			var err error
			if n, err = strconv.ParseInt(string(bytes.TrimSpace(text)), 10, 64); err != nil {
				return false
			}
		}
		switch name {
		case "i4":
			v.I4 = p.newInt(n)
		case "i8":
			v.I8 = p.newInt(n)
		default:
			v.Int = p.newInt(n)
		}
	}
	return true
}

// array reads the content and end tag of an array element.
func (p *fastParser) array(a *xmlrpcArray) bool {
	p.skipSpace()
	if p.endTag("array") {
		return true
	}
	name, empty, ok := p.startTag()
	if !ok || name != "data" {
		return false
	}
	if !empty {
		// Elements collect on p.values, above those of the arrays that contain
		// this one, and move into a slice of their own once all are read.
		base := len(p.values)
		for {
			p.skipSpace()
			if p.endTag("data") {
				break
			}
			name, empty, ok := p.startTag()
			if !ok || name != "value" {
				return false
			}
			var v xmlrpcValue
			if !empty && !p.value(&v) {
				return false
			}
			p.values = append(p.values, v)
		}
		if n := len(p.values) - base; n > 0 {
			a.Values = make([]xmlrpcValue, n)
			copy(a.Values, p.values[base:])
			p.values = p.values[:base]
		}
	}
	p.skipSpace()
	return p.endTag("array")
}

// members reads the members and end tag of a struct element.
func (p *fastParser) members(s *xmlrpcStruct) bool {
	for {
		p.skipSpace()
		if p.endTag("struct") {
			return true
		}
		if name, empty, ok := p.startTag(); !ok || empty || name != "member" {
			return false
		}
		p.skipSpace()
		name, empty, ok := p.startTag()
		if !ok || name != "name" {
			return false
		}
		var member xmlrpcMember
		if !empty {
			raw, decode, ok := p.scanText()
			if !ok || !p.endTag("name") {
				return false
			}
			if member.Name, ok = textString(raw, decode); !ok {
				return false
			}
		}
		if !p.valueIn("member", &member.Value) {
			return false
		}
		s.Members = append(s.Members, member)
	}
}

// startTag reads a start tag without attributes and returns the element's
// name, which is one of the XML-RPC names decodeFast handles, and whether the
// tag is an empty-element tag such as <string/>.
func (p *fastParser) startTag() (name string, empty, ok bool) {
	d, i := p.data, p.pos
	if i >= len(d) || d[i] != '<' {
		return "", false, false
	}
	i++
	start := i
	for i < len(d) && ('a' <= d[i] && d[i] <= 'z' || 'A' <= d[i] && d[i] <= 'Z' || '0' <= d[i] && d[i] <= '9') {
		i++
	}
	if name, ok = elementName(d[start:i]); !ok || i >= len(d) {
		return "", false, false
	}
	switch {
	case d[i] == '>':
		i++
	case d[i] == '/' && i+1 < len(d) && d[i+1] == '>':
		empty = true
		i += 2
	default:
		return "", false, false
	}
	p.pos = i
	return name, empty, true
}

// elementName returns the XML-RPC element called b without allocating.
func elementName(b []byte) (string, bool) {
	switch string(b) {
	case "value":
		return "value", true
	case "string":
		return "string", true
	case "i8":
		return "i8", true
	case "i4":
		return "i4", true
	case "int":
		return "int", true
	case "array":
		return "array", true
	case "data":
		return "data", true
	case "double":
		return "double", true
	case "boolean":
		return "boolean", true
	case "base64":
		return "base64", true
	case "struct":
		return "struct", true
	case "member":
		return "member", true
	case "name":
		return "name", true
	case "param":
		return "param", true
	case "params":
		return "params", true
	case "fault":
		return "fault", true
	case "methodResponse":
		return "methodResponse", true
	}
	return "", false
}

// endTag reads the end tag of element name if it is next.
func (p *fastParser) endTag(name string) bool {
	d := p.data[p.pos:]
	n := len(name)
	if len(d) < n+3 || d[0] != '<' || d[1] != '/' || string(d[2:2+n]) != name || d[2+n] != '>' {
		return false
	}
	p.pos += n + 3
	return true
}

func (p *fastParser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func isSpace(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}
	return true
}

// scanText reads character data up to the next '<' and checks that encoding/xml
// would accept it. decode reports whether it has references or carriage
// returns, which textBytes and textString then decode.
func (p *fastParser) scanText() (raw []byte, decode, ok bool) {
	d := p.data
	start := p.pos
	for i := start; i < len(d); {
		c := d[i]
		switch {
		case c == '<':
			p.pos = i
			return d[start:i], decode, true
		case c == '&' || c == '\r':
			decode = true
			i++
		case c == ']':
			if i+2 < len(d) && d[i+1] == ']' && d[i+2] == '>' {
				return nil, false, false
			}
			i++
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRune(d[i:])
			if r == utf8.RuneError && size == 1 || !isInCharacterRange(r) {
				return nil, false, false
			}
			i += size
		case c < 0x20 && c != '\t' && c != '\n':
			return nil, false, false
		default:
			i++
		}
	}
	return nil, false, false
}

func textString(raw []byte, decode bool) (string, bool) {
	if !decode {
		return string(raw), true
	}
	text, ok := decodeText(raw)
	return string(text), ok
}

func textBytes(raw []byte, decode bool) ([]byte, bool) {
	if !decode {
		return raw, true
	}
	return decodeText(raw)
}

// decodeText replaces references in raw with the text they stand for and line
// endings with '\n', as encoding/xml does, then checks the result for
// characters XML does not allow.
func decodeText(raw []byte) ([]byte, bool) {
	text := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		switch c := raw[i]; c {
		case '\r':
			text = append(text, '\n')
			i++
			if i < len(raw) && raw[i] == '\n' {
				i++
			}
		case '&':
			end := bytes.IndexByte(raw[i:], ';')
			if end < 0 {
				return nil, false
			}
			replacement, ok := reference(raw[i+1 : i+end])
			if !ok {
				return nil, false
			}
			text = append(text, replacement...)
			i += end + 1
		default:
			text = append(text, c)
			i++
		}
	}
	for rest := text; len(rest) > 0; {
		r, size := utf8.DecodeRune(rest)
		if r == utf8.RuneError && size == 1 || !isInCharacterRange(r) {
			return nil, false
		}
		rest = rest[size:]
	}
	return text, true
}

// reference returns the text an entity or character reference (without its
// '&' and ';') stands for, for the references encoding/xml knows.
func reference(name []byte) (string, bool) {
	switch string(name) {
	case "lt":
		return "<", true
	case "gt":
		return ">", true
	case "amp":
		return "&", true
	case "apos":
		return "'", true
	case "quot":
		return `"`, true
	}
	if len(name) < 2 || name[0] != '#' {
		return "", false
	}
	digits, base := name[1:], 10
	if digits[0] == 'x' {
		digits, base = digits[1:], 16
	}
	if len(digits) == 0 {
		return "", false
	}
	for _, c := range digits {
		if !('0' <= c && c <= '9' || base == 16 && ('a' <= c && c <= 'f' || 'A' <= c && c <= 'F')) {
			return "", false
		}
	}
	n, err := strconv.ParseUint(string(digits), base, 64)
	if err != nil || n > utf8.MaxRune {
		return "", false
	}
	return string(rune(n)), true
}

// isInCharacterRange reports whether XML allows r, as encoding/xml checks.
func isInCharacterRange(r rune) bool {
	return r == 0x09 ||
		r == 0x0A ||
		r == 0x0D ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

func (p *fastParser) newInt(n int64) *int64 {
	if len(p.ints) == cap(p.ints) {
		p.ints = make([]int64, 0, fastSlab)
	}
	p.ints = append(p.ints, n)
	return &p.ints[len(p.ints)-1]
}

func (p *fastParser) newString(s string) *string {
	if len(p.strings) == cap(p.strings) {
		p.strings = make([]string, 0, fastSlab)
	}
	p.strings = append(p.strings, s)
	return &p.strings[len(p.strings)-1]
}

func (p *fastParser) newArray() *xmlrpcArray {
	if len(p.arrays) == cap(p.arrays) {
		p.arrays = make([]xmlrpcArray, 0, fastSlab)
	}
	p.arrays = append(p.arrays, xmlrpcArray{})
	return &p.arrays[len(p.arrays)-1]
}
