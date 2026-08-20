package opencode

// opencode's config is JSONC, not strict JSON: "OpenCode supports both JSON and JSONC (JSON with
// Comments) formats." Real opencode.json files carry `//` and `/* */` comments and trailing commas —
// all of which Go's encoding/json rejects (a trailing comma before `}` yields the classic
// "invalid character '}' looking for beginning of object key string"). stripJSONC normalizes a JSONC
// byte slice into strict JSON so the stdlib parser accepts it.
//
// It is string-aware: comment markers and commas that appear INSIDE a JSON string literal (including
// escaped quotes) are left untouched. It only ever REMOVES bytes (comments and redundant trailing
// commas), so byte offsets shift but no value is ever rewritten.

// stripJSONC removes JSONC line/block comments and trailing commas from data, returning strict JSON.
func stripJSONC(data []byte) []byte {
	return stripTrailingCommas(stripComments(data))
}

// stripComments drops `//`…EOL and `/* … */` comments that are not inside a string literal. Newlines
// inside a line comment are preserved so line structure (and downstream trailing-comma detection across
// lines) is unaffected.
func stripComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(data) {
			switch data[i+1] {
			case '/': // line comment — skip to (but keep) the newline
				i += 2
				for i < len(data) && data[i] != '\n' {
					i++
				}
				i-- // let the loop's i++ land on the newline (or end)
				continue
			case '*': // block comment — skip through the closing */
				i += 2
				for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
					i++
				}
				i++ // step onto '/', loop's i++ steps past it (unterminated → past end, harmless)
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// stripTrailingCommas removes a comma that is followed (ignoring whitespace) by `}` or `]`, when not
// inside a string literal. Assumes comments have already been stripped.
func stripTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(data) && isJSONSpace(data[j]) {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue // drop the trailing comma; whitespace is written by later iterations
			}
		}
		out = append(out, c)
	}
	return out
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
