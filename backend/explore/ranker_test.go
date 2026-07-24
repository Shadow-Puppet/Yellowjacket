package explore

import (
	"bytes"
	"math"
	"testing"
)

func TestDefaultModelScoreOrdering(t *testing.T) {
	m := DefaultModel()

	// A popular, in-library exact match must outscore an obscure
	// substring match.
	strong := RankFeatures{NameMatch: 1.0, LogPopularity: 0.9, InLibrary: 1.0}
	weak := RankFeatures{NameMatch: 0.2, LogPopularity: 0.1}

	if m.Score(strong) <= m.Score(weak) {
		t.Errorf("strong candidate %.3f should outscore weak %.3f",
			m.Score(strong), m.Score(weak))
	}
}

func TestUpdateMovesTowardLabel(t *testing.T) {
	m := DefaultModel()

	// A candidate the user repeatedly clicks should see its predicted
	// probability rise after training on positive labels.
	f := RankFeatures{NameMatch: 0.4, LogPopularity: 0.2}

	before := m.Probability(f)

	for range 50 {
		m.Update(Sample{Features: f, Label: 1.0}, 0.1)
	}

	after := m.Probability(f)

	if after <= before {
		t.Errorf("probability should rise toward positive label: before=%.4f after=%.4f",
			before, after)
	}
}

func TestUpdateLearnsNegative(t *testing.T) {
	m := DefaultModel()

	f := RankFeatures{NameMatch: 0.9, LogPopularity: 0.9}

	before := m.Probability(f)

	// Shown repeatedly, never clicked — probability should fall.
	for range 50 {
		m.Update(Sample{Features: f, Label: 0.0}, 0.1)
	}

	after := m.Probability(f)

	if after >= before {
		t.Errorf("probability should fall toward negative label: before=%.4f after=%.4f",
			before, after)
	}
}

func TestMatchStrengthTiers(t *testing.T) {
	if matchStrength(true, false, false, false) != 1.0 {
		t.Error("exact match should be 1.0")
	}

	if matchStrength(false, false, false, false) != 0.0 {
		t.Error("no match should be 0.0")
	}

	// Tiers must be strictly ordered.
	exact := matchStrength(true, false, false, false)
	prefix := matchStrength(false, true, false, false)
	word := matchStrength(false, false, true, false)
	sub := matchStrength(false, false, false, true)

	if !(exact > prefix && prefix > word && word > sub) {
		t.Errorf("tiers not strictly ordered: %v %v %v %v", exact, prefix, word, sub)
	}
}

func TestModelRoundTrip(t *testing.T) {
	m := DefaultModel()
	m.Bias = 0.123
	m.Weights[0] = 0.777

	var buf bytes.Buffer
	if err := SaveModel(&buf, m); err != nil {
		t.Fatalf("SaveModel: %v", err)
	}

	got, err := LoadModel(&buf)
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}

	if math.Abs(got.Bias-m.Bias) > 1e-9 || math.Abs(got.Weights[0]-m.Weights[0]) > 1e-9 {
		t.Errorf("round trip mismatch: got %+v want %+v", got, m)
	}
}
