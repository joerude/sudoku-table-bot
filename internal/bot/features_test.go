package bot

import (
	"strings"
	"testing"
)

func TestParseOnOff(t *testing.T) {
	cases := []struct {
		in     string
		on, ok bool
	}{
		{"on", true, true}, {"вкл", true, true}, {"1", true, true},
		{"off", false, true}, {"выкл", false, true}, {"0", false, true},
		{"", false, false}, {"maybe", false, false},
	}
	for _, c := range cases {
		on, ok := parseOnOff(c.in)
		if on != c.on || ok != c.ok {
			t.Errorf("parseOnOff(%q) = (%v, %v), want (%v, %v)", c.in, on, ok, c.on, c.ok)
		}
	}
}

// withFeatures swaps the registry for one test.
func withFeatures(t *testing.T, fs []feature) {
	t.Helper()
	old := features
	features = fs
	t.Cleanup(func() { features = old })
}

func TestFeatureSummaryAndUsage(t *testing.T) {
	withFeatures(t, nil)
	if featureSummary(nil) != "" || featureUsage() != "" {
		t.Errorf("no features: summary and usage must be empty")
	}

	withFeatures(t, []feature{{Key: "x", Label: "Икс"}, {Key: "y", Label: "Игрек"}})
	got := featureSummary(map[string]bool{"x": true, "y": false})
	want := "Икс: <b>вкл</b>\nИгрек: <b>выкл</b>\n"
	if got != want {
		t.Errorf("featureSummary = %q, want %q", got, want)
	}
	usage := featureUsage()
	if !strings.Contains(usage, "/settings x|y on|off") {
		t.Errorf("usage must list the switch keys: %q", usage)
	}
	if strings.ContainsAny(usage, "—–") {
		t.Errorf("no long dashes in bot texts: %q", usage)
	}
	if f, ok := featureByKey("y"); !ok || f.Label != "Игрек" {
		t.Errorf("featureByKey(y) = %+v, %v", f, ok)
	}
	if _, ok := featureByKey("target"); ok {
		t.Errorf("a regular setting must not be taken for a feature")
	}
}

func TestFeatureOnFollowsStore(t *testing.T) {
	st := openTempStore(t)
	const chat = int64(-7104)
	b := &Bot{st: st}
	if !b.featureOn(chat, "x") {
		t.Fatalf("features are on by default")
	}
	if err := st.SetFeature(chat, "x", false); err != nil {
		t.Fatal(err)
	}
	if b.featureOn(chat, "x") {
		t.Errorf("featureOn must follow the stored switch")
	}
}
