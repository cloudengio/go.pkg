// Package textdiff providers support for diff'ing text.
package textdiff

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"os"
	// "os"
	"strings"
	"unicode/utf8"

	"cloudeng.io/algo/codec"
	"cloudeng.io/algo/lcs"
)

func LineFNVHashDecoder(data []byte) (string, int64, int) {
	if len(data) == 0 {
		return "", 0, 0
	}
	idx := bytes.Index(data, []byte{'\n'})
	if idx < 0 {
		idx = len(data) - 1
	}
	h := fnv.New64a()
	h.Write(data[:idx])
	sum := h.Sum64()
	return string(data[:idx]), int64(sum), idx + 1
}

/*
func WordDecoder(data []byte) (string, int) {
	idx := bytes.IndexFunc(data, unicode.IsSpace)
	if idx < 0 {
		return "", 0
	}
	h := fnv.New64a()
	h.Write(data[:idx])
	sum := h.Sum64()
	return string(data[:idx]), int64(sum), idx + 1
}*/

type LineDecoder struct {
	lines []string
}

func (ld *LineDecoder) Decode(data []byte) (int64, int) {
	line, sum, n := LineFNVHashDecoder(data)
	fmt.Printf("D: %v %v\n", len(ld.lines), line)
	ld.lines = append(ld.lines, line)
	return int64(sum), n
}

type group struct {
	linesA, linesB []int
	textA, textB   string
	edits          lcs.EditScript
}

type Diff struct {
	ld             *LineDecoder
	linesA, linesB []string
	groups         []group
}

func text(orig []string, lines []int) string {
	out := strings.Builder{}
	for _, l := range lines {
		out.WriteString(orig[l])
		out.WriteString("\n")
	}
	return out.String()
}

func lineRange(lines []int) string {
	if len(lines) == 0 {
		return "[]"
	}
	return fmt.Sprintf("%d..%d", lines[0], lines[len(lines)-1])
}

func getDifferentLines(edits lcs.EditScript) (a, b []int, script lcs.EditScript) {
	last := 0
	for i, edit := range edits {
		switch edit.Op {
		case lcs.Identical:
			return a, b, edits[i+1:]
		case lcs.Delete:
			a = append(a, edit.A)
		case lcs.Insert:
			b = append(b, edit.B)
		}
		last = i
	}
	return a, b, edits[last+1:]
}

func DiffByLines(a, b []byte) *Diff {
	lda, ldb := &LineDecoder{}, &LineDecoder{}
	decA, err := codec.NewDecoder(lda.Decode)
	if err != nil {
		panic(err)
	}
	decB, _ := codec.NewDecoder(ldb.Decode)
	da, db := decA.Decode([]byte(a)), decB.Decode([]byte(b))

	utf8Dec, err := codec.NewDecoder(utf8.DecodeRune)
	if err != nil {
		panic(err)
	}

	diff := &Diff{
		linesA: lda.lines,
		linesB: ldb.lines,
	}

	lineDiffs := lcs.NewMyers(da, db).SES()
	script := lineDiffs
	for len(script) > 0 {
		var linesA, linesB []int
		linesA, linesB, script = getDifferentLines(script)
		if len(linesA) == 0 && len(linesB) == 0 {
			continue
		}
		diff.groups = append(diff.groups, group{
			linesA: linesA,
			linesB: linesB,
			textA:  text(diff.linesA, linesA),
			textB:  text(diff.linesB, linesB),
		})
		fmt.Printf("A: %v\n", linesA)
	}

	for i, g := range diff.groups {
		a, b := utf8Dec.Decode([]byte(g.textA)), utf8Dec.Decode([]byte(g.textB))
		diff.groups[i].edits = lcs.NewMyers(a, b).SES()
	}

	//	lcs.PrettyVertical(os.Stdout, da, lineDiffs)
	for _, g := range diff.groups {
		fmt.Printf("<<< %v >>> %v\n", lineRange(g.linesA), lineRange(g.linesB))
		fmt.Printf("%s\n", g.textA)
		fmt.Printf("------------------\n")
		fmt.Printf("%s\n", g.textB)
		lcs.PrettyHorizontal(os.Stdout, []int32(g.textA), g.edits)
	}
	return diff
}

// multiline change 1
// multiline change 2
