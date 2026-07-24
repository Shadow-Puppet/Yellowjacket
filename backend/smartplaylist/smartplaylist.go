// Package smartplaylist builds parameterized SQL WHERE clauses from
// JSON rule definitions and evaluates them against the track_metadata
// view. Field names are whitelisted; values are always parameterized.
package smartplaylist

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"yellowjacket/backend/coverart"
	"yellowjacket/backend/database"
	"yellowjacket/backend/library"
)

// Sentinel errors for rule validation.
var (
	errInvalidField     = errors.New("invalid field: not in allowed field list")
	errInvalidOperator  = errors.New("invalid operator for field type")
	errEmptyIsAnyOf     = errors.New("is_any_of requires at least one value")
	errBetweenCount     = errors.New("between requires exactly 2 values")
	errBetweenFormat    = errors.New("between value must be \"min,max\" or [\"min\",\"max\"]")
	errUnsupportedOp    = errors.New("unsupported operator")
	errInvalidSortField = errors.New("invalid sort field: not in allowed field list")
	errNotNumeric       = errors.New("value must be numeric")
)

// Rule represents a single filter condition for a smart playlist.
type Rule struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// RuleSet holds the complete filter configuration for a smart
// playlist, including optional sort and limit.
type RuleSet struct {
	Rules     []Rule `json:"rules"`
	Limit     int    `json:"limit,omitempty"`
	SortField string `json:"sort_field,omitempty"`
	SortDir   string `json:"sort_dir,omitempty"`
}

// fieldMap maps user-facing rule field names to track_metadata column
// names. Field names MUST come from this map — never interpolated
// from user input.
var fieldMap = map[string]string{
	"title":             "title",
	"artist":            "artist_name",
	"album":             "album",
	"genre":             "genre",
	"year":              "year",
	"release_year":      "release_year",
	"composer":          "composer",
	"file_type":         "file_type",
	"duration":          "length_milliseconds",
	"sample_rate":       "sample_rate",
	"bit_depth":         "bit_depth",
	"channels":          "channels",
	"bitrate":           "bitrate",
	"file_size":         "file_size",
	"library":           "library_id",
	"track_number":      "track_number",
	"disc_number":       "disc_number",
	"play_count":        "play_count",
	"days_since_played": "days_since_played",
}

// numericFields identifies fields that accept numeric operators.
var numericFields = map[string]bool{
	"year":              true,
	"release_year":      true,
	"duration":          true,
	"sample_rate":       true,
	"bit_depth":         true,
	"channels":          true,
	"bitrate":           true,
	"file_size":         true,
	"library":           true,
	"track_number":      true,
	"disc_number":       true,
	"play_count":        true,
	"days_since_played": true,
}

// textOperators are valid operators for text fields.
var textOperators = map[string]bool{
	"is":               true,
	"is_not":           true,
	"contains":         true,
	"does_not_contain": true,
	"starts_with":      true,
	"ends_with":        true,
	"is_any_of":        true,
}

// numericOperators are valid operators for numeric fields.
var numericOperators = map[string]bool{
	"is":           true,
	"is_not":       true,
	"greater_than": true,
	"less_than":    true,
	"between":      true,
}

// genreDelimiter matches the GROUP_CONCAT delimiter used when
// batch-loading genres for matched tracks.
const genreDelimiter = "||"

