package main

import "testing"

func TestRejectMissingOrWeakProductionDatabaseConfig(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	for _, dsn := range []string{"", "invalid-dsn", "apkhtml:apkhtml@tcp(127.0.0.1:3306)/apkhtml", "apkhtml:local-dev-password@tcp(127.0.0.1:3306)/apkhtml", "root@tcp(127.0.0.1:3306)/apkhtml"} {
		if _, err := openDatabase(dsn); err == nil {
			t.Fatalf("accepted unsafe DSN")
		}
	}
}
