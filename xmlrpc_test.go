package rtapi

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

// canonical renders a decoded value the way the parsing code reads it: Text
// matters only for a value without a type element.
func canonical(v xmlrpcValue) string {
	var parts []string
	if v.String != nil {
		parts = append(parts, fmt.Sprintf("string %q", *v.String))
	}
	if v.Base64 != nil {
		parts = append(parts, fmt.Sprintf("base64 %q", *v.Base64))
	}
	if v.Int != nil {
		parts = append(parts, fmt.Sprintf("int %d", *v.Int))
	}
	if v.I4 != nil {
		parts = append(parts, fmt.Sprintf("i4 %d", *v.I4))
	}
	if v.I8 != nil {
		parts = append(parts, fmt.Sprintf("i8 %d", *v.I8))
	}
	if v.Double != nil {
		parts = append(parts, fmt.Sprintf("double %v", *v.Double))
	}
	if v.Boolean != nil {
		parts = append(parts, fmt.Sprintf("boolean %v", *v.Boolean))
	}
	if v.Array != nil {
		items := make([]string, len(v.Array.Values))
		for i, item := range v.Array.Values {
			items[i] = canonical(item)
		}
		parts = append(parts, "array ["+strings.Join(items, ", ")+"]")
	}
	if v.Struct != nil {
		members := make([]string, len(v.Struct.Members))
		for i, member := range v.Struct.Members {
			members[i] = fmt.Sprintf("%q: %s", member.Name, canonical(member.Value))
		}
		parts = append(parts, "struct {"+strings.Join(members, ", ")+"}")
	}
	if len(parts) == 0 {
		return fmt.Sprintf("text %q", v.Text)
	}
	return strings.Join(parts, " | ")
}

func canonicalResponse(resp *xmlrpcMethodResponse) string {
	var out strings.Builder
	for _, param := range resp.Params {
		out.WriteString("param " + canonical(param.Value) + "\n")
	}
	if resp.Fault != nil {
		out.WriteString("fault " + canonical(resp.Fault.Value) + "\n")
	}
	return out.String()
}

// checkDecodeFast fails when decodeFast accepts data and decodes it
// differently from encoding/xml, or accepts what encoding/xml rejects.
func checkDecodeFast(t *testing.T, data string) (fast bool) {
	t.Helper()
	got, ok := decodeFast([]byte(data))
	if !ok {
		return false
	}
	var want xmlrpcMethodResponse
	if err := xml.Unmarshal([]byte(data), &want); err != nil {
		t.Fatalf("decodeFast accepted what encoding/xml rejects (%v):\n%q", err, data)
	}
	if g, w := canonicalResponse(got), canonicalResponse(&want); g != w {
		t.Fatalf("decodeFast disagrees with encoding/xml on %q:\nfast: %s\nxml:  %s", data, g, w)
	}
	return true
}

func wrapParams(value string) string {
	return "<methodResponse><params><param><value>" + value + "</value></param></params></methodResponse>"
}