// BuildWhereClause builds a parameterized SQL WHERE clause from a
// slice of rules. It is a pure function — no database access needed.
// Returns the clause (without the leading "WHERE"), the parameter
// args, and any validation error.
func BuildWhereClause(rules []Rule) (string, []any, error) {
	if len(rules) == 0 {
		return "", nil, nil
	}

	conditions := make([]string, 0, len(rules))
	args := make([]any, 0, len(rules))

	for _, rule := range rules {
		col, ok := fieldMap[rule.Field]
		if !ok {
			return "", nil, fmt.Errorf(
				"%w: %q", errInvalidField, rule.Field,
			)
		}

		isNumeric := numericFields[rule.Field]

		if err := validateOperator(rule.Operator, isNumeric); err != nil {
			return "", nil, fmt.Errorf(
				"field %q: %w", rule.Field, err,
			)
		}

		// All genre operators use a subquery against recording_genres.
		// The smart playlist main query does not project a genre
		// column — genres are batch-loaded after the main query — so
		// even text operators like "contains" must filter through the
		// link table rather than a concatenated column.
		if rule.Field == "genre" {
			cond, condArgs, err := buildGenreCondition(rule)
			if err != nil {
				return "", nil, err
			}

			conditions = append(conditions, cond)
			args = append(args, condArgs...)

			continue
		}

		// days_since_played uses a computed expression, not a column.
		if rule.Field == "days_since_played" {
			cond, condArgs, err := buildDaysSincePlayedCondition(rule)
			if err != nil {
				return "", nil, err
			}

			conditions = append(conditions, cond)
			args = append(args, condArgs...)

			continue
		}

		cond, condArgs, err := buildCondition(col, rule, isNumeric)
		if err != nil {
			return "", nil, err
		}

		conditions = append(conditions, cond)
		args = append(args, condArgs...)
	}

	return strings.Join(conditions, " AND "), args, nil
}

// validateOperator checks that the operator is valid for the field
// type.
func validateOperator(op string, isNumeric bool) error {
	if isNumeric {
		if !numericOperators[op] {
			return fmt.Errorf(
				"%w: %q for numeric field", errInvalidOperator, op,
			)
		}
	} else {
		if !textOperators[op] {
			return fmt.Errorf(
				"%w: %q for text field", errInvalidOperator, op,
			)
		}
	}

	return nil
}

// buildGenreCondition generates a subquery condition against
// recording_genres JOIN genres for every supported text operator.
// The outer query is expected to expose the `recording_id` column of
// the audio file (aliased through the smart playlist query), which is
// compared against recording_genres.recording_id.
func buildGenreCondition(rule Rule) (string, []any, error) {
	inHead := `af.recording_id IN (
  SELECT rg_sub.recording_id FROM recording_genres rg_sub
  JOIN genres g ON rg_sub.genre_id = g.id
  WHERE `
	notInHead := `af.recording_id NOT IN (
  SELECT rg_sub.recording_id FROM recording_genres rg_sub
  JOIN genres g ON rg_sub.genre_id = g.id
  WHERE `

	switch rule.Operator {
	case "is":
		return inHead + "g.name = ? COLLATE NOCASE)", []any{rule.Value}, nil

	case "is_not":
		return notInHead + "g.name = ? COLLATE NOCASE)", []any{rule.Value}, nil

	case "contains":
		return inHead + "g.name LIKE ?)",
			[]any{"%" + rule.Value + "%"}, nil

	case "does_not_contain":
		return notInHead + "g.name LIKE ?)",
			[]any{"%" + rule.Value + "%"}, nil

	case "starts_with":
		return inHead + "g.name LIKE ?)",
			[]any{rule.Value + "%"}, nil

	case "ends_with":
		return inHead + "g.name LIKE ?)",
			[]any{"%" + rule.Value}, nil

	case "is_any_of":
		var values []string

		if err := json.Unmarshal(
			[]byte(rule.Value), &values,
		); err != nil {
			return "", nil, fmt.Errorf(
				"field %q: is_any_of value must be a JSON "+
					"string array: %w",
				rule.Field, err,
			)
		}

		if len(values) == 0 {
			return "", nil, fmt.Errorf(
				"field %q: %w", rule.Field, errEmptyIsAnyOf,
			)
		}

		placeholders := make([]string, len(values))
		condArgs := make([]any, len(values))

		for i, v := range values {
			placeholders[i] = "? COLLATE NOCASE"
			condArgs[i] = v
		}

		return inHead + "g.name IN (" +
			strings.Join(placeholders, ", ") + "))", condArgs, nil

	default:
		return "", nil, fmt.Errorf(
			"%w: %q", errUnsupportedOp, rule.Operator,
		)
	}
}

