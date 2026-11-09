package rb3net

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/carlallen/RB3Companion/internal/db"
)

// consoleHTTPPort is the TCP port RB3Enhanced's in-game HTTP server listens
// on, on the same console whose UDP broadcast we've been receiving. It's
// the same numeric port as the UDP broadcast (0x524E, 'RN'), just TCP
// instead of UDP.
const consoleHTTPPort = "21070"

// songListScreen is the ScreenName RB3Enhanced reports while the player is
// in the Music Library's song select screen, learned by observation.
const songListScreen = "song_select_screen"

// FetchSongList fetches and parses RB3Enhanced's /list_songs endpoint from
// the console at ip. The console builds this list from whatever's in the
// Music Library at the time of the request, and does the same for every
// caller, so there's no per-request state to worry about here.
func FetchSongList(ctx context.Context, ip string) ([]Song, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	target := fmt.Sprintf("http://%s:%s/list_songs", ip, consoleHTTPPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("console returned %s", resp.Status)
	}
	return parseSongListINI(resp.Body), nil
}

// parseSongListINI parses RB3Enhanced's /list_songs response: repeated
// "[shortname]\r\nkey=value\r\n...\r\n\r\n" blocks, one per song. Besides
// the identifying fields, newer RB3Enhanced builds also report genre, year,
// length, vocal_parts and a rank_<part> line per charted part; ranks are converted to
// 0-7 difficulty tiers here. Older builds omit these, leaving them unset.
func parseSongListINI(r io.Reader) []Song {
	var songs []Song
	var cur *Song

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			songs = append(songs, Song{})
			cur = &songs[len(songs)-1]
			continue
		}
		if cur == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "shortname":
			cur.Shortname = value
		case "title":
			cur.Title = value
		case "artist":
			cur.Artist = value
		case "album":
			cur.Album = value
		case "origin":
			cur.Origin = value
		case "genre":
			if value != "" {
				g := genreDisplayName(value)
				cur.Genre = &g
			}
		case "year":
			// RB3Enhanced reports dateReleased.year + 1900, so a song with
			// no release date comes through as 1900.
			if n, err := strconv.Atoi(value); err == nil && n > 1900 {
				cur.Year = &n
			}
		case "length":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				cur.Length = &n
			}
		case "vocal_parts":
			// Only reported when the console has the song's info loaded.
			if n, err := strconv.Atoi(value); err == nil && n >= 0 && n <= 3 {
				cur.VocalParts = &n
			}
		default:
			if !strings.HasPrefix(key, "rank_") {
				continue
			}
			part := strings.TrimPrefix(key, "rank_")
			rank, err := strconv.Atoi(value)
			if err != nil {
				continue
			}
			applyRank(cur, part, rank)
		}
	}
	return songs
}

// rankTierThresholds are, per RB3 rank part, the minimum raw rank for tiers
// 2-7 (any rank above 0 but below the first is tier 1) - the official
// instrument difficulty tier values, as used by rb_dta_to_rb3ec_db_yml and
// Nautilus to build RB3art's metadata files.
var rankTierThresholds = map[string][6]int{
	"band":        {165, 215, 243, 267, 292, 345},
	"guitar":      {139, 176, 221, 267, 333, 409},
	"bass":        {135, 181, 228, 293, 364, 436},
	"drum":        {124, 151, 178, 242, 345, 448},
	"keys":        {153, 211, 269, 327, 385, 443},
	"vocals":      {132, 175, 218, 279, 353, 427},
	"real_guitar": {150, 208, 267, 325, 384, 442},
	"real_bass":   {150, 208, 267, 325, 384, 442},
	"real_keys":   {153, 211, 269, 327, 385, 443},
}

// rankTier converts a raw rank for part into a 0-7 difficulty tier, 0
// meaning the song has no chart for that part.
func rankTier(part string, rank int) int {
	thresholds, ok := rankTierThresholds[part]
	if !ok || rank <= 0 {
		return 0
	}
	tier := 1
	for _, t := range thresholds {
		if rank >= t {
			tier++
		}
	}
	return tier
}

