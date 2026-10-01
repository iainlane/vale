package lint

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

func (l *Linter) lintDITA(file *core.File) error {
	var out bytes.Buffer
	var htmlFile string

	if data, ok := l.ditaHTML[absPath(file.Path)]; ok {
		return l.lintDITAHTML(file, data)
	}

	dita := system.Which([]string{"dita", "dita.bat"})
	if dita == "" {
		return core.NewE100(file.Path, errors.New("dita not found; it's needed to read DITA"))
	}

	tempDir, err := os.MkdirTemp("", "dita-")
	defer os.RemoveAll(tempDir)

	if err != nil {
		return core.NewE201FromPosition(err.Error(), file.Path, 1)
	}

	// The toolkit reads the file from disk, so ignore patterns are applied
	// to a copy beside it, where its conrefs still resolve.
	input := file.Path
	if s, terr := l.Transform(file); terr != nil {
		return terr
	} else if s != file.Content {
		copied, cerr := os.CreateTemp(filepath.Dir(file.Path), ".vale-*.dita")
		if cerr != nil {
			return core.NewE100(file.Path, cerr)
		}
		defer os.Remove(copied.Name())
		if _, cerr = copied.WriteString(s); cerr != nil {
			return core.NewE100(file.Path, cerr)
		}
		if cerr = copied.Close(); cerr != nil {
			return core.NewE100(file.Path, cerr)
		}
		input = copied.Name()
	}

	// FIXME: The `dita` command is *slow* (~4s per file)!
	cmd := exec.Command(dita, []string{
		"-i",
		input,
		"-f",
		"html5",
		"-o",
		tempDir,
		"-t",
		filepath.Join(tempDir, "temp"), // this run's own, so two runs never share one
		"--nav-toc=none",
		"--outer.control=quiet", // allows DITA files to reference external files, like in conrefs.
	}...)
	cmd.Stderr = &out

	if err = cmd.Run(); err != nil {
		// dita's own diagnostics are far more useful than the exit status,
		// but it doesn't always write any
		if msg := strings.TrimSpace(out.String()); msg != "" {
			return core.NewE100(file.Path, errors.New(msg))
		}
		return core.NewE100(file.Path, err)
	}

	targetFileName := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)) + ".html"
	_ = filepath.WalkDir(tempDir, func(fp string, de os.DirEntry, _ error) error {
		// Find .html file, also looking in subdirectories in case an
		// "outer" file was referenced in the DITA file, which is allowed
		// because of the outer.control option of the dita command.
		if de.Name() == targetFileName {
			htmlFile = fp
		}
		return nil
	})

	data, err := os.ReadFile(htmlFile)
	if err != nil {
		return core.NewE100(htmlFile, err)
	}
	return l.lintDITAHTML(file, data)
}

// lintDITAHTML lints the toolkit's HTML for file, without its head.
func (l *Linter) lintDITAHTML(file *core.File, data []byte) error {
	head1 := bytes.Index(data, []byte("<head>"))
	head2 := bytes.Index(data, []byte("</head>"))

	if head1 >= 0 && head2 >= 0 {
		data = append(data[:head1], data[head2:]...)
	}

	return l.lintHTMLTokens(file, data, 0)
}

// prepareDITA converts every DITA file the run will lint in one toolkit
// run, through a map written in their common directory. Starting the
// toolkit costs more than converting a file, so a run of many files
// costs about what one did. A file the map run does not produce falls
// back to a run of its own.
func (l *Linter) prepareDITA(input []string) {
	paths := l.ditaInputs(input)
	if len(paths) < 2 {
		return
	}
	dita := system.Which([]string{"dita", "dita.bat"})
	if dita == "" {
		return
	}
	base := commonDir(paths)
	if base == "" {
		return
	}

	outDir, err := os.MkdirTemp("", "dita-")
	if err != nil {
		return
	}
	defer os.RemoveAll(outDir)

	// A file an ignore pattern changes is converted from a copy beside it.
	converted := map[string]string{}
	for _, p := range paths {
		f, ferr := core.NewFile(p, l.Manager.Config)
		if ferr != nil {
			continue
		}
		in := p
		if s, terr := l.Transform(f); terr == nil && s != f.Content {
			copied, cerr := os.CreateTemp(filepath.Dir(p), ".vale-*.dita")
			if cerr != nil {
				continue
			}
			_, werr := copied.WriteString(s)
			if cerr = copied.Close(); werr != nil || cerr != nil {
				os.Remove(copied.Name())
				continue
			}
			defer os.Remove(copied.Name())
			in = copied.Name()
		}
		converted[p] = in
	}

	mapFile, err := os.CreateTemp(base, ".vale-*.ditamap")
	if err != nil {
		return
	}
	defer os.Remove(mapFile.Name())

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE map PUBLIC "-//OASIS//DTD DITA Map//EN" "map.dtd">` + "\n<map>\n")
	for _, p := range paths {
		in, ok := converted[p]
		if !ok {
			continue
		}
		rel, rerr := filepath.Rel(base, absPath(in))
		if rerr != nil {
			continue
		}
		b.WriteString(`<topicref href="` + filepath.ToSlash(rel) + `"/>` + "\n")
	}
	b.WriteString("</map>\n")
	if _, err = mapFile.WriteString(b.String()); err != nil {
		return
	}
	if err = mapFile.Close(); err != nil {
		return
	}

	cmd := exec.Command(dita, "-i", mapFile.Name(), "-f", "html5", "-o", outDir,
		"-t", filepath.Join(outDir, "temp"), "--nav-toc=none", "--outer.control=quiet")
	if err = cmd.Run(); err != nil {
		return
	}

	l.ditaHTML = map[string][]byte{}
	for p, in := range converted {
		rel, rerr := filepath.Rel(base, absPath(in))
		if rerr != nil {
			continue
		}
		html := filepath.Join(outDir, strings.TrimSuffix(rel, filepath.Ext(rel))+".html")
		if data, readErr := os.ReadFile(html); readErr == nil {
			l.ditaHTML[absPath(p)] = data
		}
	}
}

// ditaInputs returns the DITA files the run will lint, walked as the run
// walks them.
func (l *Linter) ditaInputs(input []string) []string {
	var found []string
	for _, root := range input {
		_ = system.Walk(root, func(fp string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() && (core.ShouldIgnoreDirectory(fp) || (fp != root && l.isStylesPath(fp))) {
				return filepath.SkipDir
			} else if info.IsDir() || l.skip(fp) {
				return nil
			}
			if ext, _ := core.FormatFromExt(fp, l.Manager.Config.Formats); ext == ".dita" {
				found = append(found, fp)
			}
			return nil
		})
	}
	return found
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// commonDir is the deepest directory holding every path, or "" if the
// paths share none.
func commonDir(paths []string) string {
	dir := filepath.Dir(absPath(paths[0]))
	for {
		all := true
		for _, p := range paths {
			if !strings.HasPrefix(absPath(p), dir+string(filepath.Separator)) {
				all = false
				break
			}
		}
		if all {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
