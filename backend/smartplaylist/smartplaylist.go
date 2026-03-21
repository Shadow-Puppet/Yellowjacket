// Package smartplaylist builds parameterized SQL WHERE clauses from
// JSON rule definitions and evaluates them against the track_metadata
// view. Field names are whitelisted; values are always parameterized.
package smartplaylist

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
	"title":        "title",
	"artist":       "artist_name",
	"album":        "album",
	"genre":        "genre",
	"year":         "year",
	"composer":     "composer",
	"file_type":    "file_type",
	"duration":     "length_milliseconds",
	"sample_rate":  "sample_rate",
	"bit_depth":    "bit_depth",
	"channels":     "channels",
	"bitrate":      "bitrate",
	"file_size":    "file_size",
	"library":      "library_id",
	"track_number": "track_number",
	"disc_number":  "disc_number",
}

// numericFields identifies fields that accept numeric operators.
var numericFields = map[string]bool{
	"year":         true,
	"duration":     true,
	"sample_rate":  true,
	"bit_depth":    true,
	"channels":     true,
	"bitrate":      true,
	"file_size":    true,
	"library":      true,
	"track_number": true,
	"disc_number":  true,
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

// genreExactOps require a subquery against recording_genres JOIN
// genres instead of matching the concatenated genre column.
var genreExactOps = map[string]bool{
	"is":        true,
	"is_not":    true,
	"is_any_of": true,
}

// genreDelimiter matches the GROUP_CONCAT delimiter in
// track_metadata_view.sql.
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

		// Genre exact-match operators use a subquery.
		if rule.Field == "genre" && genreExactOps[rule.Operator] {
			cond, condArgs, err := buildGenreSubquery(rule)
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

// buildGenreSubquery generates a subquery condition against
// recording_genres JOIN genres for exact genre matching.
func buildGenreSubquery(rule Rule) (string, []any, error) {
	subquery := `af.id IN (
  SELECT rg_sub.recording_id FROM recording_genres rg_sub
  JOIN genres g ON rg_sub.genre_id = g.id
  WHERE `

	switch rule.Operator {
	case "is":
		return subquery + "g.name = ? COLLATE NOCASE)", []any{rule.Value}, nil

	case "is_not":
		return `af.id NOT IN (
  SELECT rg_sub.recording_id FROM recording_genres rg_sub
  JOIN genres g ON rg_sub.genre_id = g.id
  WHERE g.name = ? COLLATE NOCASE)`, []any{rule.Value}, nil

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

		return subquery + "g.name IN (" +
			strings.Join(placeholders, ", ") + "))", condArgs, nil

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

// Evaluate runs the rule set against the track_metadata view and
// returns matching tracks.
func Evaluate(
	db *database.DB, ruleSet RuleSet,
) ([]library.Track, error) {
	where, args, err := BuildWhereClause(ruleSet.Rules)
	if err != nil {
		return nil, fmt.Errorf(
			"smart playlist rule error: %w", err,
		)
	}

	// SAFETY: Dynamic WHERE clause built from whitelisted field
	// names and parameterized values only. Sort field is validated
	// against fieldMap. No user-supplied strings are interpolated.
	query := `SELECT
		file_path,
		length_milliseconds,
		title,
		artist_name,
		track_number,
		disc_number,
		album,
		genre,
		year,
		composer,
		file_type,
		sample_rate,
		bit_depth,
		channels,
		bitrate,
		file_size
	FROM track_metadata af`

	if where != "" {
		query += "\nWHERE " + where
	}

	// Sort.
	if ruleSet.SortField != "" {
		if ruleSet.SortField == "random" {
			query += "\nORDER BY RANDOM()"
		} else {
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
	}

	// Limit.
	if ruleSet.Limit > 0 {
		query += "\nLIMIT ?"

		args = append(args, ruleSet.Limit)
	}

	rows, err := db.QueryContext(query, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"smart playlist query failed: %w", err,
		)
	}

	defer func() { _ = rows.Close() }()

	return scanTracks(rows)
}

// scanTracks reads all rows from a query result into a Track slice.
func scanTracks(rows *sql.Rows) ([]library.Track, error) {
	var tracks []library.Track

	for rows.Next() {
		var (
			filePath    string
			lengthMs    int64
			title       string
			artistName  string
			trackNumber sql.NullInt64
			discNumber  sql.NullInt64
			album       string
			genre       string
			year        int64
			composer    string
			fileType    string
			sampleRate  int64
			bitDepth    int64
			channels    int64
			bitrate     int64
			fileSize    int64
		)

		if err := rows.Scan(
			&filePath, &lengthMs, &title, &artistName,
			&trackNumber, &discNumber,
			&album, &genre, &year, &composer, &fileType,
			&sampleRate, &bitDepth, &channels,
			&bitrate, &fileSize,
		); err != nil {
			return nil, fmt.Errorf(
				"could not scan smart playlist row: %w", err,
			)
		}

		tracks = append(tracks, library.Track{
			TrackName:   title,
			ArtistName:  artistName,
			TrackLength: strconv.FormatInt(lengthMs, 10),
			FilePath:    filePath,
			TrackNumber: trackNumber.Int64,
			DiscNumber:  discNumber.Int64,
			Album:       album,
			Genre:       splitGenres(genre),
			Year:        year,
			Composer:    composer,
			FileType:    fileType,
			SampleRate:  sampleRate,
			BitDepth:    bitDepth,
			Channels:    channels,
			Bitrate:     bitrate,
			FileSize:    fileSize,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"smart playlist row iteration error: %w", err,
		)
	}

	return tracks, nil
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