// buildDaysSincePlayedCondition generates a condition for the
// days_since_played computed field. Uses julianday() to compute
// the number of days between last_played and now. Tracks that
// have never been played (last_played IS NULL) are treated as
// having infinite days since played — they match "greater_than"
// any value but not "less_than".
func buildDaysSincePlayedCondition(rule Rule) (string, []any, error) {
	// The expression: days since last played.
	// NULL handling: COALESCE to a very old date so never-played
	// tracks always have a large days_since_played value.
	expr := "CAST(julianday('now') - julianday(COALESCE(last_played, '2000-01-01')) AS INTEGER)"

	switch rule.Operator {
	case "is":
		v, err := parseNumericValue(rule.Field, rule.Operator, rule.Value)
		if err != nil {
			return "", nil, err
		}

		return expr + " = ?", []any{v}, nil

	case "is_not":
		v, err := parseNumericValue(rule.Field, rule.Operator, rule.Value)
		if err != nil {
			return "", nil, err
		}

		return expr + " != ?", []any{v}, nil

	case "greater_than":
		v, err := parseNumericValue(rule.Field, rule.Operator, rule.Value)
		if err != nil {
			return "", nil, err
		}

		return expr + " > ?", []any{v}, nil

	case "less_than":
		v, err := parseNumericValue(rule.Field, rule.Operator, rule.Value)
		if err != nil {
			return "", nil, err
		}

		// Never-played tracks (NULL last_played) should NOT match
		// "less than N days" — they haven't been played recently.
		return "last_played IS NOT NULL AND " + expr + " < ?", []any{v}, nil

	case "between":
		lo, hi, err := parseBetweenValue(rule.Field, rule.Value)
		if err != nil {
			return "", nil, err
		}

		return expr + " BETWEEN ? AND ?", []any{lo, hi}, nil

	default:
		return "", nil, fmt.Errorf(
			"%w: %q", errUnsupportedOp, rule.Operator,
		)
	}
}

// buildCondition generates a single SQL condition for a non-genre-
// subquery rule.
func buildCondition(
	col string, rule Rule, isNumeric bool,
) (string, []any, error) {
	switch rule.Operator {
	case "is":
		if isNumeric {
			v, err := parseNumericValue(rule.Field, rule.Operator, rule.Value)
			if err != nil {
				return "", nil, err
			}

			return col + " = ?", []any{v}, nil
		}

		return col + " = ? COLLATE NOCASE", []any{rule.Value}, nil

	case "is_not":
		if isNumeric {
			v, err := parseNumericValue(rule.Field, rule.Operator, rule.Value)
			if err != nil {
				return "", nil, err
			}

			return col + " != ?", []any{v}, nil
		}

		return col + " != ? COLLATE NOCASE", []any{rule.Value}, nil

	case "contains":
		return col + " LIKE ?",
			[]any{"%" + rule.Value + "%"}, nil

	case "does_not_contain":
		return col + " NOT LIKE ?",
			[]any{"%" + rule.Value + "%"}, nil

	case "starts_with":
		return col + " LIKE ?",
			[]any{rule.Value + "%"}, nil

	case "ends_with":
		return col + " LIKE ?",
			[]any{"%" + rule.Value}, nil

	case "is_any_of":
		var values []string

		if err := json.Unmarshal(
			[]byte(rule.Value), &values,
		); err != nil {
			return "", nil, fmt.Errorf(
				"field %q: is_any_of value must be a JSON "+
					"string array: %w",
				rule.Field, err,
			)
		}

		if len(values) == 0 {
			return "", nil, fmt.Errorf(
				"field %q: %w", rule.Field, errEmptyIsAnyOf,
			)
		}

		placeholders := make([]string, len(values))
		condArgs := make([]any, len(values))

		for i, v := range values {
			placeholders[i] = "? COLLATE NOCASE"
			condArgs[i] = v
		}

		return col + " IN (" +
			strings.Join(placeholders, ", ") + ")", condArgs, nil

	case "greater_than":
		v, err := parseNumericValue(rule.Field, rule.Operator, rule.Value)
		if err != nil {
			return "", nil, err
		}

		return col + " > ?", []any{v}, nil

	case "less_than":
		v, err := parseNumericValue(rule.Field, rule.Operator, rule.Value)
		if err != nil {
			return "", nil, err
		}

		return col + " < ?", []any{v}, nil

	case "between":
		lo, hi, err := parseBetweenValue(rule.Field, rule.Value)
		if err != nil {
			return "", nil, err
		}

		return col + " BETWEEN ? AND ?",
			[]any{lo, hi}, nil

	default:
		return "", nil, fmt.Errorf(
			"%w: %q", errUnsupportedOp, rule.Operator,
		)
	}
}

