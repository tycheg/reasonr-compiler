package reasonr

import "github.com/tycheg/reasonr-compiler/internal/codec"

// Serialization of values in formats the compiler does not know (plan
// 0021, decision 6): the model walks a value by its schema and calls the
// methods of a ValueWriter; it decodes by pulling tokens from a
// ValueReader. A format is a codec adapter of the engine (port.Codec):
// port.Writer and port.Reader have the same methods, so an adapter imports
// only the engine's port. The canonical JSON (JSONCodec) is the engine's
// built-in format and is called directly.

// ValueWriter receives a value in walk order.
type ValueWriter = codec.Writer

// ValueReader gives the tokens of a value.
type ValueReader = codec.Reader

// CodecOptions change how values are walked: sealed personal fields as
// null, unknown fields skipped.
type CodecOptions = codec.Options

// EncodeValue writes value v of type t to w.
func EncodeValue(s *Schema, t Type, v Value, a *Arena, w ValueWriter, o CodecOptions) error {
	return codec.Encode(s, t, v, a, w, o)
}

// EncodeFields writes a record with fields fields (method arguments or a
// record's fields) to w as an object.
func EncodeFields(s *Schema, fields []FieldInfo, r Record, a *Arena, w ValueWriter, o CodecOptions) error {
	return codec.EncodeFields(s, fields, r, a, w, o)
}

// EncodeArgs writes the given arguments params as an object: an argument
// that is none is left out.
func EncodeArgs(s *Schema, params []FieldInfo, r Record, a *Arena, w ValueWriter) error {
	return codec.EncodeArgs(s, params, r, a, w)
}

// DecodeValue reads a value of type t from r into the arena a.
func DecodeValue(s *Schema, t Type, r ValueReader, a *Arena, o CodecOptions) (Value, error) {
	return codec.Decode(s, t, r, a, o)
}

// DecodeArgs reads the arguments params from r; a missing one is none.
func DecodeArgs(s *Schema, params []FieldInfo, r ValueReader, a *Arena) (Record, error) {
	return codec.DecodeArgs(s, params, r, a)
}
