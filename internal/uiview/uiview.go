// Package uiview builds screen descriptions and app manifests for platform
// runtimes (RFC §22) from the interface program model.UI. A description
// is one screen of one app in one language: the labels are already in that
// language, the places carry the app name, the types are only those on the
// screen. A description does not depend on data and is cached by the
// content hash.
package uiview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Version is the version of the description format: the runtime
// conformance set checks it.
const Version = 1

// Screen is a screen description.
type Screen struct {
	Version  int                `json:"version"`
	App      string             `json:"app"`
	Screen   string             `json:"screen"`
	Lang     string             `json:"lang"`
	Platform string             `json:"platform"`
	Title    string             `json:"title"`
	Params   []model.UIParam    `json:"params,omitempty"`
	Nodes    []Node             `json:"nodes"`
	Types    map[string]Type    `json:"types,omitempty"`
	Messages map[string]Message `json:"messages,omitempty"`
}

// Node is a description node: a program node with a label and a place.
type Node struct {
	model.UINode
	Label    string `json:"label,omitempty"`
	Place    string `json:"place,omitempty"` // Admin.AdminOrder:Order.Edit
	Children []Node `json:"children,omitempty"`
}

// MarshalJSON writes a node flat: the program node fields, the label, the
// place and the description's children instead of the program's children.
func (n Node) MarshalJSON() ([]byte, error) {
	u := n.UINode
	u.Children, u.Label = nil, ""
	base, err := json.Marshal(u)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	put := func(k string, v any) error {
		data, err := json.Marshal(v)
		m[k] = data
		return err
	}
	if n.Label != "" {
		if err := put("label", n.Label); err != nil {
			return nil, err
		}
	}
	if n.Place != "" {
		if err := put("place", n.Place); err != nil {
			return nil, err
		}
	}
	if len(n.Children) > 0 {
		if err := put("children", n.Children); err != nil {
			return nil, err
		}
	}
	return json.Marshal(m)
}

