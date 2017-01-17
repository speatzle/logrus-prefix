package prefixed

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/mgutz/ansi"
	"path/filepath"
)

const reset = ansi.Reset

var (
	baseTimestamp time.Time
	isTerminal    bool
)

func init() {
	baseTimestamp = time.Now()
	isTerminal = logrus.IsTerminal()
}

func miniTS() int {
	return int(time.Since(baseTimestamp) / time.Second)
}

// TextFormatter is the prefixed version of logrus.TextFormatter
type TextFormatter struct {
	// Set to true to bypass checking for a TTY before outputting colors.
	ForceColors bool

	// Force disabling colors.
	DisableColors bool

	// Disable timestamp logging. useful when output is redirected to logging
	// system that already adds timestamps.
	DisableTimestamp bool

	// Enable logging of just the time passed since beginning of execution.
	ShortTimestamp bool

	// The fields are sorted by default for a consistent output. For
	// applications that log extremely frequently and don't use the JSON
	// formatter this may not be desired.
	DisableSorting bool

	// Indent multi-line messages by the timestamp length to preserve proper
	// alignment
	IndentMultilineMessage bool

	// Timestamp format to use for display when a full timestamp is printed.
	TimestampFormat string

	// Pad msg field with spaces on the right for display. The value for this
	// parameter will be the size of padding. Its default value is zero, which
	// means no padding will be applied for msg.
	SpacePadding int
}

func (f *TextFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	keys := make([]string, 0, len(entry.Data))
	for k := range entry.Data {
		if k != "prefix" {
			keys = append(keys, k)
		}
	}

	if !f.DisableSorting {
		sort.Strings(keys)
	}

	b := &bytes.Buffer{}

	prefixFieldClashes(entry.Data)

	isColorTerminal := isTerminal && (runtime.GOOS != "windows")
	isColored := (f.ForceColors || isColorTerminal) && !f.DisableColors

	timestampFormat := f.TimestampFormat
	if timestampFormat == "" {
		timestampFormat = time.Stamp
	}

	if isColored {
		f.printColored(b, entry, keys, timestampFormat)
	} else {
		if !f.DisableTimestamp {
			f.appendKeyValue(b, "time", entry.Time.Format(timestampFormat))
		}
		f.appendKeyValue(b, "level", entry.Level.String())
		if entry.Message != "" {
			f.appendKeyValue(b, "msg", entry.Message)
		}
		for _, key := range keys {
			f.appendKeyValue(b, key, entry.Data[key])
		}
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

func (f *TextFormatter) printColored(wr io.Writer, entry *logrus.Entry,
	keys []string, timestampFormat string) {
	var levelColor string
	var levelText string
	var debugInf string
	switch entry.Level {
	case logrus.InfoLevel:
		levelColor = ansi.Green
	case logrus.WarnLevel:
		levelColor = ansi.Yellow
	case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
		levelColor = ansi.Red
	case logrus.DebugLevel:
		pc, file, line, _ := runtime.Caller(6)
		file = filepath.Base(file)
		
		callername := runtime.FuncForPC(pc).Name()
		debugInf = fmt.Sprintf(" [%s][%s][%d]", callername, file, line)
		fallthrough
	default:
		levelColor = ansi.Blue
	}

	if entry.Level != logrus.WarnLevel {
		levelText = strings.ToUpper(entry.Level.String())
	} else {
		levelText = "WARN"
	}

	prefix := ""
	message := entry.Message

	if pfx, ok := entry.Data["prefix"]; ok {
		prefix = fmt.Sprint(" ", ansi.Cyan, pfx, ":", reset)
	} else if pfx, trimmed := extractPrefix(entry.Message); len(pfx) > 0 {
		prefix = fmt.Sprint(" ", ansi.Cyan, pfx, ":", reset)
		message = trimmed
	}

	messageFormat := "%s"
	if f.SpacePadding != 0 {
		messageFormat = fmt.Sprintf("%%-%ds", f.SpacePadding)
	}

	// Remember how many bytes we've written to the buffer (i.e. how long the
	// timestamp, etc. is).
	var padlen int
	if f.DisableTimestamp {
		padlen, _ = fmt.Fprintf(wr, "%s%s %s%+5s%s%s%s ", ansi.LightBlack, reset,
			levelColor, levelText, reset, debugInf, prefix)
	} else {
		if f.ShortTimestamp {
			padlen, _ = fmt.Fprintf(wr, "%s[%04d]%s %s%+5s%s%s%s ",
				ansi.LightBlack, miniTS(), reset, levelColor, levelText, reset,
				debugInf, prefix)
		} else {
			padlen, _ = fmt.Fprintf(wr, "%s[%s]%s %s%+5s%s%s%s ", ansi.LightBlack,
				entry.Time.Format(timestampFormat), reset, levelColor,
				levelText, reset, debugInf, prefix)
		}
	}

	if f.IndentMultilineMessage && strings.ContainsRune(message, '\n') {
		// here we subtract the length of the used control characters
		padlen -= len(ansi.LightBlack) + len(levelColor) + 2*len(reset)
		if prefix != "" {
			padlen -= len(ansi.Cyan) + len(reset)
		}
		fmt.Fprintf(wr, messageFormat, strings.Replace(message, "\n", "\n"+
			strings.Repeat(" ", padlen), -1))
	} else {
		fmt.Fprintf(wr, messageFormat, message)
	}

	for _, k := range keys {
		v := entry.Data[k]
		fmt.Fprintf(wr, " %s%s%s=%+v", levelColor, k, reset, v)
	}
}

func needsQuoting(text string) bool {
	for _, ch := range text {
		if !((ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '.') {
			return false
		}
	}
	return true
}

func extractPrefix(msg string) (string, string) {
	prefix := ""
	regex := regexp.MustCompile(`^\[(.*?)\]`)
	if regex.MatchString(msg) {
		match := regex.FindString(msg)
		prefix, msg = match[1:len(match)-1], strings.TrimSpace(msg[len(match):])
	}
	return prefix, msg
}

func (f *TextFormatter) appendKeyValue(b *bytes.Buffer, key string,
	value interface{}) {
	b.WriteString(key)
	b.WriteByte('=')

	switch value := value.(type) {
	case string:
		if needsQuoting(value) {
			b.WriteString(value)
		} else {
			fmt.Fprintf(b, "%q", value)
		}
	case error:
		errmsg := value.Error()
		if needsQuoting(errmsg) {
			b.WriteString(errmsg)
		} else {
			fmt.Fprintf(b, "%q", value)
		}
	default:
		fmt.Fprint(b, value)
	}

	b.WriteByte(' ')
}

func prefixFieldClashes(data logrus.Fields) {
	_, ok := data["time"]
	if ok {
		data["fields.time"] = data["time"]
	}
	_, ok = data["msg"]
	if ok {
		data["fields.msg"] = data["msg"]
	}
	_, ok = data["level"]
	if ok {
		data["fields.level"] = data["level"]
	}
}
