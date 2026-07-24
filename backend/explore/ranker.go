package explore

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
)

// This file sketches a learned ranking model that replaces the
// hand-tuned scoring constants (the fw* feature weights, tierBonus,
// rgTierBonus, the intent-prior multipliers) with weights that can be
// trained from the user's own click history.
//
// Status: NOT yet wired into Search().  The integration point is the
// candidate scorers in the top-results pipeline
// (scoreArtistCandidate / scoreReleaseGroupCandidate /
// scoreRecordingCandidate) and the main rerank.  Swap those additive
// constant sums for RankFeatures + LinearModel.Score once the eval
// harness has real fixtures to prove the change is a win.
//
// Why a linear model and not something fancier: it needs no scale, no
// GPU, trains in microseconds on a desktop's worth of clicks, and —
// crucially — personalises to ONE user.  That is the lever a
// multi-million-user system gets from aggregate logs; we get the same
// shape of signal from a single user's repeated intent.
//
// Training prerequisite (important): logistic training needs negatives,
// i.e. results that were SHOWN but NOT clicked.  search_clicks records
// only clicks today.  To train properly, log impressions too (the
// MBIDs shown for a query); clicked rows are positive labels, the rest
// of the shown set are negatives.  Until impressions are logged, use
// DefaultModel(), whose weights reproduce the current behaviour.

// RankFeatures is the feature vector for a single candidate.  Every
// field is normalised to roughly [0,1] so weights are comparable.
type RankFeatures struct {
	// Textual match strength against the query (mutually exclusive
	// tiers collapsed to a single 0..1 magnitude: exact=1.0,
	// prefix=0.6, whole-word=0.4, substring=0.2, none=0).
	NameMatch float64

	// Artist-credit match strength, same scale.  Lets "abbey road
	// beatles" reward the album credited to The Beatles.
	ArtistMatch float64

	// Log-scaled popularity and listener count, both via normLog so
	// they share the fixed reference scale.
	LogPopularity float64
	LogListeners  float64

	// Personalisation signals.
	InLibrary float64 // 1.0 when owned, else 0
	IsSimilar float64 // 0..1 similarity to an owned artist

	// Recency-decayed per-query click signal for this candidate.
	ClickRate float64
}

// rankFeatureCount is the number of features (excluding bias).  Used by
// the gradient step to iterate fields generically.
const rankFeatureCount = 7

// asSlice returns the features in a stable order so Score and Update
// agree on indexing.
func (f RankFeatures) asSlice() [rankFeatureCount]float64 {
	return [rankFeatureCount]float64{
		f.NameMatch,
		f.ArtistMatch,
		f.LogPopularity,
		f.LogListeners,
		f.InLibrary,
		f.IsSimilar,
		f.ClickRate,
	}
}

// LinearModel scores a candidate as bias + Σ wᵢ·featureᵢ.  For ranking
// the raw score is what matters; the logistic squashing is used only
// during training to produce a probability for the gradient.
type LinearModel struct {
	Bias    float64                   `json:"bias"`
	Weights [rankFeatureCount]float64 `json:"weights"`
}

// DefaultModel returns weights that reproduce the current hand-tuned
// scorer, so swapping the model in with no training leaves behaviour
// unchanged.  The values mirror the fw* constants in explore.go.
func DefaultModel() LinearModel {
	return LinearModel{
		Bias: 0,
		Weights: [rankFeatureCount]float64{
			1.00, // NameMatch     ~ fwExactTitle / fwPrefixTitle blend
			0.90, // ArtistMatch   ~ fwExactArtist
			0.80, // LogPopularity ~ fwListenLog
			0.60, // LogListeners  ~ fwListenerLog
			0.50, // InLibrary     ~ fwInLibrary
			0.20, // IsSimilar     ~ fwSimilar
			0.30, // ClickRate     ~ click feature cap
		},
	}
}

// Score returns the un-squashed ranking score for a candidate.  Higher
// is better.  This is the value to sort by.
func (m LinearModel) Score(f RankFeatures) float64 {
	score := m.Bias
	fs := f.asSlice()

	for i := range fs {
		score += m.Weights[i] * fs[i]
	}

	return score
}

// Probability squashes the score to (0,1) via the logistic function.
// Used during training to compute the gradient.
func (m LinearModel) Probability(f RankFeatures) float64 {
	return 1.0 / (1.0 + math.Exp(-m.Score(f)))
}

// Sample is one training example: a candidate's features and whether
// the user clicked it (1.0) or saw-but-skipped it (0.0).
type Sample struct {
	Features RankFeatures
	Label    float64
}

// Update performs one logistic-regression SGD step toward the label.
// learningRate is typically ~0.05.  Returns the pre-update prediction
// so callers can track convergence.
func (m *LinearModel) Update(s Sample, learningRate float64) float64 {
	pred := m.Probability(s.Features)
	err := s.Label - pred
	fs := s.Features.asSlice()

	m.Bias += learningRate * err

	for i := range fs {
		m.Weights[i] += learningRate * err * fs[i]
	}

	return pred
}

// Train runs SGD over the samples for the given number of epochs.  A
// few hundred clicks over a handful of epochs converges fine; this is
// cheap enough to run on startup or after a search session.
func Train(model *LinearModel, samples []Sample, epochs int, learningRate float64) {
	for range epochs {
		for _, s := range samples {
			model.Update(s, learningRate)
		}
	}
}

// matchStrength collapses the mutually-exclusive textual match tiers
// used throughout the current scorers into a single magnitude, so the
// model has one weight to learn instead of four overlapping constants.
func matchStrength(exact, prefix, wholeWord, substring bool) float64 {
	switch {
	case exact:
		return 1.0
	case prefix:
		return 0.6
	case wholeWord:
		return 0.4
	case substring:
		return 0.2
	default:
		return 0.0
	}
}

// SaveModel writes the model as JSON.  Callers persist this to disk or
// the index meta table; the model is small (8 floats).
func SaveModel(w io.Writer, m LinearModel) error {
	if err := json.NewEncoder(w).Encode(m); err != nil {
		return fmt.Errorf("ranker: encode model: %w", err)
	}

	return nil
}

// LoadModel reads a model previously written by SaveModel.
func LoadModel(r io.Reader) (LinearModel, error) {
	var m LinearModel

	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return LinearModel{}, fmt.Errorf("ranker: decode model: %w", err)
	}

	return m, nil
}
