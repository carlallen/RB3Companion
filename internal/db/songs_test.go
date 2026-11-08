package db

import "testing"

func TestSaveSongsReplacesTable(t *testing.T) {
	sqlDB, err := Open(t.TempDir() + "/test.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	first := []SongRecord{
		{Shortname: "song1", Title: "Song One", Artist: "A", Album: "X", Origin: "rb3"},
		{Shortname: "song2", Title: "Song Two", Artist: "B", Album: "Y", Origin: "rb3"},
	}
	if err := SaveSongs(sqlDB, first); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadSongs(sqlDB); err != nil || len(loaded) != 2 {
		t.Fatalf("after first save: loaded=%+v err=%v", loaded, err)
	}

	// A later fetch updates song1, drops song2, and adds song3 - the table
	// should end up mirroring exactly this list.
	second := []SongRecord{
		{Shortname: "song1", Title: "Song One Updated", Artist: "A2", Album: "X2", Origin: "rb3"},
		{Shortname: "song3", Title: "Song Three", Artist: "C", Album: "Z", Origin: "rb3"},
	}
	if err := SaveSongs(sqlDB, second); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSongs(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	byShortname := map[string]SongRecord{}
	for _, s := range loaded {
		byShortname[s.Shortname] = s
	}
	if _, ok := byShortname["song2"]; ok {
		t.Fatalf("song2 should have been removed, loaded=%+v", loaded)
	}
	if s, ok := byShortname["song1"]; !ok || s.Title != "Song One Updated" {
		t.Fatalf("song1 should be updated, got %+v", s)
	}
	if _, ok := byShortname["song3"]; !ok {
		t.Fatalf("song3 should be present, loaded=%+v", loaded)
	}

	// An empty fetch against a populated table (e.g. a transient bad
	// response from the console) should NOT wipe the table.
	if err := SaveSongs(sqlDB, nil); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadSongs(sqlDB); err != nil || len(loaded) != 2 {
		t.Fatalf("after empty save against populated table: loaded=%+v err=%v", loaded, err)
	}
}

func TestSaveSongsEmptyIsNoopOnEmptyTable(t *testing.T) {
	sqlDB, err := Open(t.TempDir() + "/test.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	if err := SaveSongs(sqlDB, nil); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadSongs(sqlDB); err != nil || len(loaded) != 0 {
		t.Fatalf("after empty save on empty table: loaded=%+v err=%v", loaded, err)
	}
}

func TestSaveSongsKeepsStoredMetadataWithoutConsoleMetadata(t *testing.T) {
	sqlDB, err := Open(t.TempDir() + "/test.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	genre := "Metal"
	year := 1990
	if err := SaveSongs(sqlDB, []SongRecord{
		{Shortname: "song", Title: "S", Origin: "rb3", DifficultyBand: 5, Genre: &genre, Year: &year},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`UPDATE songs SET vocal_parts = 2 WHERE shortname = 'song'`); err != nil {
		t.Fatal(err)
	}

	// A save from an older RB3Enhanced build (no metadata in the song list)
	// keeps everything stored.
	if err := SaveSongs(sqlDB, []SongRecord{{Shortname: "song", Title: "S", Origin: "rb3"}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSongs(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].DifficultyBand != 5 || loaded[0].Genre == nil || *loaded[0].Genre != "Metal" ||
		loaded[0].VocalParts == nil || *loaded[0].VocalParts != 2 {
		t.Fatalf("stored metadata should be kept, got %+v", loaded)
	}

	// New console metadata replaces it, but vocal parts is kept when the
	// console didn't report it.
	newGenre := "Rock"
	if err := SaveSongs(sqlDB, []SongRecord{
		{Shortname: "song", Title: "S", Origin: "rb3", DifficultyBand: 3, Genre: &newGenre},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadSongs(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	s := loaded[0]
	if s.DifficultyBand != 3 || s.Genre == nil || *s.Genre != "Rock" || s.Year != nil ||
		s.VocalParts == nil || *s.VocalParts != 2 {
		t.Fatalf("console metadata should replace stored values except vocal parts, got %+v", s)
	}

	// Console-reported vocal parts replaces the stored value.
	one := 1
	if err := SaveSongs(sqlDB, []SongRecord{
		{Shortname: "song", Title: "S", Origin: "rb3", DifficultyBand: 3, VocalParts: &one},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadSongs(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if v := loaded[0].VocalParts; v == nil || *v != 1 {
		t.Fatalf("console vocal parts should be stored, got %v", v)
	}
}

func TestOpenDropsMetadataDatetimeColumn(t *testing.T) {
	path := t.TempDir() + "/test.sql"
	sqlDB, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`ALTER TABLE songs ADD COLUMN metadata_datetime DATETIME`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO songs (shortname, title, artist, album, origin, metadata_datetime) VALUES ('s', 'S', 'A', 'B', 'rb3', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	sqlDB.Close()

	sqlDB, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	columns, err := songsColumnInfo(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := columns["metadata_datetime"]; ok {
		t.Fatal("metadata_datetime column should have been dropped")
	}
	if loaded, err := LoadSongs(sqlDB); err != nil || len(loaded) != 1 {
		t.Fatalf("existing row should survive the migration: loaded=%+v err=%v", loaded, err)
	}
}

func TestOpenDropsDifficultyProDrumColumn(t *testing.T) {
	path := t.TempDir() + "/test.sql"
	sqlDB, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`ALTER TABLE songs ADD COLUMN difficulty_pro_drum INTEGER NOT NULL DEFAULT 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO songs (shortname, title, artist, album, origin, difficulty_drum, difficulty_pro_drum) VALUES ('s', 'S', 'A', 'B', 'rb3', 4, 4)`); err != nil {
		t.Fatal(err)
	}
	sqlDB.Close()

	sqlDB, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	columns, err := songsColumnInfo(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := columns["difficulty_pro_drum"]; ok {
		t.Fatal("difficulty_pro_drum column should have been dropped")
	}
	loaded, err := LoadSongs(sqlDB)
	if err != nil || len(loaded) != 1 || loaded[0].DifficultyDrum != 4 {
		t.Fatalf("existing row should survive the migration: loaded=%+v err=%v", loaded, err)
	}
}
