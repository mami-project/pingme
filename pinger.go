package pingme

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var timeOpt string
var pingRegexp *regexp.Regexp
var parsePingLine func(string) *PingData

func init() {
	var regexpStr string
	var err error

	switch runtime.GOOS {
	case "darwin":
		timeOpt = "--apple-time"
		regexpStr = "(\\d+:\\d+:\\d+\\.\\d+) \\d+ bytes from ([^:]+): icmp_seq=(\\d+) ttl=(\\d+) time=(\\d+\\.\\d+) ms"
		parsePingLine = parsePingLineMac
	case "linux":
		timeOpt = "-D"
		regexpStr = "\\[(\\d+\\.\\d+)\\] \\d+ bytes from ([^:]+): icmp_seq=(\\d+) ttl=(\\d+) time=(\\d+\\.\\d+) ms"
		parsePingLine = parsePingLineLinux
	}

	pingRegexp, err = regexp.Compile(regexpStr)
	if err != nil {
		panic(err)
	}
}

type PingData struct {
	Seq int
	At  time.Time
	RTT time.Duration
}

func pingCommand(targetAddr net.IP) string {
	if strings.Contains(targetAddr.String(), ":") {
		return "ping6"
	} else {
		return "ping"
	}
}

func pingArgs(
	targetAddr net.IP,
	period time.Duration,
	duration time.Duration) []string {

	return []string{
		timeOpt,
		"-i",
		fmt.Sprintf("%.2f", (float64(duration) / 1e9)),
		"-c",
		fmt.Sprintf("%d", duration/period),
		targetAddr.String(),
	}

}

func parsePingLineMac(line string) *PingData {
	m := pingRegexp.FindStringSubmatch(line)
	if len(m) > 0 {
		seq, err := strconv.ParseInt(m[3], 10, 64)
		if err != nil {
			return nil
		}

		at, err := time.Parse("15:04:05.999999", m[1])
		if err != nil {
			return nil
		}

		rtt, err := strconv.ParseFloat(m[5], 64)
		if err != nil {
			return nil
		}

		out := PingData{
			Seq: int(seq),
			At:  at,
			RTT: time.Duration(int64(rtt * 1e6)),
		}

		return &out
	}

	return nil
}

func parsePingLineLinux(line string) *PingData {
	m := pingRegexp.FindStringSubmatch(line)
	if len(m) > 0 {
		seq, err := strconv.ParseInt(m[3], 10, 64)
		if err != nil {
			return nil
		}

		atfloat, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return nil
		}

		atsec, atnsec := math.Modf(atfloat)
		atnsec *= 1e9

		at := time.Unix(int64(atsec), int64(atnsec))

		rtt, err := strconv.ParseFloat(m[5], 64)
		if err != nil {
			return nil
		}

		out := PingData{
			Seq: int(seq),
			At:  at,
			RTT: time.Duration(int64(rtt * 1e6)),
		}

		return &out
	}

	return nil
}

func parsePingOutput(pipe io.ReadCloser, data chan *PingData) {

	// scan input until we're done
	in := bufio.NewScanner(pipe)
	for in.Scan() {
		pd := parsePingLine(in.Text())
		if pd != nil {
			// got data, write to channel
			data <- pd
		}
	}

	// signal done by closing channel
	close(data)
}

func PingTarget(
	targetAddr net.IP,
	period time.Duration,
	duration time.Duration,
	waitgroup chan struct{}) ([]*PingData, error) {

	// create an appropriate ping command
	ping := exec.Command(pingCommand(targetAddr), pingArgs(targetAddr, period, duration)...)

	// get a pipe for reading output
	pingpipe, err := ping.StdoutPipe()
	if err != nil {
		return nil, err
	}

	// ensure max concurrent ping
	waitgroup <- struct{}{}
	defer func() { <-waitgroup }()

	// start pinging
	if err := ping.Start(); err != nil {
		return nil, err
	}

	// start a goroutine to read from ping
	pingdata := make(chan *PingData)
	go parsePingOutput(pingpipe, pingdata)

	// collect output
	out := make([]*PingData, 0)

	for pd := range pingdata {
		out = append(out, pd)
	}

	// reap ping
	if err := ping.Wait(); err != nil {
		return nil, err
	}

	return out, nil
}