// parseNumericValue converts a string value to int64 for numeric
// field comparisons.
func parseNumericValue(field, op, value string) (int64, error) {
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf(
			"field %q operator %q: %w: %w",
			field, op, errNotNumeric, err,
		)
	}

	return v, nil
}

// parseBetweenValue parses "min,max" or JSON ["min","max"] into two
// integer values.
func parseBetweenValue(
	field, value string,
) (int64, int64, error) {
	// Try JSON array first.
	var arr []string

	if err := json.Unmarshal([]byte(value), &arr); err == nil {
		if len(arr) != 2 {
			return 0, 0, fmt.Errorf(
				"field %q: %w: got %d",
				field, errBetweenCount, len(arr),
			)
		}

		lo, err := strconv.ParseInt(arr[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf(
				"field %q between lo: %w: %w",
				field, errNotNumeric, err,
			)
		}

		hi, err := strconv.ParseInt(arr[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf(
				"field %q between hi: %w: %w",
				field, errNotNumeric, err,
			)
		}

		return lo, hi, nil
	}

	// Fall back to comma-separated.
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf(
			"field %q: %w", field, errBetweenFormat,
		)
	}

	lo, err := strconv.ParseInt(
		strings.TrimSpace(parts[0]), 10, 64,
	)
	if err != nil {
		return 0, 0, fmt.Errorf(
			"field %q between lo: %w: %w",
			field, errNotNumeric, err,
		)
	}

	hi, err := strconv.ParseInt(
		strings.TrimSpace(parts[1]), 10, 64,
	)
	if err != nil {
		return 0, 0, fmt.Errorf(
			"field %q between hi: %w: %w",
			field, errNotNumeric, err,
		)
	}

	return lo, hi, nil
}

// leanTrackQuery is the smart-playlist projection: the same columns
// the `track_metadata` view would expose minus the correlated-subquery
// `genre` aggregate that made the view expensive to scan. Genres are
// batch-loaded after this query returns.
//
// Wrapping the joins in a subquery aliased `af` lets WHERE/ORDER BY
// clauses reference the projected names (`title`, `year`, etc.) the
// same way they would against the view. SQLite flattens this subquery
// so the runtime cost is equivalent to querying the underlying tables
// directly.
const leanTrackQuery = `SELECT
	af.recording_id,
	af.file_path,
	af.length_milliseconds,
	af.title,
	af.artist_name,
	af.track_number,
	af.disc_number,
	af.album,
	af.year,
	af.composer,
	af.file_type,
	af.sample_rate,
	af.bit_depth,
	af.channels,
	af.bitrate,
	af.file_size,
	af.play_count,
	COALESCE(af.last_played, '') AS last_played
FROM (
	SELECT
		af.id,
		af.recording_id,
		af.file_path,
		af.length_milliseconds,
		COALESCE(r.name, '') AS title,
		COALESCE(ac.text, '') AS artist_name,
		r.track_number,
		r.disc_number,
		COALESCE(rg.name, '') AS album,
		-- Two year fields, matching the canonical track_metadata view:
		--   year         — the album's original (first-release) year,
		--                  the default users filter on. A 1977 album owned
		--                  as a 2010s reissue still filters as 1977.
		--   release_year — the year of the specific release in the library
		--                  (the file/release-group tag), e.g. 2013 for that
		--                  reissue.
		-- Both fall back through rg.year → r.year so a track without full
		-- MusicBrainz data still gets a sensible year.
		COALESCE(rg.original_year, rg.year, r.year, 0) AS year,
		COALESCE(rg.year, r.year, 0) AS release_year,
		COALESCE(r.composer, '') AS composer,
		COALESCE(ft.extension, '') AS file_type,
		af.sample_rate,
		af.bit_depth,
		af.channels,
		af.bitrate,
		af.file_size,
		af.library_id,
		af.play_count,
		af.last_played
	FROM audio_files af
	LEFT JOIN recordings r ON af.recording_id = r.id
	LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
	LEFT JOIN (
		SELECT recording_id,
			MIN(release_group_id) AS release_group_id
		FROM release_group_recordings
		GROUP BY recording_id
	) rgr ON r.id = rgr.recording_id
	LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
	LEFT JOIN file_types ft ON af.file_type_id = ft.id
) af`

// Evaluate runs the rule set against the library and returns matching
// tracks. It issues two queries: one lean SELECT over the joined
// metadata tables (no genre) and a batched follow-up to attach genres
// to the matched tracks. This avoids the per-row correlated genre
// subquery in `track_metadata_view`, which scaled with library size
// rather than result size.
func Evaluate(
	db *database.DB, ruleSet RuleSet,
) ([]library.Track, error) {
	start := time.Now()
	logger := db.Logger()

	where, args, err := BuildWhereClause(ruleSet.Rules)
	if err != nil {
		return nil, fmt.Errorf(
			"smart playlist rule error: %w", err,
		)
	}

	// Sort-by-genre has no single SQL column to sort on (genres are
	// many-to-one per track). Detect it up front so we can sort in
	// Go after genres are merged.
	sortByGenre := ruleSet.SortField == "genre"

	// SAFETY: Dynamic WHERE clause built from whitelisted field
	// names and parameterized values only. Sort field is validated
	// against fieldMap. No user-supplied strings are interpolated.
	query := leanTrackQuery

	if where != "" {
		query += "\nWHERE " + where
	}

	// Sort.
	applyLimitInSQL := ruleSet.Limit > 0

	switch {
	case sortByGenre:
		// Sort applied in Go after the batch genre merge. If a LIMIT
		// was requested we also defer it so the ordering is computed
		// over the full candidate set.
		applyLimitInSQL = false

	case ruleSet.SortField == "random":
		query += "\nORDER BY RANDOM()"

	case ruleSet.SortField != "":
		sortCol, ok := fieldMap[ruleSet.SortField]
		if !ok {
			return nil, fmt.Errorf(
				"%w: %q", errInvalidSortField,
				ruleSet.SortField,
			)
		}

		dir := "ASC"
		if strings.EqualFold(ruleSet.SortDir, "DESC") {
			dir = "DESC"
		}

		query += "\nORDER BY " + sortCol + " " + dir
	}

	if applyLimitInSQL {
		query += "\nLIMIT ?"

		args = append(args, ruleSet.Limit)
	}

	mainStart := time.Now()

	rows, err := db.QueryContext(query, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"smart playlist query failed: %w", err,
		)
	}

	tracks, recordingIDs, err := scanTracks(rows)

	_ = rows.Close()

	if err != nil {
		return nil, err
	}

	mainDuration := time.Since(mainStart)

	// Batch-load genres for every matched recording_id in one query
	// instead of the per-row correlated subquery the view used.
	genreStart := time.Now()

	genresByRecording, err := fetchGenres(db, recordingIDs)
	if err != nil {
		return nil, err
	}

	for i, rid := range recordingIDs {
		if g, ok := genresByRecording[rid]; ok {
			tracks[i].Genre = splitGenres(g)
		}
	}

	genreDuration := time.Since(genreStart)

	// Batch-load cover art + MusicBrainz IDs for the matched rows only.
	// These fields are presentation-only (track-row styling); keeping
	// them out of the lean query avoids a per-row correlated subquery
	// and cover-art join over the whole library before WHERE/LIMIT.
	artStart := time.Now()

	artworkByRecording, err := fetchArtwork(db, recordingIDs)
	if err != nil {
		return nil, err
	}

	for i, rid := range recordingIDs {
		art, ok := artworkByRecording[rid]
		if !ok {
			continue
		}

		tracks[i].ArtistMBID = art.artistMBID
		tracks[i].ReleaseGroupMBID = art.releaseGroupMBID
		tracks[i].RecordingMBID = art.recordingMBID

		if art.coverArtPath != "" {
			urls := coverart.ResolveURLs(art.coverArtPath)
			tracks[i].CoverArtPath = urls.Original
			tracks[i].CoverArtSmall = urls.Small
			tracks[i].CoverArtMedium = urls.Medium
			tracks[i].CoverArtLarge = urls.Large
		}
	}

	artDuration := time.Since(artStart)

	// Apply genre-sort and deferred LIMIT in Go if needed.
	if sortByGenre {
		dir := 1
		if strings.EqualFold(ruleSet.SortDir, "DESC") {
			dir = -1
		}

		sort.SliceStable(tracks, func(i, j int) bool {
			return dir*strings.Compare(
				strings.Join(tracks[i].Genre, genreDelimiter),
				strings.Join(tracks[j].Genre, genreDelimiter),
			) < 0
		})

		if ruleSet.Limit > 0 && len(tracks) > ruleSet.Limit {
			tracks = tracks[:ruleSet.Limit]
		}
	}

	logger.Debug(
		"smart playlist evaluated",
		"tracks", len(tracks),
		"main_ms", mainDuration.Milliseconds(),
		"genres_ms", genreDuration.Milliseconds(),
		"artwork_ms", artDuration.Milliseconds(),
		"total_ms", time.Since(start).Milliseconds(),
	)

	return tracks, nil
}

