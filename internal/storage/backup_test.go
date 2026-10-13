package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestValidateDB(t *testing.T) {
	// A real bot database passes.
	good := filepath.Join(t.TempDir(), "good.db")
	st, err := Open(good)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	st.Close()
	if err := ValidateDB(good); err != nil {
		t.Errorf("ValidateDB(real db) = %v, want nil", err)
	}

	// A database with only some of the tables is rejected.
	cases := map[string][]string{
		"players only":     {`CREATE TABLE players (id INTEGER)`},
		"no games":         {`CREATE TABLE players (id INTEGER)`, `CREATE TABLE seasons (id INTEGER)`, `CREATE TABLE game_results (id INTEGER)`},
		"no game_results":  {`CREATE TABLE players (id INTEGER)`, `CREATE TABLE seasons (id INTEGER)`, `CREATE TABLE games (id INTEGER)`},
		"unrelated tables": {`CREATE TABLE foo (id INTEGER)`},
	}
	for name, ddl := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "x.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatalf("sql.Open: %v", err)
			}
			for _, q := range ddl {
				if _, err := db.Exec(q); err != nil {
					t.Fatalf("exec %q: %v", q, err)
				}
			}
			db.Close()
			if err := ValidateDB(path); err == nil {
				t.Error("ValidateDB = nil, want an error")
			}
		})
	}
}
