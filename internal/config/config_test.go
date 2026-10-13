package config

import "testing"

func TestLoadOwnerID(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		want    int64
		wantErr bool
	}{
		{"empty means no owner", "", 0, false},
		{"valid id", "123456789", 123456789, false},
		{"not a number", "abc", 0, true},
		{"has a username prefix", "@owner", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOT_TOKEN", "x")
			t.Setenv("OWNER_TG_ID", tc.env)
			cfg, err := Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && cfg.OwnerID != tc.want {
				t.Errorf("OwnerID = %d, want %d", cfg.OwnerID, tc.want)
			}
		})
	}
}
