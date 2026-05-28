package termcolor

import (
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/mattn/go-isatty"
)

func init() {
	if runtime.GOOS == "windows" {
		//enableVirtualTerminal()
	}
}

var Enabled = detectEnabled()

var cache sync.Map

func detectEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd())
}

func SetEnabled(v bool) {
	Enabled = v
	cache = sync.Map{}
}

func wrap(m string, open string) string {
	if !Enabled {
		return m
	}
	return open + m + "\x1b[0m"
}

func color(code int, bg bool, bold bool) string {

	s := ""
	if bold {
		s += "\x1b[1m"
	}

	if bg {
		s += fmt.Sprintf("\x1b[%dm", code+10) // 40–47
	} else {
		s += fmt.Sprintf("\x1b[%dm", code) // 30–37
	}

	return s
}
func Red(m string, bg, bold bool) string     { return wrap(m, color(31, bg, bold)) }
func Green(m string, bg, bold bool) string   { return wrap(m, color(32, bg, bold)) }
func Yellow(m string, bg, bold bool) string  { return wrap(m, color(33, bg, bold)) }
func Blue(m string, bg, bold bool) string    { return wrap(m, color(34, bg, bold)) }
func Magenta(m string, bg, bold bool) string { return wrap(m, color(35, bg, bold)) }
func Cyan(m string, bg, bold bool) string    { return wrap(m, color(36, bg, bold)) }
func White(m string, bg, bold bool) string   { return wrap(m, color(37, bg, bold)) }
func Gray(m string) string                   { return wrap(m, "\x1b[90m") }
func XTerm(m string, code int, bg, bold bool) string {
	if !Enabled {
		return m
	}
	if code < 0 {
		code = 0
	}
	if code > 255 {
		code = 255
	}
	s := ""
	if bold {
		s += "\x1b[1m"
	}
	if bg {
		s += fmt.Sprintf("\x1b[48;5;%dm", code)
	} else {
		s += fmt.Sprintf("\x1b[38;5;%dm", code)
	}
	return s + m + "\x1b[0m"
}
func RGB(m string, r, g, b int, bg, bold bool) string {
	if !Enabled {
		return m
	}
	if bold {
		m = "\x1b[1m" + m
	}
	if bg {
		return fmt.Sprintf("\x1b[48;2;%d;%d;%dm%s\x1b[0m", r, g, b, m)
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, m)
}

type StyleOpt struct {
	Bold      bool
	Italic    bool
	Underline bool
	Dim       bool
	Inverse   bool
	Strike    bool
}

func Style(m string, o StyleOpt) string {
	if !Enabled {
		return m
	}
	var codes []int
	if o.Bold {
		codes = append(codes, 1)
	}
	if o.Italic {
		codes = append(codes, 3)
	}
	if o.Underline {
		codes = append(codes, 4)
	}
	if o.Dim {
		codes = append(codes, 2)
	}
	if o.Inverse {
		codes = append(codes, 7)
	}
	if o.Strike {
		codes = append(codes, 9)
	}
	if len(codes) == 0 {
		return m
	}
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", codes, m)
}
