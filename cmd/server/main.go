package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/carlallen/RB3Companion/internal/db"
	"github.com/carlallen/RB3Companion/internal/rb3net"
	"github.com/carlallen/RB3Companion/internal/server"
)

// migrateLegacyDB moves a database created under the project's previous name
// to newPath, so upgrading doesn't start with an empty database. The main file
// moves last: if a move fails partway, the next startup retries it.
func migrateLegacyDB(oldPath, newPath string) {
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		return
	}
	if _, err := os.Stat(oldPath); err != nil {
		return
	}
	for _, suffix := range []string{"-wal", "-shm", ""} {
		if err := os.Rename(oldPath+suffix, newPath+suffix); err != nil && !os.IsNotExist(err) {
			log.Fatalf("db: failed to move %s to %s: %v", oldPath+suffix, newPath+suffix, err)
		}
	}
	log.Printf("db: moved %s to %s", oldPath, newPath)
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	udpAddr := os.Getenv("UDP_ADDR")
	if udpAddr == "" {
		udpAddr = ":21070"
	}

	staticDir := "web/static"
	templateDir := "web/templates"

	storeDir := os.Getenv("STORE_DIR")
	if storeDir == "" {
		storeDir = "store"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(storeDir, "rb3companion.db")
		migrateLegacyDB(filepath.Join(storeDir, "rb3ecompanion.db"), dbPath)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("db: failed to open %s: %v", dbPath, err)
	}
	defer database.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := rb3net.NewHub()

	if count, err := db.CountSongs(database); err != nil {
		log.Fatalf("db: failed to count songs: %v", err)
	} else if count > 0 {
		// Bump SongListVersion so a client connecting fresh (no songVersion
		// query param, which defaults to 0) is sent the song list - which
		// is loaded from the database on demand, not cached here - rather
		// than the version mismatch check treating the default as already
		// up to date.
		hub.Mutate(func(s *rb3net.GameState) {
			s.SongListVersion++
		})
	}

	listener := &rb3net.Listener{Addr: udpAddr, Hub: hub}
	go func() {
		log.Printf("listening for RB3Enhanced on %s (udp)", udpAddr)
		if err := listener.Run(ctx); err != nil {
			log.Fatalf("udp listener error: %v", err)
		}
	}()

	songs := &rb3net.SongListWatcher{Hub: hub, DB: database}
	go songs.Run(ctx)

	wled := &rb3net.WLEDWatcher{Hub: hub, DB: database}
	go wled.Run(ctx)

	dbCheck := &rb3net.RB3ECDBCheckWatcher{Hub: hub, DB: database, StoreDir: storeDir}
	go dbCheck.Run(ctx)

	srv := &http.Server{
		Addr:    addr,
		Handler: server.NewRouter(hub, database, staticDir, storeDir, templateDir),
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	log.Println("shutting down")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
}
