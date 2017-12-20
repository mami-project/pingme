package pingme

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var timeOpt string
var ping4Regexp, ping6Regexp *regexp.Regexp
var parsePingLine func(*regexp.Regexp, string) *PingData

func init() {
	var regexp4Str, regexp6Str string
	var err error

	switch runtime.GOOS {
	case "darwin":
		regexp4Str = "\\d+ bytes from ([^:]+): icmp_seq=(\\d+) ttl=(\\d+) time=(\\d+\\.\\d+) ms"
		regexp6Str = "\\d+ bytes from ([^,]+), icmp_seq=(\\d+) hlim=(\\d+) time=(\\d+\\.\\d+) ms"
		parsePingLine = parsePingLineMac
	case "linux":
		regexp4Str = "\\d+ bytes from ([^:]+): icmp_seq=(\\d+) ttl=(\\d+) time=(\\d+\\.\\d+) ms"
		regexp6Str = "\\d+ bytes from ([^,]+), icmp_seq=(\\d+) hlim=(\\d+) time=(\\d+\\.\\d+) ms"
		parsePingLine = parsePingLineLinux
	}

	ping4Regexp, err = regexp.Compile(regexp4Str)
	if err != nil {
		panic(err)
	}

	ping6Regexp, err = regexp.Compile(regexp6Str)
	if err != nil {
		panic(err)
	}
}

type PingData struct {
	Seq int
	At  time.Time
	RTT time.Duration
}

func (d *PingData) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})

	out["seq"] = d.Seq
	out["at"] = float64(d.At.UnixNano()) / 1e9
	out["rtt"] = float64(d.RTT) / 1e6

	return json.Marshal(out)
}

func isV6(addr net.IP) bool {
	return strings.Contains(addr.String(), ":")
}

func pingArgs(
	addr net.IP,
	period time.Duration,
	duration time.Duration) []string {

	return []string{
		"-i",
		fmt.Sprintf("%.2f", (float64(period) / 1e9)),
		"-c",
		fmt.Sprintf("%d", duration/period),
		addr.String(),
	}

}

func parsePingLineMac(re *regexp.Regexp, line string) *PingData {
	m := re.FindStringSubmatch(line)
	if len(m) > 0 {
		seq, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			return nil
		}

		rtt, err := strconv.ParseFloat(m[4], 64)
		if err != nil {
			return nil
		}

		out := PingData{
			Seq: int(seq),
			RTT: time.Duration(int64(rtt * 1e6)),
		}

		return &out
	}

	return nil
}

func parsePingLineLinux(re *regexp.Regexp, line string) *PingData {
	m := re.FindStringSubmatch(line)
	if len(m) > 0 {
		seq, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			return nil
		}

		rtt, err := strconv.ParseFloat(m[4], 64)
		if err != nil {
			return nil
		}

		out := PingData{
			Seq: int(seq),
			RTT: time.Duration(int64(rtt * 1e6)),
		}

		return &out
	}

	return nil
}

func consumePingOutput(
	ctx context.Context,
	re *regexp.Regexp,
	pipe io.ReadCloser,
	data chan *PingData) {

	// FIXME check context

	// scan input until we're done
	in := bufio.NewScanner(pipe)
	for in.Scan() {
		pd := parsePingLine(re, in.Text())
		if pd != nil {
			// got data, stamp the time and write to channel
			pd.At = time.Now()
			data <- pd
		}
	}

	// signal done by closing channel
	close(data)
}

func PingTarget(
	ctx context.Context,
	addr net.IP,
	period time.Duration,
	duration time.Duration,
	waitgroup chan struct{}) ([]*PingData, error) {

	var pingcmd string
	var pingre *regexp.Regexp
	if isV6(addr) {
		pingcmd = "ping6"
		pingre = ping6Regexp
	} else {
		pingcmd = "ping"
		pingre = ping4Regexp
	}

	// create an appropriate ping command
	log.Printf("will run %s %s", pingcmd, strings.Join(pingArgs(addr, period, duration), " "))
	ping := exec.CommandContext(ctx, pingcmd, pingArgs(addr, period, duration)...)

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

	log.Printf("pinging %s: started %s", addr, pingcmd)

	// start a goroutine to read from ping
	pingdata := make(chan *PingData)
	go consumePingOutput(ctx, pingre, pingpipe, pingdata)

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

type PartialPingResultFile struct {
	Complete bool   `json:"complete"`
	Link     string `json:"link"`
}

type PingResultFile struct {
	Complete bool        `json:"complete"`
	Link     string      `json:"link"`
	Target   string      `json:"target"`
	Results  []*PingData `json:"results"`
}

func GoPingAndStore(
	cachedir string,
	ctx context.Context,
	addr net.IP,
	period time.Duration,
	duration time.Duration,
	waitgroup chan struct{}) (uuid.UUID, error) {

	// generate a uuid for the output
	id := uuid.New()

	link := fmt.Sprintf("/data/%s", id.String())

	// open and create an output file
	outpath := filepath.Join(cachedir, fmt.Sprintf("%s.json", id.String()))
	outfile, err := os.Create(outpath)
	if err != nil {
		return uuid.UUID{}, err
	}

	// Write a pending message
	b, _ := json.Marshal(PartialPingResultFile{Complete: false, Link: link})
	outfile.Write(b)

	log.Printf("%v: will ping %v with period %v duration %v", id, addr, period, duration)

	// kick off the ping
	go func() {
		defer outfile.Close()

		data, err := PingTarget(context.Background(), addr, period, duration, waitgroup)

		log.Printf("%v: done pinging %v", id, addr)
		outfile.Seek(0, 0)
		outfile.Truncate(0)

		if err != nil {
			b, err2 := json.Marshal(struct {
				Error string `json:"error"`
			}{err.Error()})
			if err2 == nil {
				outfile.Write(b)
				return
			}
		}

		b, err := json.Marshal(PingResultFile{
			Complete: true,
			Link:     link,
			Target:   addr.String(),
			Results:  data,
		})
		if err == nil {
			outfile.Write(b)
		}
	}()

	// return the uuid for later retrieval
	return id, nil
}
