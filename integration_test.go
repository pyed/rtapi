package rtapi

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// bencode encodes strings, ints, lists, and dictionaries, for test torrents.
func bencode(value any) []byte {
	var out bytes.Buffer
	var encode func(any)
	encode = func(value any) {
		switch v := value.(type) {
		case string:
			fmt.Fprintf(&out, "%d:%s", len(v), v)
		case int:
			fmt.Fprintf(&out, "i%de", v)
		case []any:
			out.WriteByte('l')
			for _, item := range v {
				encode(item)
			}
			out.WriteByte('e')
		case map[string]any:
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			out.WriteByte('d')
			for _, key := range keys {
				encode(key)
				encode(v[key])
			}
			out.WriteByte('e')
		default:
			panic(fmt.Sprintf("bencode: %T", value))
		}
	}
	encode(value)
	return out.Bytes()
}

// testTorrent returns a .torrent file for info, and its info-hash.
func testTorrent(info map[string]any, announce string) ([]byte, string) {
	info["piece length"] = 16384
	info["pieces"] = strings.Repeat("\x01", 20)
	sum := sha1.Sum(bencode(info))
	metainfo := map[string]any{"info": info}
	if announce != "" {
		metainfo["announce"] = announce
	}
	return bencode(metainfo), strings.ToUpper(hex.EncodeToString(sum[:]))
}

// TestIntegrationAgainstRTorrent runs the client against a real rTorrent,
// whose SCGI address RTAPI_TEST_RTORRENT gives; CI starts one. It loads two
// stopped torrents, uses every call, and removes them again.
func TestIntegrationAgainstRTorrent(t *testing.T) {
	address := os.Getenv("RTAPI_TEST_RTORRENT")
	if address == "" {
		t.Skip("set RTAPI_TEST_RTORRENT to an rTorrent SCGI address to run")
	}
	ctx := context.Background()
	rt, err := NewRtorrentContext(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rTorrent %s", rt.Version)
	dir := t.TempDir()

	singleName := `Ünïcødé & <single> "file" 日本.bin`
	single, singleHash := testTorrent(map[string]any{"name": singleName, "length": 1000}, "http://tracker.invalid:6969/announce")
	multi, multiHash := testTorrent(map[string]any{"name": "multi folder", "files": []any{
		map[string]any{"length": 10, "path": []any{"a.txt"}},
		map[string]any{"length": 20, "path": []any{"sub", "b.txt"}},
	}}, "")
	for _, data := range [][]byte{single, multi} {
		if err := rt.DownloadRawContext(ctx, data, &DotTorrentWithOptions{Stopped: true, Dir: dir, Label: "rtapi test"}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		rt.DeleteMetadata(&Torrent{Hash: singleHash}, &Torrent{Hash: multiHash})
	})

	// rTorrent loads in the background.
	deadline := time.Now().Add(10 * time.Second)
	for {
		hashes, err := rt.HashesContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(hashes, singleHash) && slices.Contains(hashes, multiHash) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the torrents did not load; loaded: %v", hashes)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// rTorrent's own responses take the fast decoding path.
	req, err := buildTorrentsRequest()
	if err != nil {
		t.Fatal(err)
	}
	body, err := rt.send(ctx, encode(req))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := readResponse(body, DefaultMaxResponseSize, -1)
	body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decodeFast(payload[bytes.Index(payload, []byte("<methodResponse")):]); !ok {
		t.Fatalf("rTorrent's torrent list falls back to encoding/xml:\n%.2000s", payload)
	}

	torrents, err := rt.ListContext(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	find := func(torrents Torrents, hash string) *Torrent {
		t.Helper()
		for _, torrent := range torrents {
			if torrent.Hash == hash {
				return torrent
			}
		}
		t.Fatalf("torrent %s is missing", hash)
		return nil
	}
	s, m := find(torrents, singleHash), find(torrents, multiHash)
	if s.Name != singleName || s.Size != 1000 || s.MultiFile || s.Label != "rtapi test" || s.State != Stopped || s.Tracker != nil {
		t.Errorf("single-file torrent = %+v", s)
	}
	if m.Name != "multi folder" || m.Size != 30 || !m.MultiFile || m.Directory != path.Join(dir, "multi folder") {
		t.Errorf("multi-file torrent = %+v", m)
	}
	if s.Directory != dir {
		t.Errorf("single-file directory = %q, want %q", s.Directory, dir)
	}
	if err := rt.TrackersContext(ctx, Torrents{s, m}); err != nil {
		t.Fatal(err)
	}
	if s.Tracker == nil || s.Tracker.Host != "tracker.invalid:6969" {
		t.Errorf("tracker = %v", s.Tracker)
	}
	all, err := rt.TorrentsContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := find(all, singleHash); got.Tracker == nil || got.Name != singleName {
		t.Errorf("Torrents gave %+v", got)
	}
	got, err := rt.GetTorrentContext(ctx, singleHash)
	if err != nil || got.Name != singleName || got.Tracker == nil {
		t.Errorf("GetTorrent = %+v, %v", got, err)
	}
	if _, err := rt.GetTorrentContext(ctx, strings.Repeat("0", 40)); err == nil {
		t.Error("GetTorrent found a torrent that is not loaded")
	}

	files, err := rt.FilesContext(ctx, multiHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "a.txt" || files[1].Path != "sub/b.txt" || files[1].Size != 20 || files[0].Priority != FileNormal {
		t.Fatalf("files = %+v", files)
	}
	if err := rt.SetFilePrioritiesContext(ctx, multiHash, map[int]FilePriority{0: FileSkip, 1: FileHigh}); err != nil {
		t.Fatal(err)
	}
	if files, err = rt.FilesContext(ctx, multiHash); err != nil || files[0].Priority != FileSkip || files[1].Priority != FileHigh {
		t.Fatalf("files after setting priorities = %+v, %v", files, err)
	}

	down, up, err := rt.GlobalLimitsContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.SetGlobalLimitsContext(ctx, 3<<20, 1<<20); err != nil {
		t.Fatal(err)
	}
	if newDown, newUp, err := rt.GlobalLimitsContext(ctx); err != nil || newDown != 3<<20 || newUp != 1<<20 {
		t.Errorf("limits = %d, %d, %v", newDown, newUp, err)
	}
	if err := rt.SetGlobalLimitsContext(ctx, down, up); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.StatsContext(ctx); err != nil {
		t.Error(err)
	}
	if _, _, err := rt.SpeedsContext(ctx); err != nil {
		t.Error(err)
	}
	if free, err := rt.FreeDiskSpaceContext(ctx, singleHash); err != nil || free == 0 {
		t.Errorf("free disk space = %d, %v", free, err)
	}

	for _, mutate := range []func(context.Context, ...*Torrent) error{rt.StartContext, rt.StopContext, rt.CheckContext} {
		if err := mutate(ctx, s); err != nil {
			t.Error(err)
		}
	}
	if err := rt.DeleteMetadataContext(ctx, s, m); err != nil {
		t.Fatal(err)
	}
	hashes, err := rt.HashesContext(ctx)
	if err != nil || slices.Contains(hashes, singleHash) || slices.Contains(hashes, multiHash) {
		t.Fatalf("after DeleteMetadata, hashes = %v, %v", hashes, err)
	}
}