// Type is an enumeration with value labels or a value object with field
// labels.
type Type struct {
	Values []string          `json:"values,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	Fields []Field           `json:"fields,omitempty"`
}

// Field is a value object field.
type Field struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

// Message is the text of an error or notification with {parameter} substitutions.
type Message struct {
	Text string `json:"text"`
}

// Manifest is the app manifest: the menu, screens with description hashes, places.
type Manifest struct {
	Version   int                `json:"version"`
	App       string             `json:"app"`
	Lang      string             `json:"lang"`
	Title     string             `json:"title"`
	Platforms []string           `json:"platforms"`
	Me        string             `json:"me,omitempty"`
	Nodes     []Node             `json:"nodes,omitempty"`
	Screens   map[string]string  `json:"screens"` // screen → description hash
	Places    []model.UIPlace    `json:"places,omitempty"`
	Types     map[string]Type    `json:"types,omitempty"`
	Messages  map[string]Message `json:"messages,omitempty"`
	Test      bool               `json:"test,omitempty"` // the engine's test mode: "sign in as"
	// Features are the feature flags switched on for the caller (RFC §23.4).
	Features []string `json:"features,omitempty"`
	// States are the visibility of the app layout nodes for the caller:
	// access and if conditions (like nodes in screen data).
	States map[string]any `json:"states,omitempty"`
}

// Builder builds the descriptions of one app.
type Builder struct {
	UI  *model.UI
	App *model.UIApp
}

// New returns the description builder of the app or nil if there is none.
func New(ui *model.UI, app string) *Builder {
	a := ui.App(app)
	if a == nil {
		return nil
	}
	return &Builder{UI: ui, App: a}
}

// Screen builds a screen description in the language lang; nil — the app
// has no such screen.
func (b *Builder) Screen(name, lang, platform string) *Screen {
	s := b.UI.Screen(name) // the full or short screen name
	if s == nil || !contains(b.App.Screens, s.Name) {
		return nil
	}
	t := b.texts(lang)
	out := &Screen{Version: Version, App: b.App.Name, Screen: s.Name, Lang: lang, Platform: platform,
		Title: t.title(s.Name, s.Title), Params: s.Params}
	used := map[string]bool{}
	out.Nodes = b.nodes(t, s, s.Nodes, used)
	for _, p := range s.Params {
		markTypes(p.Type, used)
	}
	out.Types = b.types(t, used)
	out.Messages = b.messages(t, s.Nodes)
	return out
}

// Manifest builds the app manifest in the language lang with the screen
// description hashes.
func (b *Builder) Manifest(lang, platform string) *Manifest {
	t := b.texts(lang)
	out := &Manifest{Version: Version, App: b.App.Name, Lang: lang, Title: t.title(b.App.Name, b.App.Title),
		Platforms: b.App.Platforms, Me: b.App.Me, Places: b.App.Places, Screens: map[string]string{}}
	used := map[string]bool{}
	out.Nodes = b.nodes(t, nil, b.App.Nodes, used)
	out.Types = b.types(t, used)
	out.Messages = b.messages(t, b.App.Nodes)
	for _, name := range b.App.Screens {
		out.Screens[name] = Hash(b.Screen(name, lang, platform))
	}
	return out
}

// Hash is the content hash of a description: its address in the cache.
func Hash(v any) string {
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

func (b *Builder) nodes(t *texts, s *model.UIScreen, nodes []model.UINode, used map[string]bool) []Node {
	var out []Node
	for _, n := range nodes {
		out = append(out, b.node(t, s, n, used))
	}
	return out
}

func (b *Builder) node(t *texts, s *model.UIScreen, n model.UINode, used map[string]bool) Node {
	owner := b.App.Name
	if s != nil {
		owner = s.Name
	}
	out := Node{UINode: n}
	if n.Bind != nil {
		out.Place = b.App.Name + ":" + n.Key
		if s != nil {
			out.Place = b.App.Name + "." + s.Name + ":" + n.Key
			// The compiler gives the place name: the screen in it is the short name (RFC §22.10).
			for _, pl := range b.App.Places {
				if pl.Screen == s.Name && pl.Node == n.ID {
					out.Place = pl.Name
				}
			}
		}
		markTypes(n.Bind.Result, used)
		for _, a := range n.Bind.Args {
			markTypes(a.Type, used)
		}
	}
	markTypes(n.FieldType, used)
	out.Label = b.label(t, owner, n)
	out.Children = b.nodes(t, s, n.Children, used)
	return out
}

// label is a node label (RFC §22.11): the texts of the user's language,
// then the layout string and the texts of the base language, then the name
// split into words.
func (b *Builder) label(t *texts, owner string, n model.UINode) string {
	var keys []string
	if n.Key != "" && !n.Auto {
		keys = append(keys, owner+":"+n.Key)
	}
	fallback := ""
	switch {
	case n.Bind != nil:
		keys = append(keys, n.Bind.Type+"."+n.Bind.Method)
		fallback = n.Bind.Method
	case n.Kind == model.UIFieldNode:
		if n.Text != "" {
			keys = append(keys, n.Text)
		}
		fallback = n.Field
	case n.To != nil && n.Kind != model.UIOnNode:
		keys = append(keys, n.To.Screen)
		if s := b.UI.Screen(n.To.Screen); s != nil && s.Title != "" {
			return t.get(keys, s.Title)
		}
		fallback = n.To.Screen
	default:
		return t.get(keys, "")
	}
	return t.get(keys, Humanize(fallback))
}

// types are the enumerations and value objects on the screen, with labels
// in the language.
func (b *Builder) types(t *texts, used map[string]bool) map[string]Type {
	// Value object fields pull in their types.
	for changed := true; changed; {
		changed = false
		for name := range used {
			if ut := b.UI.Type(name); ut != nil {
				for _, f := range ut.Fields {
					for _, n := range typeNames(f.Type) {
						if !used[n] {
							used[n], changed = true, true
						}
					}
				}
			}
		}
	}
	out := map[string]Type{}
	for name := range used {
		ut := b.UI.Type(name)
		if ut == nil {
			continue
		}
		typ := Type{Values: ut.Values}
		if len(ut.Values) > 0 {
			typ.Labels = map[string]string{}
			for _, v := range ut.Values {
				typ.Labels[v] = t.get([]string{name + "." + v}, Humanize(v))
			}
		}
		for _, f := range ut.Fields {
			typ.Fields = append(typ.Fields, Field{Name: f.Name, Type: f.Type, Label: t.get([]string{name + "." + f.Name}, Humanize(f.Name))})
		}
		out[name] = typ
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// messages are the texts of the errors and notifications of the types whose
// commands and queries the screen is bound to: the runtime shows them.
func (b *Builder) messages(t *texts, nodes []model.UINode) map[string]Message {
	owners := map[string]bool{}
	var walk func([]model.UINode)
	walk = func(list []model.UINode) {
		for _, n := range list {
			if n.Bind != nil {
				owners[n.Bind.Type] = true
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	out := map[string]Message{}
	for _, lang := range []string{t.lang, t.base} {
		for key, text := range b.UI.Texts[lang] {
			i := strings.LastIndex(key, ".")
			if i < 0 || !owners[key[:i]] || strings.Contains(key, ":") {
				continue
			}
			if _, done := out[key]; !done && isMessage(key) {
				out[key] = Message{Text: text}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isMessage: a key Type.name with a lowercase name is an error or a
// notification (lowercase fields and enumeration values belong to values
// and enumerations, not to entities).
func isMessage(key string) bool {
	name := key[strings.LastIndex(key, ".")+1:]
	return name != "" && unicode.IsLower([]rune(name)[0])
}

type texts struct {
	ui         *model.UI
	lang, base string
}

func (b *Builder) texts(lang string) *texts {
	return &texts{ui: b.UI, lang: lang, base: b.App.Language}
}

// get looks up a text by keys in the user's language, then in the app's
// base language — the layout strings among them; otherwise the fallback text.
func (t *texts) get(keys []string, fallback string) string {
	for _, lang := range []string{t.lang, t.base} {
		for _, k := range keys {
			if s, ok := t.ui.Texts[lang][k]; ok {
				return s
			}
		}
	}
	return fallback
}

func (t *texts) title(key, layout string) string {
	if layout != "" {
		return t.get([]string{key}, layout)
	}
	return t.get([]string{key}, Humanize(key))
}

// Humanize splits a name into words: placedAt → "Placed at".
func Humanize(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case i == 0:
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsUpper(r):
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// markTypes marks the type names of a type written as Order.Id, list<OrderRow>, Money?.
func markTypes(t string, used map[string]bool) {
	for _, n := range typeNames(t) {
		used[n] = true
	}
}

func typeNames(t string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(t, func(r rune) bool { return r == '<' || r == '>' || r == ',' || r == ' ' || r == '?' }) {
		switch part {
		case "list", "set", "page", "map":
			continue
		}
		out = append(out, strings.TrimSuffix(part, ".Id"))
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
