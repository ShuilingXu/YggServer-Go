package database

import (
	"strconv"
	"strings"
)

func Bind(dialect, query string) string {
	if dialect != "postgres" {
		return query
	}
	n := 0
	var b strings.Builder
	for _, c := range query {
		if c == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}
