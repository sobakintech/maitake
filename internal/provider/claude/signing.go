package claude

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/cespare/xxhash/v2"
	"github.com/tidwall/gjson"
)

// Claude Code signs each request body with a short hash ("cch") inside the billing
// header block, and embeds a build fingerprint derived from the first user message.
// Ported from CLIProxyAPI's reproduction of Claude Code's algorithm.

const (
	fingerprintSalt = "59cf53e54c78"
	cchSeed         = 0x4D659218E32A3268
	cchLength       = 5
)

var cchZero = []byte("00000")

func computeFingerprint(messageText, version string) string {
	units := utf16.Encode([]rune(messageText))
	var sampled [3]uint16
	for i, idx := range [...]int{4, 7, 20} {
		sampled[i] = '0'
		if idx < len(units) {
			sampled[i] = units[idx]
		}
	}
	h := sha256.Sum256([]byte(fingerprintSalt + string(utf16.Decode(sampled[:])) + version))
	return hex.EncodeToString(h[:])[:3]
}

func billingHeader(version, messageText string) string {
	var b strings.Builder
	b.WriteString("x-anthropic-billing-header: cc_version=")
	b.WriteString(version)
	b.WriteByte('.')
	b.WriteString(computeFingerprint(messageText, version))
	b.WriteString("; cc_entrypoint=cli; cch=00000;")
	return b.String()
}

// signBody fills in the cch digits of the billing header in an already-serialized body.
func signBody(body []byte) ([]byte, error) {
	offset, ok := cchOffset(body)
	if !ok {
		return body, nil
	}
	unsigned := bytes.Clone(body)
	copy(unsigned[offset:offset+cchLength], cchZero)
	normalized, err := normalizeForCCH(unsigned)
	if err != nil {
		return nil, err
	}
	h := xxhash.NewWithSeed(cchSeed)
	h.Write(normalized)
	copy(unsigned[offset:offset+cchLength], fmt.Sprintf("%05x", h.Sum64()&0xFFFFF))
	return unsigned, nil
}

func cchOffset(body []byte) (int, bool) {
	billing := gjson.GetBytes(body, "system.0.text")
	if billing.Type != gjson.String || !strings.HasPrefix(billing.String(), "x-anthropic-billing-header:") {
		return 0, false
	}
	raw := []byte(billing.Raw)
	for from := 0; from < len(raw); {
		rel := bytes.Index(raw[from:], []byte("cch="))
		if rel < 0 {
			return 0, false
		}
		digits := from + rel + len("cch=")
		end := digits + cchLength
		if end < len(raw) && raw[end] == ';' && isLowerHex(raw[digits:end]) {
			return billing.Index + digits, true
		}
		from = digits
	}
	return 0, false
}

