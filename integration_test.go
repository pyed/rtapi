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
	multi, multiHash := testTorrent(map[string]any{"name": "multi folder", "private": 1, "files": []any{
		map[string]any{"length": 10, "path": []any{"a.txt"}},
		map[string]any{"length": 20, "path": []any{"sub", "b.txt"}},
	}}, "http://other.invalid/announce")
	for _, data := range [][]byte{single, multi} {
		if err := rt.DownloadRawContext(ctx, data, &DotTorrentWithOptions{Stopped: true, Dir: dir, Label: "rtapi test"}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		rt.DeleteMetadata(&Torrent{Hash: singleHash}, &Torrent{Hash: multiHash})
	})

	waitLoaded(t, rt, singleHash, multiHash)

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
	if s.Name != singleName || s.Size != 1000 || s.MultiFile || s.Label != "rtapi test" || s.State != Stopped || s.Tracker != nil || s.Private {
		t.Errorf("single-file torrent = %+v", s)
	}
	if m.Name != "multi folder" || m.Size != 30 || !m.MultiFile || m.Directory != path.Join(dir, "multi folder") || !m.Private {
		t.Errorf("multi-file torrent = %+v", m)
	}
	if s.Directory != dir {
		t.Errorf("single-file directory = %q, want %q", s.Directory, dir)
	}
	// Torrents loaded stopped have not started yet.
	if s.Age == 0 || s.Started != 0 {
		t.Errorf("loaded stopped: Age = %d, Started = %d; want Age set and Started 0", s.Age, s.Started)
	}
	transfers, err := rt.TransfersContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(transfers, Transfer{Hash: singleHash}) || !slices.Contains(transfers, Transfer{Hash: multiHash}) {
		t.Errorf("Transfers = %+v, want both new torrents, with nothing transferred", transfers)
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

	if err := rt.SetLabelContext(ctx, "rtapi%20relabeled", m); err != nil {
		t.Fatal(err)
	}
	if got, err := rt.GetTorrentContext(ctx, multiHash); err != nil {
		t.Error(err)
	} else if got.Label != "rtapi%20relabeled" {
		t.Errorf("label after SetLabel = %q", got.Label)
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
	if stats, err := rt.StatsContext(ctx); err != nil || stats.PID <= 0 {
		t.Errorf("Stats = %+v, %v; want rTorrent's process ID", stats, err)
	}
	if _, _, err := rt.SpeedsContext(ctx); err != nil {
		t.Error(err)
	}
	// rTorrent learns where a torrent's data is only when it opens the
	// torrent, so until then it reports no free space.
	if free, err := rt.FreeDiskSpaceContext(ctx, singleHash); err != nil || free != 0 {
		t.Errorf("free disk space of an unopened torrent = %d, %v; want 0", free, err)
	}
	if err := rt.StartContext(ctx, s); err != nil {
		t.Fatal(err)
	}
	if free, err := rt.FreeDiskSpaceContext(ctx, singleHash); err != nil || free == 0 {
		t.Errorf("free disk space of a started torrent = %d, %v", free, err)
	}
	if started := waitStarted(t, rt, singleHash); started < s.Age {
		t.Errorf("Started = %d, before Age %d", started, s.Age)
	}
	if free, err := rt.FreeDiskSpacesContext(ctx, singleHash, multiHash); err != nil || len(free) != 2 || free[0] == 0 || free[1] != 0 {
		t.Errorf("free disk space of a started and an unopened torrent = %v, %v", free, err)
	}
	// p.multicall nested in the list works on a real rTorrent; with no
	// peers to reach, there are no connections.
	if count, err := rt.ConnectionsContext(ctx); err != nil || count != (Connections{}) {
		t.Errorf("Connections = %+v, %v", count, err)
	}
	for _, mutate := range []func(context.Context, ...*Torrent) error{rt.StopContext, rt.CheckContext} {
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

// waitLoaded waits for rTorrent to list every one of hashes; it loads
// torrents in the background.
func waitLoaded(t *testing.T, rt *Rtorrent, hashes ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		loaded, err := rt.Hashes()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(hashes, func(hash string) bool { return !slices.Contains(loaded, hash) }) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the torrents did not load; loaded: %v", loaded)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitStarted waits for rTorrent to record that the torrent with hash has
// started, which it does once the torrent's first hash check is done, and
// returns when it started.
func waitStarted(t *testing.T, rt *Rtorrent, hash string) uint64 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		torrent, err := rt.GetTorrent(hash)
		if err != nil {
			t.Fatal(err)
		}
		if torrent.Started != 0 {
			return torrent.Started
		}
		if time.Now().After(deadline) {
			t.Fatalf("rTorrent did not record that %s started", hash)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestIntegrationAcrossRestart checks what an rTorrent restart keeps, in two
// runs that CI makes around one. With RTAPI_TEST_RESTART=before, it loads
// and starts a torrent and leaves it loaded; with after, it checks that
// rTorrent kept when the torrent started but loaded it anew, and removes it.
func TestIntegrationAcrossRestart(t *testing.T) {
	address, phase := os.Getenv("RTAPI_TEST_RTORRENT"), os.Getenv("RTAPI_TEST_RESTART")
	if address == "" || phase == "" {
		t.Skip("set RTAPI_TEST_RTORRENT, and RTAPI_TEST_RESTART to before and then after an rTorrent restart, to run")
	}
	ctx := context.Background()
	rt, err := NewRtorrentContext(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	data, hash := testTorrent(map[string]any{"name": "rtapi restart probe", "length": 1000}, "http://tracker.invalid:6969/announce")
	switch phase {
	case "before":
		if err := rt.DownloadRawContext(ctx, data, &DotTorrentWithOptions{Stopped: true}); err != nil {
			t.Fatal(err)
		}
		waitLoaded(t, rt, hash)
		if err := rt.StartContext(ctx, &Torrent{Hash: hash}); err != nil {
			t.Fatal(err)
		}
		waitStarted(t, rt, hash)
		// Save the torrent's session now, rather than rely on the shutdown.
		if _, err := rt.call(ctx, "d.save_full_session", newStringValue(hash)); err != nil {
			t.Fatal(err)
		}
	case "after":
		torrent, err := rt.GetTorrentContext(ctx, hash)
		if err != nil {
			t.Fatalf("the torrent did not survive the restart: %v", err)
		}
		t.Cleanup(func() { rt.DeleteMetadata(torrent) })
		if torrent.Started == 0 || torrent.Age <= torrent.Started {
			t.Fatalf("after the restart, Started = %d and Age = %d; want Started kept and Age later", torrent.Started, torrent.Age)
		}
	default:
		t.Fatalf("RTAPI_TEST_RESTART = %q; want before or after", phase)
	}
}
