package pingme

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

func extractRemote(remote string) (net.IP, uint16) {
	sepidx := strings.LastIndex(remote, ":")
	addr := net.ParseIP(remote[:sepidx])

	port, err := strconv.ParseInt(remote[sepidx+1:len(remote)], 10, 64)
	if err != nil {
		port = 0
	}

	return addr, uint16(port)
}

func HandlePing(w http.ResponseWriter, r *http.Request) {

	addr, _ := extractRemote(r.RemoteAddr)

	// test: retrieve address from request and write it straight back
	fmt.Fprintf(w, "remote address is %s\r\n", addr.String())
}
