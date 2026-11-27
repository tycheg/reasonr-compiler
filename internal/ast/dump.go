package ast

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

var (
	posType      = reflect.TypeOf(source.Pos(0))
	kindType     = reflect.TypeOf(token.Kind(0))
	commentsType = reflect.TypeOf([]token.Comment(nil))
)

// flagPos are positions of optional words printed as flags in a dump.
var flagPos = map[string]bool{"DescPos": true, "UniquePos": true, "AutoPos": true, "BackPos": true, "ScreenPos": true}

// Dump prints the tree in readable form without positions. Used in parser
// golden tests and for debugging.
func Dump(n Node) string {
	var b strings.Builder
	dumpValue(&b, reflect.ValueOf(n), 0)
	b.WriteByte('\n')
	return b.String()
}

func dumpValue(b *strings.Builder, v reflect.Value, indent int) {
	if s, ok := inline(v); ok {
		b.WriteString(s)
		return
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		dumpValue(b, v.Elem(), indent)
	case reflect.Struct:
		b.WriteString(v.Type().Name())
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			ft := v.Type().Field(i)
			if flagPos[ft.Name] && f.Int() != 0 {
				// The position of an optional word is in effect a flag: desc, view, unique.
				newline(b, indent+1)
				b.WriteString(strings.TrimSuffix(ft.Name, "Pos") + ": true")
				continue
			}
			if ft.Type == posType || ft.Type == commentsType || f.IsZero() {
				continue
			}
			newline(b, indent+1)
			b.WriteString(ft.Name)
			b.WriteString(":")
			if f.Kind() != reflect.Slice {
				b.WriteString(" ")
			}
			dumpValue(b, f, indent+1)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			newline(b, indent+1)
			b.WriteString("- ")
			dumpValue(b, v.Index(i), indent+2)
		}
	default:
		b.WriteString("?" + v.Kind().String())
	}
}

func newline(b *strings.Builder, indent int) {
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("  ", indent))
}

// inline returns a short form for leaf nodes and scalars.
func inline(v reflect.Value) (string, bool) {
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return "nil", true
	}
	if v.Type() == kindType {
		return token.Kind(v.Uint()).String(), true
	}
	switch x := v.Interface().(type) {
	case *Ident:
		return x.Name, true
	case *NamedType:
		return "NamedType " + x.String(), true
	case *Path:
		return "Path " + x.String(), true
	case *BasicLit:
		s := "BasicLit " + x.Kind.String() + " " + x.Raw
		if x.Value != x.Raw && strconv.Quote(x.Value) != x.Raw {
			s += " = " + strconv.Quote(x.Value)
		}
		return s, true
	case *CommentGroup:
		return strconv.Quote(x.Text()), true
	case string:
		return strconv.Quote(x), true
	case bool:
		return strconv.FormatBool(x), true
	}
	return "", false
}
