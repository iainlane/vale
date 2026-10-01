package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
)

type valeError struct {
	line    int
	path    string
	text    string
	code    string
	span    int
	context string // an E100's `[context]`, which has no file position
}

var logger = log.New(os.Stderr, "", 0)
var header = regexp.MustCompile(`(\w+) .+ \[(.+):(\d+):(\d+)\]`)
var runtimeHeader = regexp.MustCompile(`^(E\d+) \[(.+?)\] `)

func parseError(err error) (valeError, error) {
	var parsed valeError

	plain := core.StripANSI(err.Error())
	lines := strings.Split(plain, "\n\n")

	if len(lines) < 3 {
		return parsed, errors.New("missing body")
	} else if m := runtimeHeader.FindStringSubmatch(lines[0]); m != nil && !header.MatchString(lines[0]) {
		// An E100 has no position: its body is everything between the
		// header and the closing line.
		parsed.code, parsed.context = m[1], m[2]
		parsed.text = strings.Join(lines[1:len(lines)-1], "\n\n")
		return parsed, nil
	} else if !header.MatchString(lines[0]) {
		return parsed, errors.New("missing header")
	}

	groups := header.FindStringSubmatch(lines[0])

	parsed.code = groups[1]
	parsed.path = groups[2]

	i, err := strconv.Atoi(groups[3])
	if err != nil {
		return parsed, errors.New("missing line")
	}
	parsed.line = i

	i, err = strconv.Atoi(groups[4])
	if err != nil {
		return parsed, errors.New("missing span")
	}
	parsed.span = i

	parsed.text = lines[len(lines)-2]
	return parsed, nil
}

// ShowError displays the given error in the user-specified format.
func ShowError(err error, style string, out io.Writer) {
	parsed, failed := parseError(err)

	logger.SetOutput(out)
	switch style {
	case "JSON":
		var data interface{}

		if failed != nil {
			data = struct {
				Line int
				Path string
				Text string
				Code string
				Span int
			}{
				Line: 0,
				Path: "",
				Text: core.StripANSI(err.Error()),
				Code: "E100",
				Span: 0,
			}
		} else {
			data = struct {
				Line int
				Path string
				Text string
				Code string
				Span int
			}{
				Line: parsed.line,
				Path: parsed.path,
				Text: parsed.text,
				Code: parsed.code,
				Span: parsed.span,
			}
		}

		logger.Println(getJSON(data))
	case "line":
		var data string

		if failed != nil {
			data = err.Error()
		} else {
			data = fmt.Sprintf("%s:%d:%s:%s",
				parsed.path, parsed.line, parsed.code, parsed.text)
			if parsed.path == "" {
				// No file to point at: the context stands in for it, and
				// the message keeps to one line.
				first, _, _ := strings.Cut(parsed.text, "\n")
				data = fmt.Sprintf("[%s]:%s:%s", parsed.context, parsed.code, first)
			}
		}

		logger.Println(data)
	default:
		logger.Println(err)
	}
}
