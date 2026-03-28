package core

import "testing"

func TestCountingPoolEmpty(t *testing.T) {
	pool := NewCountingPool[int](nil)
	if pool.Next() != nil {
		t.Fatal("expected nil for empty pool")
	}
	if !pool.IsEmpty() {
		t.Fatal("expected empty")
	}
}

func TestCountingPoolSingleItem(t *testing.T) {
	pool := NewCountingPool([]string{"only"})
	for i := 0; i < 10; i++ {
		v := pool.Next()
		if v == nil || *v != "only" {
			t.Fatalf("expected 'only', got %v", v)
		}
	}
}

func TestCountingPoolDistributesEvenly(t *testing.T) {
	pool := NewCountingPool([]string{"a", "b", "c", "d"})
	counts := map[string]int{}
	for i := 0; i < 400; i++ {
		v := pool.Next()
		counts[*v]++
	}
	for item, count := range counts {
		if count != 100 {
			t.Errorf("item %q got %d, expected 100", item, count)
		}
	}
}

func TestCountingPoolNextExcluding(t *testing.T) {
	pool := NewCountingPool([]string{"a", "b"})
	for i := 0; i < 10; i++ {
		v := pool.NextExcluding(func(s string) bool { return s == "a" })
		if v == nil || *v != "b" {
			t.Fatalf("expected 'b', got %v", v)
		}
	}
}

func TestCountingPoolNextExcludingFallback(t *testing.T) {
	pool := NewCountingPool([]string{"only"})
	v := pool.NextExcluding(func(s string) bool { return s == "only" })
	if v == nil || *v != "only" {
		t.Fatalf("expected fallback to 'only'")
	}
}

func TestCountingPoolNextExcludingEmpty(t *testing.T) {
	pool := NewCountingPool[string](nil)
	v := pool.NextExcluding(func(s string) bool { return true })
	if v != nil {
		t.Fatal("expected nil for empty pool")
	}
}
