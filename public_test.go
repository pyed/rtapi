package rtapi_test

import (
	"context"
	"testing"

	"github.com/pyed/rtapi"
)

var _ interface {
	Stats() (*rtapi.Stats, error)
	DownloadRaw([]byte, *rtapi.DotTorrentWithOptions) error
	DeleteMetadata(...*rtapi.Torrent) error
	SpeedsWithError() (uint64, uint64, error)

	TorrentsContext(context.Context) (rtapi.Torrents, error)
	List(rtapi.ListOptions) (rtapi.Torrents, error)
	ListContext(context.Context, rtapi.ListOptions) (rtapi.Torrents, error)
	Trackers(rtapi.Torrents) error
	TrackersContext(context.Context, rtapi.Torrents) error
	Hashes() ([]string, error)
	HashesContext(context.Context) ([]string, error)
	GetTorrentContext(context.Context, string) (*rtapi.Torrent, error)
	DownloadContext(context.Context, string) error
	DownloadWithOptionsContext(context.Context, *rtapi.DotTorrentWithOptions) error
	DownloadRawContext(context.Context, []byte, *rtapi.DotTorrentWithOptions) error
	StartContext(context.Context, ...*rtapi.Torrent) error
	StopContext(context.Context, ...*rtapi.Torrent) error
	CheckContext(context.Context, ...*rtapi.Torrent) error
	DeleteMetadataContext(context.Context, ...*rtapi.Torrent) error
	SpeedsContext(context.Context) (uint64, uint64, error)
	StatsContext(context.Context) (*rtapi.Stats, error)

	FilesContext(context.Context, string) ([]rtapi.File, error)
	SetFilePrioritiesContext(context.Context, string, map[int]rtapi.FilePriority) error
	GlobalLimitsContext(context.Context) (uint64, uint64, error)
	SetGlobalLimitsContext(context.Context, uint64, uint64) error
	FreeDiskSpaceContext(context.Context, string) (uint64, error)
} = (*rtapi.Rtorrent)(nil)

var _ func(context.Context, string) (*rtapi.Rtorrent, error) = rtapi.NewRtorrentContext

func TestPublicTypesAreNameable(t *testing.T) {
	var sorting rtapi.Sorting = rtapi.ByName
	var stats *rtapi.Stats
	if sorting != rtapi.ByName || stats != nil {
		t.Fatal("unexpected public type values")
	}
}
