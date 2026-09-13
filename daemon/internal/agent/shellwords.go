package agent

import (
	"strings"
)

// JoinShellWords renders an argv vector without changing arguments containing quotes or spaces.
// This is also used for command display when a harness stores argv instead of a command string.
func JoinShellWords(args []string) string {
	words := make([]string, len(args))
	for i, arg := range args {
		if arg != "" && !strings.ContainsAny(arg, " \t\r\n\\\"'`$;&|<>(){}[]*?!~") {
			words[i] = arg
		} else {
			words[i] = ShellQuote(arg)
		}
	}
	return strings.Join(words, " ")
}

// ShellWords decodes shell-quoted argv for comparison, without executing or expanding anything.
// Unbalanced input is not comparable. Quoting style alone must not give a tool a new identity.
func ShellWords(command string) []string {
	var words []string
	var word strings.Builder
	var quote rune
	started := false
	chars := []rune(command)
	for i := 0; i < len(chars); i++ {
		ch := chars[i]
		if quote == '\'' {
			if ch == quote {
				quote = 0
			} else {
				word.WriteRune(ch)
			}
			continue
		}
		if ch == '\\' {
			if i+1 == len(chars) {
				return nil
			}
			next := chars[i+1]
			if quote == '"' && !strings.ContainsRune("$`\"\\\n", next) {
				word.WriteRune(ch)
				continue
			}
			i++
			if next != '\n' {
				word.WriteRune(next)
				started = true
			}
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				word.WriteRune(ch)
			}
		} else if ch == '\'' || ch == '"' {
			quote, started = ch, true
		} else if ch == ' ' || ch == '\t' || ch == '\n' {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		} else {
			word.WriteRune(ch)
			started = true
		}
	}
	if quote != 0 {
		return nil
	}
	if started {
		words = append(words, word.String())
	}
	return words
}