// applyRank sets the difficulty field on s matching an RB3 rank part. RB3
// has no separate pro-drums rank, so drum covers both.
func applyRank(s *Song, part string, rank int) {
	tier := rankTier(part, rank)
	switch part {
	case "band":
		s.DifficultyBand = tier
	case "guitar":
		s.DifficultyGuitar = tier
	case "bass":
		s.DifficultyBass = tier
	case "drum":
		s.DifficultyDrum = tier
	case "keys":
		s.DifficultyKeys = tier
	case "vocals":
		s.DifficultyVocals = tier
	case "real_guitar":
		s.DifficultyProGuitar = tier
	case "real_bass":
		s.DifficultyProBass = tier
	case "real_keys":
		s.DifficultyProKeys = tier
	}
}

// genreDisplayNames maps RB3 genre symbols to their display names, matching
// the genre strings in RB3art's metadata files. Unknown symbols are shown
// as-is.
var genreDisplayNames = map[string]string{
	"alternative":        "Alternative",
	"blues":              "Blues",
	"classical":          "Classical",
	"classicrock":        "Classic Rock",
	"country":            "Country",
	"emo":                "Emo",
	"fusion":             "Fusion",
	"glam":               "Glam",
	"grunge":             "Grunge",
	"hiphoprap":          "Hip-Hop/Rap",
	"indierock":          "Indie Rock",
	"jazz":               "Jazz",
	"jrock":              "J-Rock",
	"latin":              "Latin",
	"metal":              "Metal",
	"new_wave":           "New Wave",
	"novelty":            "Novelty",
	"numetal":            "Nu-Metal",
	"other":              "Other",
	"poprock":            "Pop-Rock",
	"popdanceelectronic": "Pop/Dance/Electronic",
	"prog":               "Prog",
	"punk":               "Punk",
	"rbsoulfunk":         "R&B/Soul/Funk",
	"reggaeska":          "Reggae/Ska",
	"inspirational":      "Inspirational",
	"rock":               "Rock",
	"southernrock":       "Southern Rock",
	"urban":              "Urban",
	"world":              "World",
}

func genreDisplayName(symbol string) string {
	if name, ok := genreDisplayNames[symbol]; ok {
		return name
	}
	return symbol
}

// Jump tells the console to select shortname in the Music Library.
func Jump(ctx context.Context, ip, shortname string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	target := fmt.Sprintf("http://%s:%s/jump?shortname=%s", ip, consoleHTTPPort, url.QueryEscape(shortname))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("console returned %s", resp.Status)
	}
	return nil
}

// SongListWatcher fetches the song list from the console's HTTP server the
// first time RB3Enhanced reports the song select screen after each
// successful connection and persists it to DB, bumping the Hub's
// SongListVersion so subscribers, e.g. the web dashboard, know to reload it
// from there.
type SongListWatcher struct {
	Hub *Hub
	DB  *sql.DB
}

func (w *SongListWatcher) Run(ctx context.Context) {
	ch := w.Hub.Subscribe()
	defer w.Hub.Unsubscribe(ch)

	wasConnected := false
	fetchedThisConnection := false
	for {
		select {
		case <-ctx.Done():
			return
		case state, ok := <-ch:
			if !ok {
				return
			}
			if state.Connected && !wasConnected {
				// A new connection: allow one more refresh, the first time
				// the song select screen is visited during it.
				fetchedThisConnection = false
			}
			wasConnected = state.Connected

			if fetchedThisConnection || state.ScreenName != songListScreen {
				continue
			}
			ip := state.SourceIP
			if ip == "" {
				continue
			}
			fetchedThisConnection = true
			go w.fetch(ctx, ip)
		}
	}
}

func (w *SongListWatcher) fetch(ctx context.Context, ip string) {
	songs, err := FetchSongList(ctx, ip)
	if err != nil {
		log.Printf("rb3net: song list fetch failed: %v (console_ip=%s)", err, ip)
		return
	}
	sort.Slice(songs, func(i, j int) bool {
		return strings.ToLower(songs[i].Title) < strings.ToLower(songs[j].Title)
	})

	records := make([]db.SongRecord, len(songs))
	for i, s := range songs {
		records[i] = db.SongRecord(s)
	}
	if err := db.SaveSongs(w.DB, records); err != nil {
		log.Printf("rb3net: song list save failed: %v (console_ip=%s)", err, ip)
	}

	w.Hub.Mutate(func(s *GameState) {
		s.SongListVersion++
	})
}
