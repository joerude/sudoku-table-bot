package storage

import "testing"

func TestStateMarker(t *testing.T) {
	st := openTemp(t)
	if v, err := st.State("last_backup"); err != nil || v != "" {
		t.Fatalf("unset marker = %q, %v; want empty", v, err)
	}
	for _, want := range []string{"2026-W48", "2026-W49"} {
		if err := st.SetState("last_backup", want); err != nil {
			t.Fatal(err)
		}
		if v, _ := st.State("last_backup"); v != want {
			t.Errorf("marker = %q, want %q", v, want)
		}
	}
	if v, _ := st.State("other"); v != "" {
		t.Errorf("another key must stay unset, got %q", v)
	}
}
