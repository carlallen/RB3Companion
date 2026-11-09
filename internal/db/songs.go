package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// SongRecord mirrors rb3net.Song. It's a separate type so this package
// doesn't need to import rb3net. The DifficultyX fields are chart
// difficulty ratings from 0-7, NOT NULL DEFAULT 0, where 0 means the song
// has no chart for that part at all (unlike Genre/VocalParts/Year/Length
// below, which stay nullable - nil when unknown).
type SongRecord struct {
	Shortname string
	Title     string
	Artist    string
	Album     string
	Origin    string

	DifficultyBand      int
	DifficultyGuitar    int
	DifficultyBass      int
	DifficultyDrum      int
	DifficultyKeys      int
	DifficultyVocals    int
	DifficultyProGuitar int
	DifficultyProBass   int
	DifficultyProKeys   int

	// Genre, VocalParts, Year and Length are nullable - nil when unknown.
	Genre      *string
	VocalParts *int // 0-3
	Year       *int
	Length     *int // milliseconds
}

// songsTableColumns are the songs table's columns, shared by
// createSongsTable and rebuildSongsTableNullable (which recreates the
// table under this same definition when migrating an older database).
const songsTableColumns = `
	shortname               TEXT PRIMARY KEY,
	title                   TEXT NOT NULL,
	artist                  TEXT NOT NULL,
	album                   TEXT NOT NULL,
	origin                  TEXT NOT NULL,
	difficulty_band         INTEGER NOT NULL DEFAULT 0,
	difficulty_guitar       INTEGER NOT NULL DEFAULT 0,
	difficulty_bass         INTEGER NOT NULL DEFAULT 0,
	difficulty_drum         INTEGER NOT NULL DEFAULT 0,
	difficulty_keys         INTEGER NOT NULL DEFAULT 0,
	difficulty_vocals       INTEGER NOT NULL DEFAULT 0,
	difficulty_pro_guitar   INTEGER NOT NULL DEFAULT 0,
	difficulty_pro_bass     INTEGER NOT NULL DEFAULT 0,
	difficulty_pro_keys     INTEGER NOT NULL DEFAULT 0,
	genre                   TEXT,
	vocal_parts             INTEGER,
	year                    INTEGER,
	length                  INTEGER,
	RB3art_check          INTEGER NOT NULL DEFAULT 0
`

var createSongsTable = "CREATE TABLE IF NOT EXISTS songs (" + songsTableColumns + ")"

// songDifficultyColumns are the songs table's difficulty columns, which must
// be NOT NULL DEFAULT 0 - checked by rebuildSongsTableDifficultyNotNull,
// since SQLite can't just ALTER a column to add a NOT NULL constraint.
var songDifficultyColumns = []string{
	"difficulty_band", "difficulty_guitar", "difficulty_bass", "difficulty_drum", "difficulty_keys",
	"difficulty_vocals", "difficulty_pro_guitar", "difficulty_pro_bass", "difficulty_pro_keys",
}

// songMigrationColumns are the songs columns added after the table's
// initial release, in the form "name type-and-default"; migrateSongsTable
// adds any of these missing from an existing database so upgrades don't
// need to drop it.
var songMigrationColumns = []string{
	"difficulty_band INTEGER NOT NULL DEFAULT 0",
	"difficulty_guitar INTEGER NOT NULL DEFAULT 0",
	"difficulty_bass INTEGER NOT NULL DEFAULT 0",
	"difficulty_drum INTEGER NOT NULL DEFAULT 0",
	"difficulty_keys INTEGER NOT NULL DEFAULT 0",
	"difficulty_vocals INTEGER NOT NULL DEFAULT 0",
	"difficulty_pro_guitar INTEGER NOT NULL DEFAULT 0",
	"difficulty_pro_bass INTEGER NOT NULL DEFAULT 0",
	"difficulty_pro_keys INTEGER NOT NULL DEFAULT 0",
	"genre TEXT",
	"vocal_parts INTEGER",
	"year INTEGER",
	"length INTEGER",
	"RB3art_check INTEGER NOT NULL DEFAULT 0",
}

// obsoleteSongColumns are songs columns earlier versions of this schema had
// and the current one doesn't: metadata_datetime was only used by the
// removed local metadata files, and difficulty_pro_drum always matched
// difficulty_drum (RB3 has no separate pro-drums rank), so the UI uses
// difficulty_drum for both. migrateSongsTable drops any still present.
var obsoleteSongColumns = []string{"metadata_datetime", "difficulty_pro_drum"}

