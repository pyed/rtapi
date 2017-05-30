package rtapi

import "sort"

// Sorting method on Torrents

type sorting int8

const (
	DefaultSorting sorting = iota
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

// CurrentSorting holds the sorting in use.
var CurrentSorting = DefaultSorting

func (t Torrents) Sort(aSorting sorting) {
	switch aSorting {
	case ByName:
		sort.Sort(byName(t))
	case ByNameRev:
		sort.Sort(sort.Reverse(byName(t)))
	case ByDownRate:
		sort.Sort(byDownRate(t))
	case ByDownRateRev:
		sort.Sort(sort.Reverse(byDownRate(t)))
	case ByUpRate:
		sort.Sort(byUpRate(t))
	case ByUpRateRev:
		sort.Sort(sort.Reverse(byUpRate(t)))
	case BySize:
		sort.Sort(bySize(t))
	case BySizeRev:
		sort.Sort(sort.Reverse(bySize(t)))
	case ByRatio:
		sort.Sort(byRatio(t))
	case ByRatioRev:
		sort.Sort(sort.Reverse(byRatio(t)))
	case ByAge:
		sort.Sort(byAge(t))
	case ByAgeRev:
		sort.Sort(sort.Reverse(byAge(t)))
	case ByUpTotal:
		sort.Sort(byUpTotal(t))
	case ByUpTotalRev:
		sort.Sort(sort.Reverse(byUpTotal(t)))
	}
}

type byName Torrents

func (s byName) Len() int           { return len(s) }
func (s byName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byName) Less(i, j int) bool { return s[i].Name < s[j].Name }

type byDownRate Torrents

func (s byDownRate) Len() int           { return len(s) }
func (s byDownRate) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byDownRate) Less(i, j int) bool { return s[i].DownRate < s[j].DownRate }

type byUpRate Torrents

func (s byUpRate) Len() int           { return len(s) }
func (s byUpRate) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byUpRate) Less(i, j int) bool { return s[i].UpRate < s[j].UpRate }

type bySize Torrents

func (s bySize) Len() int           { return len(s) }
func (s bySize) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s bySize) Less(i, j int) bool { return s[i].Size < s[j].Size }

type byRatio Torrents

func (s byRatio) Len() int           { return len(s) }
func (s byRatio) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byRatio) Less(i, j int) bool { return s[i].Ratio < s[j].Ratio }

type byAge Torrents

func (s byAge) Len() int           { return len(s) }
func (s byAge) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byAge) Less(i, j int) bool { return s[i].Age < s[j].Age }

type byUpTotal Torrents

func (s byUpTotal) Len() int           { return len(s) }
func (s byUpTotal) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byUpTotal) Less(i, j int) bool { return s[i].UpTotal < s[j].UpTotal }