func isLowerHex(v []byte) bool {
	if len(v) != cchLength {
		return false
	}
	for _, c := range v {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// normalizeForCCH builds the hash input without reserializing: "model" string values are
// emptied and max_tokens / fallbacks / fallback_credit_token members are dropped at any depth.
func normalizeForCCH(body []byte) ([]byte, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid JSON body")
	}
	sc := scanner{body: body}
	if err := sc.value(true); err != nil {
		return nil, err
	}
	sort.Slice(sc.edits, func(i, j int) bool { return sc.edits[i].start < sc.edits[j].start })
	out := make([]byte, 0, len(body))
	last := 0
	for _, e := range sc.edits {
		if e.start < last || e.end > len(body) {
			return nil, fmt.Errorf("overlapping edit at %d", e.start)
		}
		out = append(out, body[last:e.start]...)
		last = e.end
	}
	return append(out, body[last:]...), nil
}

type edit struct{ start, end int }

type member struct {
	start, end              int
	commaBefore, commaAfter int
	excluded                bool
}

type scanner struct {
	body  []byte
	pos   int
	edits []edit
}

func (s *scanner) ws() {
	for s.pos < len(s.body) {
		switch s.body[s.pos] {
		case ' ', '\t', '\r', '\n':
			s.pos++
		default:
			return
		}
	}
}

func (s *scanner) eat(c byte) bool {
	if s.pos < len(s.body) && s.body[s.pos] == c {
		s.pos++
		return true
	}
	return false
}

func (s *scanner) value(collect bool) error {
	s.ws()
	if s.pos >= len(s.body) {
		return fmt.Errorf("missing value at %d", s.pos)
	}
	switch s.body[s.pos] {
	case '{':
		return s.object(collect)
	case '[':
		return s.array(collect)
	case '"':
		_, _, err := s.str()
		return err
	default:
		start := s.pos
		for s.pos < len(s.body) {
			switch s.body[s.pos] {
			case ',', '}', ']', ' ', '\t', '\r', '\n':
				if s.pos == start {
					return fmt.Errorf("missing value at %d", start)
				}
				return nil
			}
			s.pos++
		}
		return nil
	}
}

func excludedKey(key []byte) bool {
	switch string(key) {
	case `"max_tokens"`, `"fallbacks"`, `"fallback_credit_token"`:
		return true
	}
	return false
}

func (s *scanner) object(collect bool) error {
	s.pos++
	s.ws()
	if s.eat('}') {
		return nil
	}
	var members []member
	commaBefore := -1
	for {
		s.ws()
		mStart := s.pos
		ks, ke, err := s.str()
		if err != nil {
			return err
		}
		s.ws()
		if !s.eat(':') {
			return fmt.Errorf("missing colon at %d", s.pos)
		}
		s.ws()
		key := s.body[ks:ke]
		excluded := collect && excludedKey(key)
		if collect && bytes.Equal(key, []byte(`"model"`)) && s.pos < len(s.body) && s.body[s.pos] == '"' {
			vs, ve, err := s.str()
			if err != nil {
				return err
			}
			s.add(vs+1, ve-1)
		} else if err := s.value(collect && !excluded); err != nil {
			return err
		}
		mEnd := s.pos
		s.ws()
		commaAfter := -1
		if s.eat(',') {
			commaAfter = s.pos - 1
		}
		members = append(members, member{mStart, mEnd, commaBefore, commaAfter, excluded})
		if commaAfter >= 0 {
			commaBefore = commaAfter
			continue
		}
		if !s.eat('}') {
			return fmt.Errorf("missing object end at %d", s.pos)
		}
		break
	}
	if collect {
		s.excludeMembers(members)
	}
	return nil
}

func (s *scanner) array(collect bool) error {
	s.pos++
	s.ws()
	if s.eat(']') {
		return nil
	}
	for {
		if err := s.value(collect); err != nil {
			return err
		}
		s.ws()
		if s.eat(',') {
			continue
		}
		if !s.eat(']') {
			return fmt.Errorf("missing array end at %d", s.pos)
		}
		return nil
	}
}

func (s *scanner) str() (int, int, error) {
	if s.pos >= len(s.body) || s.body[s.pos] != '"' {
		return 0, 0, fmt.Errorf("missing string at %d", s.pos)
	}
	start := s.pos
	s.pos++
	for s.pos < len(s.body) {
		switch s.body[s.pos] {
		case '\\':
			s.pos += 2
		case '"':
			s.pos++
			return start, s.pos, nil
		default:
			s.pos++
		}
	}
	return 0, 0, fmt.Errorf("unterminated string at %d", start)
}

// excludeMembers mirrors Claude Code's quirks for removing members, including leaving the
// preceding comma when an object ends with several consecutive excluded members.
func (s *scanner) excludeMembers(ms []member) {
	for start := 0; start < len(ms); {
		if !ms[start].excluded {
			start++
			continue
		}
		end := start
		for end+1 < len(ms) && ms[end+1].excluded {
			end++
		}
		switch {
		case end+1 < len(ms):
			s.add(ms[start].start, ms[end].commaAfter+1)
		case start > 0 && end > start:
			s.add(ms[start].start, ms[end].end)
		case start > 0:
			s.add(ms[start].commaBefore, ms[end].end)
		default:
			s.add(ms[start].start, ms[end].end)
		}
		start = end + 1
	}
}

func (s *scanner) add(start, end int) {
	if start < end {
		s.edits = append(s.edits, edit{start, end})
	}
}

// Resign recomputes the billing-header hash after the body was edited.
func Resign(body []byte) ([]byte, error) {
	off, ok := cchOffset(body)
	if !ok {
		return body, nil
	}
	cleared := bytes.Clone(body)
	copy(cleared[off:off+cchLength], cchZero)
	return signBody(cleared)
}
