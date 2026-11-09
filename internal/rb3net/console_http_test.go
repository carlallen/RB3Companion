package rb3net

import (
	"strings"
	"testing"
)

func TestParseSongListINIMetadata(t *testing.T) {
	body := strings.Join([]string{
		"[highwaystar]",
		"shortname=highwaystar",
		"title=Highway Star",
		"artist=Deep Purple",
		"album=Machine Head",
		"origin=rb1",
		"genre=classicrock",
		"year=1972",
		"length=369375",
		"vocal_parts=2",
		"rank_drum=430",
		"rank_guitar=385",
		"rank_bass=385",
		"rank_vocals=290",
		"rank_keys=477",
		"rank_real_keys=476",
		"rank_real_guitar=442",
		"rank_real_bass=430",
		"rank_band=440",
		"",
		"[oldbuild]",
		"shortname=oldbuild",
		"title=Old Build",
		"artist=A",
		"album=B",
		"origin=rb3",
		"",
	}, "\r\n")

	songs := parseSongListINI(strings.NewReader(body))
	if len(songs) != 2 {
		t.Fatalf("got %d songs, want 2", len(songs))
	}

	s := songs[0]
	if s.Shortname != "highwaystar" || s.Title != "Highway Star" || s.Origin != "rb1" {
		t.Fatalf("identifying fields wrong: %+v", s)
	}
	if s.Genre == nil || *s.Genre != "Classic Rock" {
		t.Errorf("genre = %v, want Classic Rock", s.Genre)
	}
	if s.Year == nil || *s.Year != 1972 {
		t.Errorf("year = %v, want 1972", s.Year)
	}
	if s.Length == nil || *s.Length != 369375 {
		t.Errorf("length = %v, want 369375", s.Length)
	}
	if s.VocalParts == nil || *s.VocalParts != 2 {
		t.Errorf("vocal parts = %v, want 2", s.VocalParts)
	}

	// Expected tiers match RB3art's metadata/rb1/highwaystar.yml.
	want := map[string][2]int{
		"band":       {s.DifficultyBand, 7},
		"guitar":     {s.DifficultyGuitar, 6},
		"bass":       {s.DifficultyBass, 6},
		"drum":       {s.DifficultyDrum, 6},
		"keys":       {s.DifficultyKeys, 7},
		"vocals":     {s.DifficultyVocals, 5},
		"pro_guitar": {s.DifficultyProGuitar, 7},
		"pro_bass":   {s.DifficultyProBass, 6},
		"pro_keys":   {s.DifficultyProKeys, 7},
	}
	for part, gw := range want {
		if gw[0] != gw[1] {
			t.Errorf("%s tier = %d, want %d", part, gw[0], gw[1])
		}
	}

	old := songs[1]
	if old.Genre != nil || old.Year != nil || old.Length != nil || old.VocalParts != nil || old.DifficultyBand != 0 {
		t.Errorf("song without metadata fields should leave them unset: %+v", old)
	}
}

func TestRankTier(t *testing.T) {
	cases := []struct {
		part string
		rank int
		want int
	}{
		{"drum", 0, 0},
		{"drum", 1, 1},
		{"drum", 123, 1},
		{"drum", 124, 2},
		{"drum", 447, 6},
		{"drum", 448, 7},
		{"unknown", 300, 0},
	}
	for _, c := range cases {
		if got := rankTier(c.part, c.rank); got != c.want {
			t.Errorf("rankTier(%q, %d) = %d, want %d", c.part, c.rank, got, c.want)
		}
	}
}
