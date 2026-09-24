package kube

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

// The rendering vocabulary shared by every table and report: how a cell is
// toned, how an age is written, and the describer that lays out a report.

// AllNamespaces is the namespace filter meaning "do not filter".
const AllNamespaces = ""

// rowID is the key the frontend uses to identify a row across refreshes.
func rowID(kind, namespace, name string) string {
	return kind + "/" + namespace + "/" + name
}

func plain(s string) Cell       { return Cell{Text: s} }
func toned(s, tone string) Cell { return Cell{Text: s, Tone: tone} }
func status(s string) Cell      { return Cell{Text: s, Tone: toneFor(s)} }
func number(n int) Cell         { return Cell{Text: fmt.Sprintf("%d", n)} }
func muted(s string) Cell       { return Cell{Text: s, Tone: "info"} }

// tagged is a cell that says several things at once, each in its own tone: a
// node that is Ready and cordoned is both, and green alone or amber alone hides
// half of it. Text is kubectl's spelling, "Ready,SchedulingDisabled", so a
// filter or a sort still reads the cell as one string; Tone is the worst of the
// parts, for anything that can draw only one. A single tag is an ordinary cell.
func tagged(tags []Tag) Cell {
	if len(tags) == 0 {
		return Cell{}
	}
	texts := make([]string, len(tags))
	tone := ""
	for i, t := range tags {
		texts[i] = t.Text
		if toneRank(t.Tone) > toneRank(tone) {
			tone = t.Tone
		}
	}
	c := Cell{Text: strings.Join(texts, ","), Tone: tone}
	if len(tags) > 1 {
		c.Tags = tags
	}
	return c
}

// toneRank orders the tones by how much they want to be seen.
func toneRank(tone string) int {
	switch tone {
	case "error":
		return 4
	case "warn":
		return 3
	case "ok":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// farFuture sorts a cell with no time after every cell that has one, so
// "<none>" collects at the end rather than leading the list.
const farFuture = 1 << 62

// timeCell renders a moment as an age and carries the seconds behind it.
//
// The seconds are what makes ascending order mean "most recent first": a
// smaller age is a more recent event, so the natural first click on Last Seen
// or Age gives what the reader wants without a descending pass.
func timeCell(t time.Time) Cell {
	if t.IsZero() {
		return Cell{Text: "<none>", Tone: "info", Sort: strconv.FormatInt(farFuture, 10)}
	}
	seconds := int64(time.Since(t).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return Cell{Text: age(int(seconds / 60)), Tone: "info", Sort: strconv.FormatInt(seconds, 10), At: t.UTC().Format(time.RFC3339)}
}

// durationCell renders an elapsed span, which sorts by its own length.
func durationCell(d time.Duration) Cell {
	seconds := int64(d.Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return Cell{Text: age(int(seconds / 60)), Tone: "info", Sort: strconv.FormatInt(seconds, 10)}
}

// quantityCell renders a Kubernetes quantity and sorts it by its value, so
// "500Mi" lands below "5Gi" instead of above it.
func quantityCell(s string) Cell {
	if s == "" {
		return Cell{}
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return Cell{Text: s}
	}
	return Cell{Text: s, Sort: strconv.FormatInt(q.Value(), 10)}
}

// age renders minutes the way kubectl does: the two most significant units,
// dropping to one once the value is large.
func age(minutes int) string {
	switch {
	case minutes < 1:
		return "0s"
	case minutes < 60:
		return fmt.Sprintf("%dm", minutes)
	case minutes < 60*48:
		h, m := minutes/60, minutes%60
		if h < 10 && m > 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	default:
		d := minutes / (60 * 24)
		if d < 10 {
			if h := (minutes % (60 * 24)) / 60; h > 0 {
				return fmt.Sprintf("%dd%dh", d, h)
			}
		}
		return fmt.Sprintf("%dd", d)
	}
}

// toneFor maps a status string onto the four tones the frontend knows how to
// colour, so status colouring lives in one place rather than in every view.
func toneFor(status string) string {
	switch status {
	case "Running", "Ready", "Active", "Available", "Bound", "Complete", "Completed", "Succeeded", "True", "Normal":
		return "ok"
	case "Pending", "Terminating", "Progressing", "ContainerCreating", "PodInitializing", "Suspended", "Warning", "SchedulingDisabled":
		return "warn"
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "Failed", "Error",
		"NotReady", "Evicted", "Lost", "OOMKilled", "Unknown":
		return "error"
	default:
		return ""
	}
}

// describer lays out a report in the aligned, sectioned shape the slide-in
// panel renders.
type describer struct{ b strings.Builder }

func (d *describer) field(k, v string) {
	fmt.Fprintf(&d.b, "%-22s%s\n", k+":", v)
}

func (d *describer) section(title string) {
	d.b.WriteString(title + ":\n")
}

func (d *describer) line(indent int, format string, args ...any) {
	d.b.WriteString(strings.Repeat(" ", indent))
	fmt.Fprintf(&d.b, format, args...)
	d.b.WriteString("\n")
}

func (d *describer) blank() { d.b.WriteString("\n") }