// scanTracks reads all rows from a lean-query result into parallel
// slices: the Track values (minus genres, which are attached later)
// and the recording_id for each, used for the batched genre fetch.
func scanTracks(rows *sql.Rows) ([]library.Track, []int64, error) {
	var (
		tracks       []library.Track
		recordingIDs []int64
	)

	for rows.Next() {
		var (
			recordingID sql.NullInt64
			filePath    string
			lengthMs    int64
			title       string
			artistName  string
			trackNumber sql.NullInt64
			discNumber  sql.NullInt64
			album       string
			year        int64
			composer    string
			fileType    string
			sampleRate  int64
			bitDepth    int64
			channels    int64
			bitrate     int64
			fileSize    int64
			playCount   int64
			lastPlayed  string
		)

		if err := rows.Scan(
			&recordingID, &filePath, &lengthMs, &title, &artistName,
			&trackNumber, &discNumber,
			&album, &year, &composer, &fileType,
			&sampleRate, &bitDepth, &channels,
			&bitrate, &fileSize,
			&playCount, &lastPlayed,
		); err != nil {
			return nil, nil, fmt.Errorf(
				"could not scan smart playlist row: %w", err,
			)
		}

		track := library.Track{
			TrackName:   title,
			ArtistName:  artistName,
			TrackLength: strconv.FormatInt(lengthMs, 10),
			FilePath:    filePath,
			TrackNumber: trackNumber.Int64,
			DiscNumber:  discNumber.Int64,
			Album:       album,
			Year:        year,
			Composer:    composer,
			FileType:    fileType,
			SampleRate:  sampleRate,
			BitDepth:    bitDepth,
			Channels:    channels,
			Bitrate:     bitrate,
			FileSize:    fileSize,
			PlayCount:   playCount,
			LastPlayed:  lastPlayed,
		}

		tracks = append(tracks, track)
		recordingIDs = append(recordingIDs, recordingID.Int64)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf(
			"smart playlist row iteration error: %w", err,
		)
	}

	return tracks, recordingIDs, nil
}

