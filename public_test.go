package rtapi_test

import (
	"testing"

	"github.com/pyed/rtapi"
)

var _ interface {
	Stats() (*rtapi.Stats, error)
	DownloadRaw([]byte, *rtapi.DotTorrentWithOptions) error
	DeleteMetadata(...*rtapi.Torrent) error
	SpeedsWithError() (uint64, uint64, error)
} = (*rtapi.Rtorrent)(nil)

func TestPublicTypesAreNameable(t *testing.T) {
	var sorting rtapi.Sorting = rtapi.ByName
	var stats *rtapi.Stats
	if sorting != rtapi.ByName || stats != nil {
		t.Fatal("unexpected public type values")
	}
}
