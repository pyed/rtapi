package rtapi

import (
	"cmp"
	"slices"
	"strings"
)

// Sorting selects a Torrent field and direction.
type Sorting int8

const (
	DefaultSorting Sorting = iota
	ByName
	ByNameRev
	ByDownRate
	ByDownRateRev
	ByUpRate
	ByUpRateRev
	BySize
	BySizeRev
	ByRatio
	ByRatioRev
	ByAge
	ByAgeRev
	ByUpTotal
	ByUpTotalRev
)

// Sort orders torrents in place. Names compare case-insensitively, and the
// sort is stable: torrents that compare equal keep their order, in reverse
// sorts too.
func (t Torrents) Sort(aSorting Sorting) {
	var compare func(a, b *Torrent) int
	switch aSorting {
	case ByName, ByNameRev:
		compare = func(a, b *Torrent) int { return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) }
	case ByDownRate, ByDownRateRev:
		compare = func(a, b *Torrent) int { return cmp.Compare(a.DownRate, b.DownRate) }
	case ByUpRate, ByUpRateRev:
		compare = func(a, b *Torrent) int { return cmp.Compare(a.UpRate, b.UpRate) }
	case BySize, BySizeRev:
		compare = func(a, b *Torrent) int { return cmp.Compare(a.Size, b.Size) }
	case ByRatio, ByRatioRev:
		compare = func(a, b *Torrent) int { return cmp.Compare(a.Ratio, b.Ratio) }
	case ByAge, ByAgeRev:
		compare = func(a, b *Torrent) int { return cmp.Compare(a.Age, b.Age) }
	case ByUpTotal, ByUpTotalRev:
		compare = func(a, b *Torrent) int { return cmp.Compare(a.UpTotal, b.UpTotal) }
	default:
		return
	}

	switch aSorting {
	case ByNameRev, ByDownRateRev, ByUpRateRev, BySizeRev, ByRatioRev, ByAgeRev, ByUpTotalRev:
		ascending := compare
		compare = func(a, b *Torrent) int { return ascending(b, a) }
	}
	slices.SortStableFunc(t, compare)
}