// fetchGenres batch-loads the GROUP_CONCAT-joined genre string for
// every recording_id in ids using a single IN-list query. Returns a
// map from recording_id to the concatenated genre string.
func fetchGenres(
	db *database.DB, ids []int64,
) (map[int64]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Deduplicate to keep the IN list minimal.
	seen := make(map[int64]struct{}, len(ids))
	unique := make([]int64, 0, len(ids))

	for _, id := range ids {
		if id == 0 {
			continue
		}

		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}

		unique = append(unique, id)
	}

	if len(unique) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(unique))
	args := make([]any, len(unique))

	for i, id := range unique {
		placeholders[i] = "?"
		args[i] = id
	}

	// SAFETY: placeholders are static "?" tokens; every value is
	// parameterized.
	query := `SELECT rg_sub.recording_id,
		GROUP_CONCAT(g.name, '` + genreDelimiter + `')
	FROM recording_genres rg_sub
	JOIN genres g ON rg_sub.genre_id = g.id
	WHERE rg_sub.recording_id IN (` +
		strings.Join(placeholders, ", ") + `)
	GROUP BY rg_sub.recording_id`

	rows, err := db.QueryContext(query, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"smart playlist genre fetch failed: %w", err,
		)
	}

	defer func() { _ = rows.Close() }()

	result := make(map[int64]string, len(unique))

	for rows.Next() {
		var (
			rid   int64
			names string
		)

		if err := rows.Scan(&rid, &names); err != nil {
			return nil, fmt.Errorf(
				"could not scan smart playlist genre row: %w", err,
			)
		}

		result[rid] = names
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"smart playlist genre iteration error: %w", err,
		)
	}

	return result, nil
}

