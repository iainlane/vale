package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
)

var logger = log.New(os.Stderr, "", 0)

// asValeError returns err as Vale's own error, or wraps one that isn't --
// from a library, say -- as an E100.
func asValeError(err error) *core.Error {
	var ve *core.Error
	if errors.As(err, &ve) {
		return ve
	}
	return &core.Error{Code: "E100", Msg: err.Error()}
}

// ShowError displays the given error in the user-specified format.
func ShowError(err error, style string, out io.Writer) {
	ve := asValeError(err)

	logger.SetOutput(out)
	switch style {
	case "JSON":
		logger.Println(getJSON(struct {
			Line int
			Path string
			Text string
			Code string
			Span int
		}{
			Line: ve.Line,
			Path: filepath.ToSlash(ve.Path),
			Text: ve.Msg,
			Code: ve.Code,
			Span: ve.Span,
		}))
	case "line":
		// One line, as the alerts are: a position when there is one, and
		// otherwise the context stands in for it. A message can span lines
		// (a parser's own excerpt, say), so only its first is kept.
		first, _, _ := strings.Cut(ve.Msg, "\n")
		switch {
		case ve.Path != "":
			logger.Println(fmt.Sprintf("%s:%d:%s:%s", filepath.ToSlash(ve.Path), ve.Line, ve.Code, first))
		case ve.Context != "":
			logger.Println(fmt.Sprintf("[%s]:%s:%s", ve.Context, ve.Code, first))
		default:
			logger.Println(fmt.Sprintf("%s:%s", ve.Code, first))
		}
	default:
		logger.Println(err)
	}
}
