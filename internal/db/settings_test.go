package db

import "testing"

func TestConsoleIPRoundTrip(t *testing.T) {
	sqlDB, err := Open(t.TempDir() + "/test.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	if ip, err := GetConsoleIP(sqlDB); err != nil || ip != "" {
		t.Fatalf("GetConsoleIP on fresh db = %q, %v; want \"\", nil", ip, err)
	}
	for _, want := range []string{"192.168.1.50", "192.168.1.51"} {
		if err := SetConsoleIP(sqlDB, want); err != nil {
			t.Fatal(err)
		}
		if ip, err := GetConsoleIP(sqlDB); err != nil || ip != want {
			t.Fatalf("GetConsoleIP = %q, %v; want %q", ip, err, want)
		}
	}
	if err := SetConsoleIP(sqlDB, ""); err != nil {
		t.Fatal(err)
	}
	if ip, err := GetConsoleIP(sqlDB); err != nil || ip != "" {
		t.Fatalf("GetConsoleIP after clear = %q, %v; want \"\", nil", ip, err)
	}
}