// migrateSongsTable brings an already-existing songs table up to
// songsTableColumns: it renames the old db_art_check or RB3EC_db_check column
// if present, drops any obsoleteSongColumns still present, adds any songMigrationColumns that are missing
// entirely, then, if needed,
// rebuilds the table to restore the NOT NULL DEFAULT 0 constraint an earlier
// version of this schema dropped from the difficulty columns, coalescing any
// existing NULLs there to 0 along the way.
func migrateSongsTable(sqlDB *sql.DB) error {
	columns, err := songsColumnInfo(sqlDB)
	if err != nil {
		return err
	}

	for _, oldName := range []string{"db_art_check", "RB3EC_db_check"} {
		if _, hasOld := columns[oldName]; !hasOld {
			continue
		}
		if _, hasNew := columns["RB3art_check"]; !hasNew {
			if _, err := sqlDB.Exec(fmt.Sprintf(`ALTER TABLE songs RENAME COLUMN %s TO RB3art_check`, oldName)); err != nil {
				return err
			}
			columns["RB3art_check"] = columns[oldName]
		}
		delete(columns, oldName)
	}

	for _, name := range obsoleteSongColumns {
		if _, ok := columns[name]; !ok {
			continue
		}
		if _, err := sqlDB.Exec(fmt.Sprintf(`ALTER TABLE songs DROP COLUMN %s`, name)); err != nil {
			return err
		}
		delete(columns, name)
	}

	for _, colDef := range songMigrationColumns {
		name, _, _ := strings.Cut(colDef, " ")
		if _, ok := columns[name]; ok {
			continue
		}
		if _, err := sqlDB.Exec(fmt.Sprintf(`ALTER TABLE songs ADD COLUMN %s`, colDef)); err != nil {
			return err
		}
		columns[name] = strings.Contains(colDef, "NOT NULL")
	}

	needsRebuild := false
	for _, name := range songDifficultyColumns {
		if !columns[name] {
			needsRebuild = true
			break
		}
	}
	if needsRebuild {
		return rebuildSongsTableDifficultyNotNull(sqlDB)
	}
	return nil
}

// songsColumnInfo returns the songs table's columns, mapped to whether
// each has a NOT NULL constraint.
func songsColumnInfo(sqlDB *sql.DB) (map[string]bool, error) {
	rows, err := sqlDB.Query(`PRAGMA table_info(songs)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			dfltValue interface{}
			pk        int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = notNull != 0
	}
	return columns, rows.Err()
}

// rebuildSongsTableDifficultyNotNull recreates the songs table under the
// current songsTableColumns definition and copies the existing rows across,
// coalescing any existing NULLs in the difficulty columns to 0. SQLite has
// no ALTER TABLE form for adding a NOT NULL constraint to a column that may
// already hold NULLs, so this is the only way to restore the NOT NULL
// DEFAULT 0 constraint an earlier version of this schema dropped from the
// difficulty columns.
func rebuildSongsTableDifficultyNotNull(sqlDB *sql.DB) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("CREATE TABLE songs_new (" + songsTableColumns + ")"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO songs_new SELECT
			shortname, title, artist, album, origin,
			COALESCE(difficulty_band, 0), COALESCE(difficulty_guitar, 0), COALESCE(difficulty_bass, 0),
			COALESCE(difficulty_drum, 0), COALESCE(difficulty_keys, 0), COALESCE(difficulty_vocals, 0),
			COALESCE(difficulty_pro_guitar, 0), COALESCE(difficulty_pro_bass, 0), COALESCE(difficulty_pro_keys, 0),
			genre, vocal_parts, year, length, RB3art_check
		FROM songs`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE songs`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE songs_new RENAME TO songs`); err != nil {
		return err
	}
	return tx.Commit()
}

// applySongMetadata sets s's difficulty/genre/vocal-parts/year/length
// fields. Metadata the console reported in the song list (see
// hasConsoleMetadata) is used as-is, except that a missing VocalParts (only
// reported when the console has the song's info loaded) keeps the stored
// value. Otherwise - an older RB3Enhanced
// build that only reports the identifying fields - s keeps whatever
// metadata was already stored for it (nothing, for a song seen for the
// first time).
func applySongMetadata(s *SongRecord, stored SongRecord) {
	if hasConsoleMetadata(*s) {
		if s.VocalParts == nil {
			s.VocalParts = stored.VocalParts
		}
		return
	}
	copySongMetadata(s, stored)
}

// hasConsoleMetadata reports whether s carries metadata from the console's
// song list - newer RB3Enhanced builds report genre/year/length/vocal parts
// and per-part ranks there, older ones only the identifying fields.
func hasConsoleMetadata(s SongRecord) bool {
	return s.Genre != nil || s.Year != nil || s.Length != nil || s.VocalParts != nil ||
		s.DifficultyBand != 0 || s.DifficultyGuitar != 0 || s.DifficultyBass != 0 ||
		s.DifficultyDrum != 0 || s.DifficultyKeys != 0 || s.DifficultyVocals != 0 ||
		s.DifficultyProGuitar != 0 || s.DifficultyProBass != 0 || s.DifficultyProKeys != 0
}