// trackArtwork holds the presentation-only cover-art path and
// MusicBrainz identifiers attached to a matched track after the main
// filter query, keyed by recording_id.
type trackArtwork struct {
	coverArtPath     string
	artistMBID       string
	releaseGroupMBID string
	recordingMBID    string
}

// fetchArtwork batch-loads cover-art paths and MusicBrainz IDs for the
// given recording_ids in a single IN-list query. These fields drive
// track-row styling only, so scoping them to the matched result set
// keeps the cost proportional to results rather than library size.
func fetchArtwork(
	db *database.DB, ids []int64,
) (map[int64]trackArtwork, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Deduplicate to keep the IN list minimal.
	seen := make(map[int64]struct{}, len(ids))
	unique := make([]int64, 0, len(ids))

	for _, id := range ids {
		if id == 0 {
			continue
		}

		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}

		unique = append(unique, id)
	}

	if len(unique) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(unique))

	for i := range unique {
		placeholders[i] = "?"
	}

	inList := strings.Join(placeholders, ", ")

	// A recording's artist credit can name several artists; the old
	// correlated subquery picked one via LIMIT 1. GROUP BY r.id with
	// MIN() reproduces a single stable value without multiplying rows.
	// SAFETY: placeholders are static "?" tokens; every value is
	// parameterized. The IN list is bound twice (subquery + outer).
	query := `SELECT r.id,
		COALESCE(MIN(ca.file_path), '') AS cover_art_path,
		COALESCE(MIN(a.mbid), '') AS artist_mbid,
		COALESCE(MIN(rg.mbid), '') AS release_group_mbid,
		COALESCE(r.mbid, '') AS recording_mbid
	FROM recordings r
	LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
	LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
	LEFT JOIN artists a ON a.id = aca.artist_id
	LEFT JOIN (
		SELECT recording_id,
			MIN(release_group_id) AS release_group_id
		FROM release_group_recordings
		WHERE recording_id IN (` + inList + `)
		GROUP BY recording_id
	) rgr ON r.id = rgr.recording_id
	LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
	LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
	WHERE r.id IN (` + inList + `)
	GROUP BY r.id`

	args := make([]any, 0, len(unique)*2)
	for range 2 {
		for _, id := range unique {
			args = append(args, id)
		}
	}

	rows, err := db.QueryContext(query, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"smart playlist artwork fetch failed: %w", err,
		)
	}

	defer func() { _ = rows.Close() }()

	result := make(map[int64]trackArtwork, len(unique))

	for rows.Next() {
		var (
			rid int64
			art trackArtwork
		)

		if err := rows.Scan(
			&rid, &art.coverArtPath, &art.artistMBID,
			&art.releaseGroupMBID, &art.recordingMBID,
		); err != nil {
			return nil, fmt.Errorf(
				"could not scan smart playlist artwork row: %w", err,
			)
		}

		result[rid] = art
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"smart playlist artwork iteration error: %w", err,
		)
	}

	return result, nil
}

// ParseRuleSet parses a JSON string into a validated RuleSet.
func ParseRuleSet(jsonStr string) (RuleSet, error) {
	var rs RuleSet

	if err := json.Unmarshal(
		[]byte(jsonStr), &rs,
	); err != nil {
		return RuleSet{}, fmt.Errorf(
			"invalid smart playlist rules JSON: %w", err,
		)
	}

	return rs, nil
}

// splitGenres splits a GROUP_CONCAT genre string into individual
// genre names. An empty string returns nil.
func splitGenres(concatenated string) []string {
	if concatenated == "" {
		return nil
	}

	return strings.Split(concatenated, genreDelimiter)
}
