package lint

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// XML configuration.
var xsltArgs = []string{
	"--stringparam",
	"use.extensions",
	"0",
	"--stringparam",
	"generate.toc",
	"nop",
}

func (l *Linter) lintXML(file *core.File) error {
	var out bytes.Buffer
	var eut bytes.Buffer

	xsltproc := system.Which([]string{"xsltproc", "xsltproc.exe"})
	if xsltproc == "" {
		return core.NewE100(file.Path, errors.New("xsltproc not found; it's needed to read XML"))
	} else if file.Transform == "" {
		return core.NewE100(
			file.Path,
			errors.New("no XSLT transform provided; set Transform in this file's section"))
	}

	args := append([]string{}, xsltArgs...)
	args = append(args, []string{file.Transform, "-"}...)

	cmd := exec.Command(xsltproc, args...)
	cmd.Stdin = strings.NewReader(file.Content)
	cmd.Stdout = &out
	cmd.Stderr = &eut

	if err := cmd.Run(); err != nil {
		return core.NewE100(file.Path, errors.New(eut.String()))
	}

	return l.lintHTMLTokens(file, out.Bytes(), 0)
}
