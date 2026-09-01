package rtapi

import (
	"cmp"
	"slices"
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

// Sort orders torrents in place.
func (t Torrents) Sort(aSorting Sorting) {
	switch aSorting {
	case ByName, ByNameRev:
		slices.SortFunc(t, func(a, b *Torrent) int { return cmp.Compare(a.Name, b.Name) })
	case ByDownRate, ByDownRateRev:
		slices.SortFunc(t, func(a, b *Torrent) int { return cmp.Compare(a.DownRate, b.DownRate) })
	case ByUpRate, ByUpRateRev:
		slices.SortFunc(t, func(a, b *Torrent) int { return cmp.Compare(a.UpRate, b.UpRate) })
	case BySize, BySizeRev:
		slices.SortFunc(t, func(a, b *Torrent) int { return cmp.Compare(a.Size, b.Size) })
	case ByRatio, ByRatioRev:
		slices.SortFunc(t, func(a, b *Torrent) int { return cmp.Compare(a.Ratio, b.Ratio) })
	case ByAge, ByAgeRev:
		slices.SortFunc(t, func(a, b *Torrent) int { return cmp.Compare(a.Age, b.Age) })
	case ByUpTotal, ByUpTotalRev:
		slices.SortFunc(t, func(a, b *Torrent) int { return cmp.Compare(a.UpTotal, b.UpTotal) })
	default:
		return
	}

	switch aSorting {
	case ByNameRev, ByDownRateRev, ByUpRateRev, BySizeRev, ByRatioRev, ByAgeRev, ByUpTotalRev:
		slices.Reverse(t)
	}
}
