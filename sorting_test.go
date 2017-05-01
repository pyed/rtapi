package rtapi

import (
	"testing"
)

func TestSorting(t *testing.T) {
	torrents := Torrents{
		&Torrent{
			Name:     "Debian",
			DownRate: 9,
			UpRate:   79,
			Size:     1024,
			Ratio:    2.3,
			Age:      1492021111,
			UpTotal:  93413,
		},
		&Torrent{
			Name:     "Ubuntu",
			DownRate: 33,
			UpRate:   19,
			Size:     4048,
			Ratio:    0.3,
			Age:      1492929111,
			UpTotal:  993413,
		},
		&Torrent{
			Name:     "Archlinux",
			DownRate: 3300,
			UpRate:   1,
			Size:     448,
			Ratio:    9.3,
			Age:      1492929977,
			UpTotal:  9176445,
		},
	}

	torrents.Sort(ByName)
	if torrents[0].Name != "Archlinux" ||
		torrents[2].Name != "Ubuntu" {
		t.Errorf("byName: Expected: 'Archlinux', 'Debian', 'Ubuntu', got: '%s', '%s', '%s'",
			torrents[0].Name, torrents[1].Name, torrents[2].Name)
	}

	torrents.Sort(ByDownRate)
	if torrents[0].DownRate != 9 ||
		torrents[2].DownRate != 3300 {
		t.Errorf("byDownRate: Expected: 9, 33, 3300, got: %d, %d, %d",
			torrents[0].DownRate, torrents[1].DownRate, torrents[2].DownRate)
	}

	torrents.Sort(ByUpRate)
	if torrents[0].UpRate != 1 ||
		torrents[2].UpRate != 79 {
		t.Errorf("byUpRate: Expected: 1, 19, 79, got: %d, %d, %d",
			torrents[0].UpRate, torrents[1].UpRate, torrents[2].UpRate)
	}

	torrents.Sort(BySize)
	if torrents[0].Size != 448 ||
		torrents[2].Size != 4048 {
		t.Errorf("bySize: Expected: 448, 1024, 4048, got: %d, %d, %d",
			torrents[0].Size, torrents[1].Size, torrents[2].Size)
	}

	torrents.Sort(ByRatio)
	if torrents[0].Ratio != 0.3 ||
		torrents[2].Ratio != 9.3 {
		t.Errorf("byRatio: Expected: 0.3, 2.3, 9.3, got: %.1f, %.1f, %.1f",
			torrents[0].Ratio, torrents[1].Ratio, torrents[2].Ratio)
	}

	torrents.Sort(ByAge)
	if torrents[0].Age != 1492021111 ||
		torrents[2].Age != 1492929977 {
		t.Errorf("byAge: Expected: 1492021111, 1492929111, 1492929977, got: %d, %d, %d",
			torrents[0].Age, torrents[1].Age, torrents[2].Age)
	}

	torrents.Sort(ByUpTotal)
	if torrents[0].UpTotal != 93413 ||
		torrents[2].UpTotal != 9176445 {
		t.Errorf("byUpTotal: Expected: 93413, 993413, 9176445, got: %d, %d, %d",
			torrents[0].UpTotal, torrents[1].UpTotal, torrents[2].UpTotal)
	}

}
