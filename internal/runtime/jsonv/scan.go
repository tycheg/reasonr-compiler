package jsonv

import (
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// scanner parses JSON (RFC 8259) in place: the decoder asks for what it
// expects by type, the scanner checks the syntax.
type scanner struct {
	data []byte
	pos  int
	buf  []byte // a string after unescaping
}

func (s *scanner) ws() {
	for s.pos < len(s.data) {
		switch s.data[s.pos] {
		case ' ', '\t', '\n', '\r':
			s.pos++
		default:
			return
		}
	}
}

// peek returns the next significant byte, or 0 at the end.
func (s *scanner) peek() byte {
	s.ws()
	if s.pos >= len(s.data) {
		return 0
	}
	return s.data[s.pos]
}

func (s *scanner) expect(c byte) error {
	if s.peek() != c {
		return s.unexpected("expected %q", c)
	}
	s.pos++
	return nil
}

func (s *scanner) unexpected(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if s.pos >= len(s.data) {
		return errorf("unexpected end of JSON: %s", msg)
	}
	return errorf("unexpected character %q at position %d: %s", s.data[s.pos], s.pos, msg)
}

// literal reads true, false or null.
func (s *scanner) literal(word string) bool {
	if s.peek() != word[0] || len(s.data)-s.pos < len(word) || string(s.data[s.pos:s.pos+len(word)]) != word {
		return false
	}
	s.pos += len(word)
	return true
}

// null reads null if it is next.
func (s *scanner) null() bool { return s.literal("null") }

// string reads a string; the result is valid until the next call.
func (s *scanner) string() ([]byte, error) {
	if err := s.expect('"'); err != nil {
		return nil, err
	}
	s.buf = s.buf[:0]
	start := s.pos
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		switch {
		case c == '"':
			s.buf = append(s.buf, s.data[start:s.pos]...)
			s.pos++
			if !utf8.Valid(s.buf) {
				return nil, ruleErr("encoding", "the string is not UTF-8")
			}
			return s.buf, nil
		case c < 0x20:
			return nil, s.unexpected("a control character in a string")
		case c == '\\':
			s.buf = append(s.buf, s.data[start:s.pos]...)
			if err := s.escape(); err != nil {
				return nil, err
			}
			start = s.pos
		default:
			s.pos++
		}
	}
	return nil, errorf("unterminated string")
}

// escape parses an escape after a backslash.
func (s *scanner) escape() error {
	s.pos++
	if s.pos >= len(s.data) {
		return errorf("unterminated string")
	}
	c := s.data[s.pos]
	s.pos++
	switch c {
	case '"', '\\', '/':
		s.buf = append(s.buf, c)
	case 'b':
		s.buf = append(s.buf, '\b')
	case 'f':
		s.buf = append(s.buf, '\f')
	case 'n':
		s.buf = append(s.buf, '\n')
	case 'r':
		s.buf = append(s.buf, '\r')
	case 't':
		s.buf = append(s.buf, '\t')
	case 'u':
		r, ok := s.hex4()
		if !ok {
			return errorf("invalid \\u escape")
		}
		if utf16.IsSurrogate(r) {
			lo := rune(-1)
			if s.pos+1 < len(s.data) && s.data[s.pos] == '\\' && s.data[s.pos+1] == 'u' {
				s.pos += 2
				if lo, ok = s.hex4(); !ok {
					return errorf("invalid \\u escape")
				}
			}
			if r = utf16.DecodeRune(r, lo); r == utf8.RuneError {
				return errorf("incomplete surrogate pair")
			}
		}
		s.buf = utf8.AppendRune(s.buf, r)
	default:
		return errorf("unknown escape \\%c", c)
	}
	return nil
}

func (s *scanner) hex4() (rune, bool) {
	if len(s.data)-s.pos < 4 {
		return 0, false
	}
	var r rune
	for _, c := range s.data[s.pos : s.pos+4] {
		switch {
		case '0' <= c && c <= '9':
			r = r<<4 | rune(c-'0')
		case 'a' <= c && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case 'A' <= c && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	s.pos += 4
	return r, true
}

// number reads a number and returns its text.
func (s *scanner) number() ([]byte, error) {
	s.ws()
	start := s.pos
	if s.pos < len(s.data) && s.data[s.pos] == '-' {
		s.pos++
	}
	digits := func() int {
		n := 0
		for s.pos < len(s.data) && '0' <= s.data[s.pos] && s.data[s.pos] <= '9' {
			s.pos++
			n++
		}
		return n
	}
	intStart := s.pos
	if n := digits(); n == 0 || n > 1 && s.data[intStart] == '0' {
		s.pos = start
		return nil, s.unexpected("expected a number")
	}
	if s.pos < len(s.data) && s.data[s.pos] == '.' {
		s.pos++
		if digits() == 0 {
			return nil, s.unexpected("no digits after the point")
		}
	}
	if s.pos < len(s.data) && (s.data[s.pos] == 'e' || s.data[s.pos] == 'E') {
		s.pos++
		if s.pos < len(s.data) && (s.data[s.pos] == '+' || s.data[s.pos] == '-') {
			s.pos++
		}
		if digits() == 0 {
			return nil, s.unexpected("no exponent digits")
		}
	}
	return s.data[start:s.pos], nil
}

// array reads an array, calling each for every element.
func (s *scanner) array(each func(i int) error) error {
	if err := s.expect('['); err != nil {
		return err
	}
	if s.peek() == ']' {
		s.pos++
		return nil
	}
	for i := 0; ; i++ {
		if err := each(i); err != nil {
			return err
		}
		switch s.peek() {
		case ',':
			s.pos++
		case ']':
			s.pos++
			return nil
		default:
			return s.unexpected("expected , or ]")
		}
	}
}

// object reads an object, calling each for every key; each reads the value.
func (s *scanner) object(each func(key string) error) error {
	if err := s.expect('{'); err != nil {
		return err
	}
	if s.peek() == '}' {
		s.pos++
		return nil
	}
	for {
		k, err := s.string()
		if err != nil {
			return err
		}
		key := string(k)
		if err := s.expect(':'); err != nil {
			return err
		}
		if err := each(key); err != nil {
			return err
		}
		switch s.peek() {
		case ',':
			s.pos++
		case '}':
			s.pos++
			return nil
		default:
			return s.unexpected("expected , or }")
		}
	}
}

// skip skips any value.
func (s *scanner) skip(depth int) error {
	if depth > maxDepth {
		return errorf("nesting deeper than %d", maxDepth)
	}
	switch s.peek() {
	case '"':
		_, err := s.string()
		return err
	case '{':
		return s.object(func(string) error { return s.skip(depth + 1) })
	case '[':
		return s.array(func(int) error { return s.skip(depth + 1) })
	case 't':
		if s.literal("true") {
			return nil
		}
	case 'f':
		if s.literal("false") {
			return nil
		}
	case 'n':
		if s.null() {
			return nil
		}
	default:
		_, err := s.number()
		return err
	}
	return s.unexpected("expected a value")
}