// copySongMetadata copies stored's difficulty/genre/vocal-parts/year/
// length fields onto s, leaving s's identifying fields
// (shortname, title, artist, album, origin) untouched.
func copySongMetadata(s *SongRecord, stored SongRecord) {
	s.DifficultyBand = stored.DifficultyBand
	s.DifficultyGuitar = stored.DifficultyGuitar
	s.DifficultyBass = stored.DifficultyBass
	s.DifficultyDrum = stored.DifficultyDrum
	s.DifficultyKeys = stored.DifficultyKeys
	s.DifficultyVocals = stored.DifficultyVocals
	s.DifficultyProGuitar = stored.DifficultyProGuitar
	s.DifficultyProBass = stored.DifficultyProBass
	s.DifficultyProKeys = stored.DifficultyProKeys
	s.Genre = stored.Genre
	s.VocalParts = stored.VocalParts
	s.Year = stored.Year
	s.Length = stored.Length
}

// nullableSongMetadata holds sql.Scan destinations for every nullable songs
// column (genre/vocal-parts/year/length - the difficulty
// columns are NOT NULL, so they're scanned directly into SongRecord's plain
// int fields instead), in the column order shared by loadStoredSongMetadata
// and LoadSongs's queries.
type nullableSongMetadata struct {
	genre      sql.NullString
	vocalParts sql.NullInt64
	year       sql.NullInt64
	length     sql.NullInt64
}

// dests returns Scan destinations for n's fields, in column order.
func (n *nullableSongMetadata) dests() []interface{} {
	return []interface{}{&n.genre, &n.vocalParts, &n.year, &n.length}
}

// applyTo sets s's corresponding fields to nil, or a pointer to the scanned
// value, for each of n's columns that came back non-NULL.
func (n nullableSongMetadata) applyTo(s *SongRecord) {
	if n.genre.Valid {
		g := n.genre.String
		s.Genre = &g
	}
	s.VocalParts = nullIntPtr(n.vocalParts)
	s.Year = nullIntPtr(n.year)
	s.Length = nullIntPtr(n.length)
}

func nullIntPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// loadStoredSongMetadata returns the difficulty/genre/vocal-parts/year/
// length fields currently stored for every song, keyed by shortname, so
// SaveSongs can keep them for songs the console reports no metadata for.
func loadStoredSongMetadata(tx *sql.Tx) (map[string]SongRecord, error) {
	rows, err := tx.Query(`
		SELECT
			shortname,
			difficulty_band, difficulty_guitar, difficulty_bass, difficulty_drum, difficulty_keys,
			difficulty_vocals, difficulty_pro_guitar, difficulty_pro_bass, difficulty_pro_keys,
			genre, vocal_parts, year, length
		FROM songs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]SongRecord)
	for rows.Next() {
		var s SongRecord
		var n nullableSongMetadata
		dest := append([]interface{}{
			&s.Shortname,
			&s.DifficultyBand, &s.DifficultyGuitar, &s.DifficultyBass, &s.DifficultyDrum, &s.DifficultyKeys,
			&s.DifficultyVocals, &s.DifficultyProGuitar, &s.DifficultyProBass, &s.DifficultyProKeys,
		}, n.dests()...)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		n.applyTo(&s)
		result[s.Shortname] = s
	}
	return result, rows.Err()
}

// SaveSongs replaces the songs table's contents with songs: rows for
// shortnames no longer present are deleted, the rest are inserted or
// updated, so the table always mirrors the Music Library as of the most
// recent fetch.
//
// As a safety net against wiping a populated table from a bad fetch (e.g. a
// transient empty or malformed response from the console), an empty songs
// list is only allowed to clear the table if the table was already empty;
// otherwise the existing rows are left untouched and a warning is logged.
//
// Songs the console reported no metadata for keep their stored metadata -
// see applySongMetadata. This is done here, rather than by the caller, so
// every insert path gets it consistently. songs is mutated in place with
// the resulting values.
func SaveSongs(sqlDB *sql.DB, songs []SongRecord) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stored, err := loadStoredSongMetadata(tx)
	if err != nil {
		return err
	}
	for i := range songs {
		applySongMetadata(&songs[i], stored[songs[i].Shortname])
	}

	stmt, err := tx.Prepare(`
		INSERT INTO songs (
			shortname, title, artist, album, origin,
			difficulty_band, difficulty_guitar, difficulty_bass, difficulty_drum, difficulty_keys,
			difficulty_vocals, difficulty_pro_guitar, difficulty_pro_bass, difficulty_pro_keys,
			genre, vocal_parts, year, length
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(shortname) DO UPDATE SET
			title = excluded.title,
			artist = excluded.artist,
			album = excluded.album,
			origin = excluded.origin,
			difficulty_band = excluded.difficulty_band,
			difficulty_guitar = excluded.difficulty_guitar,
			difficulty_bass = excluded.difficulty_bass,
			difficulty_drum = excluded.difficulty_drum,
			difficulty_keys = excluded.difficulty_keys,
			difficulty_vocals = excluded.difficulty_vocals,
			difficulty_pro_guitar = excluded.difficulty_pro_guitar,
			difficulty_pro_bass = excluded.difficulty_pro_bass,
			difficulty_pro_keys = excluded.difficulty_pro_keys,
			genre = excluded.genre,
			vocal_parts = excluded.vocal_parts,
			year = excluded.year,
			length = excluded.length`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	shortnames := make([]interface{}, len(songs))
	placeholders := make([]string, len(songs))
	for i := range songs {
		s := songs[i]
		if _, err := stmt.Exec(
			s.Shortname, s.Title, s.Artist, s.Album, s.Origin,
			s.DifficultyBand, s.DifficultyGuitar, s.DifficultyBass, s.DifficultyDrum, s.DifficultyKeys,
			s.DifficultyVocals, s.DifficultyProGuitar, s.DifficultyProBass, s.DifficultyProKeys,
			s.Genre, s.VocalParts, s.Year, s.Length,
		); err != nil {
			return err
		}
		shortnames[i] = s.Shortname
		placeholders[i] = "?"
	}

	if len(shortnames) == 0 {
		var existing int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM songs`).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			log.Printf("db: SaveSongs got an empty song list but %d songs are already stored; skipping the wipe", existing)
			return tx.Commit()
		}
	}

	deleteQuery := "DELETE FROM songs"
	if len(shortnames) > 0 {
		deleteQuery += " WHERE shortname NOT IN (" + strings.Join(placeholders, ",") + ")"
	}
	if _, err := tx.Exec(deleteQuery, shortnames...); err != nil {
		return err
	}

	return tx.Commit()
}

