package spanenc

import (
	"fmt"
	"slices"

	"cloud.google.com/go/spanner"
	fields "github.com/apstndb/structfields"
)

// EncodeOption configures value encoding in [ValueOf],
// [StructColumnsAndValues], [ValuesFromSlice], and [ArrayValueFromSlice].
type EncodeOption func(*encodeConfig)

type encodeConfig struct {
	lossOfPrecisionHandling spanner.LossOfPrecisionHandlingOption
}

// numericRounding reports whether NUMERIC validation is skipped. Like the
// client's encodeValue switch, anything other than NumericError skips
// validation.
func (cfg encodeConfig) numericRounding() bool {
	return cfg.lossOfPrecisionHandling != spanner.NumericError
}

func newEncodeConfig(opts []EncodeOption) encodeConfig {
	// Default to NumericError-style validation. This deliberately ignores
	// the client's package-global spanner.LossOfPrecisionHandling (whose
	// default is NumericRound): per-call explicitness over mutable global
	// state, and no silent precision loss unless asked for.
	cfg := encodeConfig{lossOfPrecisionHandling: spanner.NumericError}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithLossOfPrecisionHandling selects the NUMERIC loss-of-precision behavior
// for this call, using the client's [spanner.LossOfPrecisionHandlingOption]
// vocabulary: [spanner.NumericError] validates precision and scale,
// returning [ErrNumericOutOfRange] for values Spanner cannot represent
// exactly; [spanner.NumericRound] silently rounds out-of-scale values to
// Spanner's 9 fractional digits via the canonical wire formatting. Like the
// client, rounding affects only the fractional part; a whole component
// exceeding the NUMERIC precision is passed through and rejected by Spanner.
//
// Unlike the client, the behavior is configured per call instead of through
// the package-global [spanner.LossOfPrecisionHandling], which this package
// never reads; without this option the default is [spanner.NumericError]
// (the client's global defaults to NumericRound — pass it explicitly for
// that behavior).
func WithLossOfPrecisionHandling(handling spanner.LossOfPrecisionHandlingOption) EncodeOption {
	return func(cfg *encodeConfig) { cfg.lossOfPrecisionHandling = handling }
}

// ColumnMaskOption configures the column mask of [MutationColumnsAndValues],
// [MutationMap], and [ParamsMap].
type ColumnMaskOption func(*columnMaskConfig)

type columnMaskConfig struct {
	include []string
	exclude []string
}

func newColumnMaskConfig(opts []ColumnMaskOption) columnMaskConfig {
	var cfg columnMaskConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithColumns restricts the output columns to the listed ones (an include
// mask). Output keeps the struct declaration order regardless of the
// argument order. Naming an unknown column returns [ErrInvalidColumnMask] —
// as does naming a read-only column in the write-shaped helpers
// ([MutationColumnsAndValues], [MutationMap]) or combining with
// [WithoutColumns]. Multiple WithColumns options accumulate.
func WithColumns(columns ...string) ColumnMaskOption {
	return func(cfg *columnMaskConfig) { cfg.include = append(cfg.include, columns...) }
}

// WithoutColumns drops the listed columns from the output (an exclude mask).
// Naming an unknown column returns [ErrInvalidColumnMask] (naming a
// read-only column is allowed: in the write-shaped helpers it is already
// excluded from writes); combining with [WithColumns] does too. Multiple
// WithoutColumns options accumulate.
func WithoutColumns(columns ...string) ColumnMaskOption {
	return func(cfg *columnMaskConfig) { cfg.exclude = append(cfg.exclude, columns...) }
}

// keep reports whether the field name passes the configured mask.
// validate must have been called first.
func (cfg *columnMaskConfig) keep(name string) bool {
	if cfg.include != nil {
		return slices.Contains(cfg.include, name)
	}
	return !slices.Contains(cfg.exclude, name)
}

// validate checks the mask against the listed fields: include and exclude
// are mutually exclusive and every masked name must exist. With
// requireWritable (the write-shaped helpers), include may not name
// read-only fields.
func (cfg *columnMaskConfig) validate(fl fields.List, requireWritable bool) error {
	if cfg.include != nil && cfg.exclude != nil {
		return fmt.Errorf("%w: WithColumns and WithoutColumns are mutually exclusive", ErrInvalidColumnMask)
	}
	byName := make(map[string]fields.Field, len(fl))
	for _, f := range fl {
		byName[f.Name] = f
	}
	for _, name := range cfg.include {
		f, ok := byName[name]
		if !ok {
			return fmt.Errorf("%w: unknown column %q", ErrInvalidColumnMask, name)
		}
		if requireWritable && isReadOnlyField(f) {
			return fmt.Errorf("%w: column %q is read-only", ErrInvalidColumnMask, name)
		}
	}
	for _, name := range cfg.exclude {
		if _, ok := byName[name]; !ok {
			return fmt.Errorf("%w: unknown column %q", ErrInvalidColumnMask, name)
		}
	}
	return nil
}