var decodeSeeds = []string{
	wrapParams("<string>plain</string>"),
	wrapParams("implicit text"),
	wrapParams(""),
	wrapParams("<string/>"),
	wrapParams("<string>a &amp; b &lt;c&gt; &quot;d&quot; &apos;e&apos;</string>"),
	wrapParams("<string>&#65;&#x42;&#X43;</string>"),
	wrapParams("<string>&#0;</string>"),
	wrapParams("<string>&#xD800;</string>"),
	wrapParams("<string>&#x110000;</string>"),
	wrapParams("<string>&unknown;</string>"),
	wrapParams("<string>&amp</string>"),
	wrapParams("<string>line\r\nbreak\rend\r&#13;\n</string>"),
	wrapParams("<string>tab\there</string>"),
	wrapParams("<string>bell\x07</string>"),
	wrapParams("<string>Ünïcødé 日本語 🎬</string>"),
	wrapParams("<string>bad \xff utf8</string>"),
	wrapParams("<string>\uFFFE</string>"),
	wrapParams("<string>a]]>b</string>"),
	wrapParams("<string>[Group] a]b]]</string>"),
	wrapParams("<string><![CDATA[x]]></string>"),
	wrapParams("<string>a<!-- c -->b</string>"),
	wrapParams("<i8>9223372036854775807</i8>"),
	wrapParams("<i8>9223372036854775808</i8>"),
	wrapParams("<i4> -12 </i4>"),
	wrapParams("<int>&#49;2</int>"),
	wrapParams("<i8></i8>"),
	wrapParams("<i8/>"),
	wrapParams("<i8> </i8>"),
	wrapParams("<i8>\u00a07\u00a0</i8>"),
	wrapParams("<i8>1e3</i8>"),
	wrapParams("<double>1.5e3</double>"),
	wrapParams("<double>NaN</double>"),
	wrapParams("<boolean>1</boolean>"),
	wrapParams("<boolean>true</boolean>"),
	wrapParams("<boolean>yes</boolean>"),
	wrapParams("<base64>aGk=</base64>"),
	wrapParams("<array><data><value><i8>1</i8></value><value>two</value><value/></data></array>"),
	wrapParams("<array><data/></array>"),
	wrapParams("<array/>"),
	wrapParams("<array></array>"),
	wrapParams("<array><data></data><data><value>x</value></data></array>"),
	wrapParams("<array><data><value><array><data><value><string>deep</string></value></data></array></value></data></array>"),
	wrapParams(" \r\n <string>spaced</string> \r\n "),
	wrapParams("text<string>mixed</string>"),
	wrapParams("<string>a</string><string>b</string>"),
	wrapParams("<nil/>"),
	wrapParams("<ex:nil/>"),
	wrapParams(`<string attr="1">x</string>`),
	wrapParams("<string >x</string>"),
	wrapParams("<string>x</string >"),
	wrapParams("<struct><member><name>k</name><value><i4>1</i4></value></member></struct>"),
	wrapParams("<struct/>"),
	wrapParams("<struct><member><value><i4>1</i4></value><name>k</name></member></struct>"),
	wrapParams("<string>unclosed"),
	wrapParams("<string>x</value>"),
	wrapParams(strings.Repeat("<array><data><value>", 70) + "x" + strings.Repeat("</value></data></array>", 70)),
	"<methodResponse><fault><value><struct><member><name>faultCode</name><value><int>-501</int></value></member><member><name>faultString</name><value><string>Could not find info-hash.</string></value></member></struct></value></fault></methodResponse>",
	"<methodResponse><params><param><value><i8>1</i8></value></param><param><value>2</value></param></params></methodResponse>",
	"<methodResponse><params></params></methodResponse>",
	"<methodResponse/>",
	"<methodResponse><params><param><value>a</value></param></params><params><param><value>b</value></param></params></methodResponse>",
	"<methodResponse xmlns=\"x\"><params/></methodResponse>",
	"<methodResponse><params><param><value>x</value></param></params></methodResponse>trailing",
	"<methodResponse><params><param><value>x</value></param></params>",
	"<methodResponseX><params/></methodResponseX>",
}

func TestDecodeFastMatchesEncodingXML(t *testing.T) {
	var fast int
	for _, seed := range decodeSeeds {
		if checkDecodeFast(t, seed) {
			fast++
		}
	}
	if !checkDecodeFast(t, benchListResponse(50)[strings.Index(benchListResponse(50), "<methodResponse"):]) {
		t.Fatal("decodeFast rejected an rTorrent-style torrent list")
	}
	// Guard against a decodeFast that agrees by rejecting everything.
	if fast < len(decodeSeeds)/2 {
		t.Fatalf("decodeFast accepted only %d of %d seeds", fast, len(decodeSeeds))
	}
	// What rTorrent writes must take the fast path.
	for _, data := range []string{
		wrapParams("<string>plain</string>"),
		wrapParams("implicit text"),
		wrapParams("<string/>"),
		wrapParams("<string>a &amp; b &lt;c&gt; &quot;d&quot; &apos;e&apos;</string>"),
		wrapParams("<string>&#65;&#x42;</string>"),
		wrapParams("<string>line\r\nbreak</string>"),
		wrapParams("<string>Ünïcødé 日本語 🎬</string>"),
		wrapParams("<string>[Group] a]b]]</string>"),
		wrapParams("<i8>-9223372036854775808</i8>"),
		wrapParams("<i4>7</i4>"),
		wrapParams("<int>7</int>"),
		wrapParams("<double>1.5</double>"),
		wrapParams("<boolean>0</boolean>"),
		wrapParams("<base64>aGk=</base64>"),
		wrapParams(" \r\n <array><data>\r\n<value><array><data><value><i8>1</i8></value></data></array></value>\r\n</data></array> "),
		"<methodResponse><fault><value><struct><member><name>faultCode</name><value><i8>-506</i8></value></member>" +
			"<member><name>faultString</name><value><string>method 'x' not defined</string></value></member></struct></value></fault></methodResponse>",
	} {
		if _, ok := decodeFast([]byte(data)); !ok {
			t.Errorf("decodeFast fell back for %q", data)
		}
	}
}

