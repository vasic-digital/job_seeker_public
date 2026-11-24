package ext

import "testing"

type n string

func (x n) NameID() string { return string(x) }

func TestFavouritesFirst(t *testing.T) {
	s := FavouritesFirst[n]{}
	c := []n{"a", "b", "c"}
	if i, r := s.Choose(c, StrategyInput{Favourites: []string{"c", "a"}}); i != 2 || r.Code != "strategy_favourite" {
		t.Fatalf("%d %v", i, r)
	}
	if i, _ := s.Choose(c, StrategyInput{}); i != 0 {
		t.Fatal("non-strict falls back to first ranked")
	}
	if i, r := s.Choose(c, StrategyInput{Favourites: []string{"zzz"}, Strict: true}); i != -1 || r.Code != "strategy_none_acceptable" {
		t.Fatal("strict must refuse rather than guess")
	}
}
