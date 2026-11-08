package db

import (
	"database/sql"
	"errors"
)

const createSettingsTable = `
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
)`

// settingConsoleIP is the settings key holding the user-configured console
// address; see GetConsoleIP.
const settingConsoleIP = "console_ip"

// GetSetting returns the value stored under key, or "" if none is set.
func GetSetting(sqlDB *sql.DB, key string) (string, error) {
	var value string
	err := sqlDB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// SetSetting stores value under key; an empty value removes the setting.
func SetSetting(sqlDB *sql.DB, key, value string) error {
	if value == "" {
		_, err := sqlDB.Exec(`DELETE FROM settings WHERE key = ?`, key)
		return err
	}
	_, err := sqlDB.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetConsoleIP returns the console (Xbox 360 / PS3) address the user has
// configured, or "" if they haven't - in which case the address RB3Enhanced's
// UDP packets arrive from is used instead (see rb3net.ConsoleAddress).
func GetConsoleIP(sqlDB *sql.DB) (string, error) {
	return GetSetting(sqlDB, settingConsoleIP)
}

// SetConsoleIP stores the configured console address; "" clears it, going
// back to auto-detection.
func SetConsoleIP(sqlDB *sql.DB, ip string) error {
	return SetSetting(sqlDB, settingConsoleIP, ip)
}
