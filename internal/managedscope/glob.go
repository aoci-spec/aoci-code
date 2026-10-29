package managedscope

import (
	"fmt"
	"unicode/utf8"
)

// A glob program is the token form of one pattern under the dialect
// buildGlobRegexp compiles, matched as an NFA over the runes of a path:
//
//	literal   one exact rune
//	star      any run of runes other than '/'          ([^/]*)
//	qmark     exactly one rune other than '/'          ([^/])
//	any       any run of runes other than newline      (.*,  from **)
//	anyDirs   optionally, any run ending in '/'        ((?:.*/)?, from **/)
//
// The newline exclusions are not a design wish; they are what the regexp
// engine does with '.' and what the oracle therefore defines. A path never
// carries a newline past NormalizeRelPath, but the two must agree on every
// input the fuzz target can invent.
type globToken struct {
	kind globTokenKind
	rune rune
}

type globTokenKind uint8

const (
	globLiteral globTokenKind = iota
	globStar
	globQMark
	globAny
	globAnyDirs
)

type globProgram struct {
	tokens []globToken
	// prefix and suffix are the literal runs before the first and after the
	// last wildcard: a path that lacks either cannot match, which rejects
	// nearly every (rule, path) pair of a starter policy before the NFA runs.
	// They are compared rune by rune, never byte by byte: the regexp decodes
	// an invalid byte as U+FFFD, and so must this (the fuzzer's first find).
	prefix, suffix []rune
}

func hasRunePrefix(rel string, prefix []rune) bool {
	for _, want := range prefix {
		if rel == "" {
			return false
		}
		r, size := utf8.DecodeRuneInString(rel)
		if r != want {
			return false
		}
		rel = rel[size:]
	}
	return true
}

func hasRuneSuffix(rel string, suffix []rune) bool {
	for index := len(suffix) - 1; index >= 0; index-- {
		if rel == "" {
			return false
		}
		r, size := utf8.DecodeLastRuneInString(rel)
		if r != suffix[index] {
			return false
		}
		rel = rel[:len(rel)-size]
	}
	return true
}

func compileGlobProgram(pattern string) (*globProgram, error) {
	program := &globProgram{}
	runes := []rune(pattern)
	for index := 0; index < len(runes); index++ {
		r := runes[index]
		switch r {
		case '*':
			if index+1 < len(runes) && runes[index+1] == '*' {
				index++
				if index+1 < len(runes) && runes[index+1] == '/' {
					index++
					program.tokens = append(program.tokens, globToken{kind: globAnyDirs})
				} else {
					program.tokens = append(program.tokens, globToken{kind: globAny})
				}
			} else {
				program.tokens = append(program.tokens, globToken{kind: globStar})
			}
		case '?':
			program.tokens = append(program.tokens, globToken{kind: globQMark})
		case '[':
			return nil, fmt.Errorf("character_classes_not_supported: a glob cannot hold \"[\"; name a literal path such as pages/[id].vue or app/[locale] with pattern kind file or directory")
		default:
			program.tokens = append(program.tokens, globToken{kind: globLiteral, rune: r})
		}
	}
	first, last := len(program.tokens), -1
	for index, token := range program.tokens {
		if token.kind != globLiteral {
			if index < first {
				first = index
			}
			last = index
		}
	}
	for _, token := range program.tokens[:first] {
		program.prefix = append(program.prefix, token.rune)
	}
	if last >= 0 {
		for _, token := range program.tokens[last+1:] {
			program.suffix = append(program.suffix, token.rune)
		}
	}
	return program, nil
}

// match runs the NFA. States are token indices; state len(tokens) is accept.
// A star-like token is a state that may consume a rune and stay, and that is
// also skipped by an epsilon move when it is entered, which is what makes
// "**/" optional and "*" able to match nothing.
func (p *globProgram) match(rel string) bool {
	if !hasRunePrefix(rel, p.prefix) || !hasRuneSuffix(rel, p.suffix) {
		return false
	}
	// Without any wildcard the program is its prefix and the path must be it.
	if len(p.tokens) == len(p.prefix) {
		return utf8.RuneCountInString(rel) == len(p.prefix)
	}
	n := len(p.tokens)
	var currentBuffer, nextBuffer [64]bool
	current, next := currentBuffer[:], nextBuffer[:]
	if n+1 > len(current) {
		current, next = make([]bool, n+1), make([]bool, n+1)
	}
	current = current[:n+1]
	next = next[:n+1]
	p.closure(current, 0)
	for _, r := range rel {
		for i := range next {
			next[i] = false
		}
		alive := false
		for state := 0; state < n; state++ {
			if !current[state] {
				continue
			}
			token := p.tokens[state]
			switch token.kind {
			case globLiteral:
				if r == token.rune {
					p.closure(next, state+1)
					alive = true
				}
			case globQMark:
				if r != '/' {
					p.closure(next, state+1)
					alive = true
				}
			case globStar:
				// A run may end after any rune, so staying also re-opens the
				// epsilon move to the next token.
				if r != '/' {
					p.closure(next, state)
					alive = true
				}
			case globAny:
				if r != '\n' {
					p.closure(next, state)
					alive = true
				}
			case globAnyDirs:
				// Once inside the optional group only a '/' can close it:
				// stay without the epsilon, and open the next token on '/'.
				if r != '\n' {
					next[state] = true
					alive = true
					if r == '/' {
						p.closure(next, state+1)
					}
				}
			}
		}
		if !alive {
			return false
		}
		current, next = next, current
	}
	return current[n]
}

// closure marks state and every state reachable from it by consuming
// nothing: a star-like token may be skipped, so entering it also enters the
// state after it.
func (p *globProgram) closure(states []bool, state int) {
	for state < len(p.tokens) {
		if states[state] {
			return
		}
		states[state] = true
		switch p.tokens[state].kind {
		case globStar, globAny, globAnyDirs:
			state++
		default:
			return
		}
	}
	states[state] = true
}
