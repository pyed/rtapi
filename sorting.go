package rtapi

import "sort"

// Sorting method on Torrents

type Sorting int8

const (
	Name Sorting = iota
	NameRev
	DownRate
	DownRateRev
	UpRate
	UpRateRev
	Size
	SizeRev
	Ratio
	RatioRev
	Age
	AgeRev
	UpTotal
	UpTotalRev
)

func (t Torrents) Sort(sorting Sorting) {
	switch sorting {
	case Name:
		sort.Sort(byName(t))
	case NameRev:
		sort.Reverse(byName(t))
	case DownRate:
		sort.Sort(byDownRate(t))
	case DownRateRev:
		sort.Reverse(byDownRate(t))
	case UpRate:
		sort.Sort(byUpRate(t))
	case UpRateRev:
		sort.Reverse(byUpRate(t))
	case Size:
		sort.Sort(bySize(t))
	case SizeRev:
		sort.Reverse(bySize(t))
	case Ratio:
		sort.Sort(byRatio(t))
	case RatioRev:
		sort.Reverse(byRatio(t))
	case Age:
		sort.Sort(byAge(t))
	case AgeRev:
		sort.Reverse(byAge(t))
	case UpTotal:
		sort.Sort(byUpTotal(t))
	case UpTotalRev:
		sort.Reverse(byUpTotal(t))
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
