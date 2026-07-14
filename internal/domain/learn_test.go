package domain

import (
	"math/rand"
	"strings"
	"testing"
)

func TestTechniquesCatalog(t *testing.T) {
	all := Techniques()
	if len(all) != 18 {
		t.Fatalf("Techniques() = %d entries, want 18", len(all))
	}

	seen := map[string]bool{}
	for _, tech := range all {
		if seen[tech.Key] {
			t.Errorf("duplicate key %q", tech.Key)
		}
		seen[tech.Key] = true

		if tech.Key == "" || len(tech.Key) > 24 {
			t.Errorf("key %q: want non-empty, <=24 bytes", tech.Key)
		}
		for _, r := range tech.Key {
			if r > 127 || r == ':' || r == '|' {
				t.Errorf("key %q: want plain ascii without separators", tech.Key)
				break
			}
		}
		if tech.Name == "" || tech.Alias == "" || tech.Blurb == "" {
			t.Errorf("%s: Name/Alias/Blurb must be non-empty", tech.Key)
		}
		if !strings.HasPrefix(tech.URL, "https://") {
			t.Errorf("%s: URL %q must be https", tech.Key, tech.URL)
		}
		if tech.Wiki != "" && !strings.HasPrefix(tech.Wiki, "https://") {
			t.Errorf("%s: Wiki %q must be https", tech.Key, tech.Wiki)
		}
		if tech.Wiki == tech.URL && tech.Wiki != "" {
			t.Errorf("%s: Wiki duplicates URL", tech.Key)
		}
	}
}

func TestTechniquesByTier(t *testing.T) {
	for _, tier := range Tiers() {
		got := TechniquesByTier(tier)
		if len(got) != 6 {
			t.Errorf("tier %s: %d techniques, want 6", tier, len(got))
		}
		for _, tech := range got {
			if tech.Tier != tier {
				t.Errorf("tier %s: got technique %s of tier %s", tier, tech.Key, tech.Tier)
			}
		}
	}
	if got := TechniquesByTier("nope"); got != nil {
		t.Errorf("unknown tier: got %v, want nil", got)
	}
}

func TestTechniqueByKey(t *testing.T) {
	for _, want := range Techniques() {
		got, ok := TechniqueByKey(want.Key)
		if !ok || got.Key != want.Key {
			t.Errorf("TechniqueByKey(%q) = %v, %v", want.Key, got.Key, ok)
		}
	}
	if _, ok := TechniqueByKey("does-not-exist"); ok {
		t.Error("TechniqueByKey(garbage) = ok, want !ok")
	}
}

func TestRandomTechnique(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	first := RandomTechnique(r)
	if _, ok := TechniqueByKey(first.Key); !ok {
		t.Fatalf("RandomTechnique returned %q, not in catalog", first.Key)
	}
	// Same seed → same sequence (deterministic for tests and replayable in prod).
	again := RandomTechnique(rand.New(rand.NewSource(42)))
	if again.Key != first.Key {
		t.Errorf("same seed gave %q then %q", first.Key, again.Key)
	}
}
