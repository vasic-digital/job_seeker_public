package ext

// Strategy picks one candidate among several for a decision point (which channel, which ATS profile, which
// format for a country, which Listing of a Position). It is the only place where "more than one plugin
// applies" is resolved, so the choice is explainable and testable on its own.
type Strategy[C any] interface {
	// ID is the value stored in policy `strategy.<port>`.
	ID() string
	// Choose returns the index of the chosen candidate, or -1 with a reason when none is acceptable.
	Choose(cands []C, in StrategyInput) (idx int, why Reason)
}

// StrategyInput is the data every strategy may consult (ids and enums only).
type StrategyInput struct {
	Country, Lang, Host string
	Favourites          []string // ordered ids from policy/operator
	Strict              bool     // favourites_strict
}

// Named is a candidate with a stable id.
type Named interface{ NameID() string }

// FavouritesFirst chooses the first candidate whose id is in Favourites order; with Strict=false the
// remaining candidates follow in the given (already deterministic) order.
type FavouritesFirst[C Named] struct{}

func (FavouritesFirst[C]) ID() string { return "favourites_first" }

func (FavouritesFirst[C]) Choose(c []C, in StrategyInput) (int, Reason) {
	for _, f := range in.Favourites {
		for i, x := range c {
			if x.NameID() == f {
				return i, Reason{Code: "strategy_favourite", Refs: map[string]string{"id": f}}
			}
		}
	}
	if in.Strict || len(c) == 0 {
		return -1, Reason{Code: "strategy_none_acceptable"}
	}
	return 0, Reason{Code: "strategy_first_ranked", Refs: map[string]string{"id": c[0].NameID()}}
}