// RB3ArtCheckSong is a song not yet checked against the RB3art repository
// - see PendingRB3ArtCheckSongs.
type RB3ArtCheckSong struct {
	Shortname string
	Origin    string
}

// PendingRB3ArtCheckSongs returns the shortname and origin of every song
// whose RB3art_check flag is still false, i.e. hasn't been checked yet.
func PendingRB3ArtCheckSongs(sqlDB *sql.DB) ([]RB3ArtCheckSong, error) {
	rows, err := sqlDB.Query(`SELECT shortname, origin FROM songs WHERE RB3art_check = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []RB3ArtCheckSong
	for rows.Next() {
		var s RB3ArtCheckSong
		if err := rows.Scan(&s.Shortname, &s.Origin); err != nil {
			return nil, err
		}
		pending = append(pending, s)
	}
	return pending, rows.Err()
}

// MarkRB3ArtChecked sets shortname's RB3art_check flag so
// PendingRB3ArtCheckSongs won't return it again.
func MarkRB3ArtChecked(sqlDB *sql.DB, shortname string) error {
	_, err := sqlDB.Exec(`UPDATE songs SET RB3art_check = 1 WHERE shortname = ?`, shortname)
	return err
}

// CountSongs returns the number of songs currently stored in the songs
// table, without loading their data.
func CountSongs(sqlDB *sql.DB) (int, error) {
	var count int
	err := sqlDB.QueryRow(`SELECT COUNT(*) FROM songs`).Scan(&count)
	return count, err
}

// LoadSongs returns every song stored in the songs table, ordered by title.
func LoadSongs(sqlDB *sql.DB) ([]SongRecord, error) {
	rows, err := sqlDB.Query(`
		SELECT
			shortname, title, artist, album, origin,
			difficulty_band, difficulty_guitar, difficulty_bass, difficulty_drum, difficulty_keys,
			difficulty_vocals, difficulty_pro_guitar, difficulty_pro_bass, difficulty_pro_keys,
			genre, vocal_parts, year, length
		FROM songs ORDER BY title COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []SongRecord
	for rows.Next() {
		var s SongRecord
		var n nullableSongMetadata
		dest := append([]interface{}{
			&s.Shortname, &s.Title, &s.Artist, &s.Album, &s.Origin,
			&s.DifficultyBand, &s.DifficultyGuitar, &s.DifficultyBass, &s.DifficultyDrum, &s.DifficultyKeys,
			&s.DifficultyVocals, &s.DifficultyProGuitar, &s.DifficultyProBass, &s.DifficultyProKeys,
		}, n.dests()...)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		n.applyTo(&s)
		songs = append(songs, s)
	}
	return songs, rows.Err()
}
