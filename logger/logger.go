package logger

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DesKaOne/deskaone-sdk/termcolor"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

type Logger struct {
	Name              string
	UseEmoji          bool
	ColorizeLevel     bool
	ColorizeMessage   bool
	IncludeNamePrefix bool
}

func New(name string) *Logger {
	return &Logger{
		Name:          name,
		UseEmoji:      true,
		ColorizeLevel: true,
	}
}
func (l *Logger) log(level, msg string) {
	now := time.Now().Format("15:04:05")

	label := level
	if l.ColorizeLevel {
		switch level {
		case "ERROR":
			label = termcolor.Red(level, false, true)
		case "WARN":
			label = termcolor.Yellow(level, false, true)
		case "SUCCESS":
			label = termcolor.Green(level, false, true)
		default:
			label = termcolor.Cyan(level, false, true)
		}
	}

	namePrefix := ""
	if l.IncludeNamePrefix {
		namePrefix = "[" + l.Name + "] "
	}
	if l.ColorizeMessage {
		msg = termcolor.White(msg, false, false)
	}

	prefix := fmt.Sprintf("=> %s | %s | ", now, label)
	fmt.Println(prefix + namePrefix + msg)
}

func (l *Logger) Info(m string)    { l.log("INFO   ", m) }
func (l *Logger) Warn(m string)    { l.log("WARN   ", m) }
func (l *Logger) Error(m string)   { l.log("ERROR  ", m) }
func (l *Logger) Success(m string) { l.log("SUCCESS", m) }

func DrawBox(x, y, w, h int) {
	// pindah cursor
	fmt.Printf("\x1b[%d;%dH", y, x)

	// top
	fmt.Print("┌" + strings.Repeat("─", w-2) + "┐")

	// body
	for i := 1; i < h-1; i++ {
		fmt.Printf("\x1b[%d;%dH│%s│",
			y+i, x,
			strings.Repeat(" ", w-2),
		)
	}

	// bottom
	fmt.Printf("\x1b[%d;%dH└%s┘",
		y+h-1, x,
		strings.Repeat("─", w-2),
	)
}

func DrawLayout() {
	fmt.Print("\x1b[2J") // clear screen

	width := 60
	height := 12

	leftW := 18
	rightW := width - leftW - 1

	DrawBox(1, 1, leftW, height)
	DrawBox(leftW+2, 1, rightW, height)
}

func WriteAt(x, y int, text string) {
	fmt.Printf("\x1b[%d;%dH%s", y, x, text)
}

func Spinner(stop <-chan struct{}, x, y int) {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	for {
		select {
		case <-stop:
			return
		default:
			WriteAt(x, y, frames[i%len(frames)])
			time.Sleep(100 * time.Millisecond)
			i++
		}
	}
}

func termSize() (w, h int) {
	w, h, _ = term.GetSize(int(os.Stdout.Fd()))
	return
}

func WatchResize(on func()) {
	ch := make(chan os.Signal, 1)
	//signal.Notify(ch, syscall.SIGWINCH)
	signal.Notify(ch, syscall.SIGPIPE)

	go func() {
		for range ch {
			on()
		}
	}()
}

type Panel struct {
	X, Y int
	W, H int

	buffer []string
	mu     sync.Mutex
}

func (p *Panel) Draw() {
	border := termcolor.Gray

	// top
	move(p.Y, p.X)
	//fmt.Print("┌" + strings.Repeat("─", p.W-2) + "┐")
	fmt.Print(border("┌" + strings.Repeat("─", p.W-2) + "┐"))

	// body
	for i := 1; i < p.H-1; i++ {
		move(p.Y+i, p.X)
		//fmt.Print("│" + strings.Repeat(" ", p.W-2) + "│")
		fmt.Print(border("│") +
			strings.Repeat(" ", p.W-2) +
			border("│"))
	}

	// bottom
	move(p.Y+p.H-1, p.X)
	//fmt.Print("└" + strings.Repeat("─", p.W-2) + "┘")
	fmt.Print(border("└" + strings.Repeat("─", p.W-2) + "┘"))
}

func (p *Panel) AppendLine(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	max := p.H - 2
	p.buffer = append(p.buffer, line)

	if len(p.buffer) > max {
		p.buffer = p.buffer[len(p.buffer)-max:]
	}

	p.redrawContent()
}

func (p *Panel) redrawContent() {
	max := p.H - 2

	for i := 0; i < max; i++ {
		move(p.Y+1+i, p.X+1)

		var line string
		if i < len(p.buffer) {
			line = p.buffer[i]
		} else {
			line = ""
		}

		// potong kalau kepanjangan
		if len(line) > p.W-2 {
			line = line[:p.W-2]
		}

		fmt.Print(padRight(line, p.W-2))
	}
}

func move(row, col int) {
	fmt.Printf("\x1b[%d;%dH", row, col)
}

func padRight(s string, w int) string {
	l := visibleLen(s)
	if l >= w {
		return s
	}
	return s + strings.Repeat(" ", w-l)
}

func Layout() (left, right *Panel) {
	w, h := termSize()

	leftW := w / 10
	rightW := w - leftW - 1

	left = &Panel{
		X: 1, Y: 1,
		W: leftW,
		H: h,
	}

	right = &Panel{
		X: leftW + 2, Y: 1,
		W: rightW,
		H: h,
	}

	return
}

func (p *Panel) SetLine(row int, text string) {
	if row < 0 || row >= p.H-2 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	move(p.Y+1+row, p.X+1)

	if len(text) > p.W-2 {
		text = text[:p.W-2]
	}

	fmt.Print(padRight(text, p.W-2))
}

func ColorLog(line string) string {
	switch {
	case strings.Contains(line, "ERROR"):
		return termcolor.Red(line, false, true)
	case strings.Contains(line, "WARN"):
		return termcolor.Yellow(line, false, true)
	case strings.Contains(line, "OK"):
		return termcolor.Green(line, false, false)
	default:
		return line
	}
}

func RunTimeLog(p *Panel) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for t := range ticker.C {
		p.AppendLine(t.Format("15:04:05"))
	}
}

func Header(text string, bold bool) {
	width := 80
	if isTerminal() {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			width = w
		}
	}

	border := "-"
	if bold {
		border = "="
	}

	borderLine := strings.Repeat(border, width)
	fmt.Println(borderLine)

	lines := strings.SplitSeq(strings.TrimSpace(text), "\n")
	for line := range lines {
		t := strings.TrimSpace(line)
		pad := max((width-visibleLen(t))/2, 0)
		centered := strings.Repeat(" ", pad) + t
		fmt.Println(padRight(centered, width))
	}

	fmt.Println(borderLine)
}

func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visibleLen(s string) int {
	return len(ansiRE.ReplaceAllString(s, ""))
}
