package lifestyle

import "testing"

func TestCategoriesAndValidation(t *testing.T) {
	if len(Categories()) != 6 {
		t.Fatal("categories")
	}
	sleep, ok := Lookup("sleep")
	if !ok || !sleep.ValidValue(7.5) || sleep.ValidValue(25) || sleep.ValidValue(-1) {
		t.Fatal("sleep range")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("unknown category")
	}
	mood, _ := Lookup("mood")
	if mood.ValidValue(0) || !mood.ValidValue(3) {
		t.Fatal("mood range")
	}
	if got := mood.Combine([]float64{2, 4}); got != 3 {
		t.Fatalf("mean %v", got)
	}
	if got := sleep.Combine([]float64{3, 4}); got != 7 {
		t.Fatalf("sum %v", got)
	}
	if got := mood.Combine(nil); got != 0 {
		t.Fatalf("empty %v", got)
	}
}

func TestPearson(t *testing.T) {
	x := []float64{1, 2, 3, 4, 5, 6, 7, 8}
	up := []float64{2, 4, 6, 8, 10, 12, 14, 16}
	down := []float64{16, 14, 12, 10, 8, 6, 4, 2}
	if c, ok := Pearson(x, up); !ok || c.R != 1 || c.Strength != "strong positive" || c.Days != 8 {
		t.Fatalf("up %+v", c)
	}
	if c, ok := Pearson(x, down); !ok || c.R != -1 || c.Strength != "strong negative" {
		t.Fatalf("down %+v", c)
	}
	if c, ok := Pearson(x, []float64{1, 5, 2, 6, 1, 5, 2, 6}); !ok || c.Strength == "" {
		t.Fatalf("mixed %+v", c)
	}
	if _, ok := Pearson(x[:3], up[:3]); ok {
		t.Fatal("too few")
	}
	if _, ok := Pearson(x, []float64{1, 1, 1, 1, 1, 1, 1, 1}); ok {
		t.Fatal("no variance")
	}
	if _, ok := Pearson(x, up[:5]); ok {
		t.Fatal("length mismatch")
	}
	for r, want := range map[float64]string{0.1: "negligible", 0.3: "weak positive", -0.5: "moderate negative"} {
		if got := strength(r); got != want {
			t.Errorf("%v: %s", r, got)
		}
	}
	if k := SortedKeys(map[string]int{"b": 1, "a": 2}); k[0] != "a" {
		t.Fatal("keys")
	}
}