func TestDecodeFastFallsBackForUnusualXML(t *testing.T) {
	for _, data := range []string{
		wrapParams("<nil/>"),
		wrapParams("<string><![CDATA[x]]></string>"),
		wrapParams(`<string attr="1">x</string>`),
		wrapParams("text<string>mixed</string>"),
		wrapParams(strings.Repeat("<array><data><value>", maxFastDepth+1) + "x" + strings.Repeat("</value></data></array>", maxFastDepth+1)),
	} {
		if _, ok := decodeFast([]byte(data)); ok {
			t.Errorf("decodeFast accepted %q", data)
		}
		resp, err := decodeMethodResponse(strings.NewReader(data), 0)
		if err != nil {
			t.Errorf("decodeMethodResponse(%q): %v", data, err)
		} else if len(resp.Params) != 1 {
			t.Errorf("decodeMethodResponse(%q) = %+v", data, resp)
		}
	}
}

func FuzzDecodeFast(f *testing.F) {
	for _, seed := range decodeSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		checkDecodeFast(t, data)
	})
}

// TestMarshalMethodCallMatchesEncodingXML checks the hand-written request
// encoder against encoding/xml, byte for byte.
func TestMarshalMethodCallMatchesEncodingXML(t *testing.T) {
	text := func(s string) *string { return &s }
	number := func(n int64) *int64 { return &n }
	double, yes := 1.5e-7, true
	awkward := "a&b<c>d\"e'f\tg\nh\ri\x01j\xffk l日本"
	requests := []xmlrpcMethodCall{
		{MethodName: "system.client_version"},
		{MethodName: "d.multicall2", Params: []xmlrpcParam{newStringParam(""), newStringParam("main"), newStringParam("d.name=")}},
		{MethodName: "load.raw_start", Params: []xmlrpcParam{newStringParam(""), newBase64Param([]byte("d8:announce"))}},
		{MethodName: awkward, Params: []xmlrpcParam{newStringParam(awkward), {Value: xmlrpcValue{Text: awkward}}}},
		{MethodName: "system.multicall", Params: []xmlrpcParam{{Value: newArrayValue(
			newMethodCall("t.url", testHash+":t0"),
			newMethodCallValues("f.priority.set", newStringValue(testHash+":f0"), newIntValue(-2)),
			newArrayValue(),
			xmlrpcValue{Struct: &xmlrpcStruct{}},
			xmlrpcValue{Int: number(1), I4: number(-4), Double: &double, Boolean: &yes, String: text("both"), Base64: text("x")},
		)}}},
	}
	for _, special := range []string{`"`, "'", "&", "<", ">", "\t", "\n", "\r", "\x01", "\x7f", "\x80", "\ufffe"} {
		requests = append(requests, xmlrpcMethodCall{MethodName: "m" + special, Params: []xmlrpcParam{newStringParam("a" + special + "b")}})
	}
	for _, request := range requests {
		got, err := marshalMethodCall(request)
		if err != nil {
			t.Fatal(err)
		}
		want, err := xml.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if got != xml.Header+string(want) {
			t.Errorf("marshalMethodCall(%s):\n got %q\nwant %q", request.MethodName, got, xml.Header+string(want))
		}
	}
}
