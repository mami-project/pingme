package pingme

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

func extractRemote(remote string) (net.IP, uint16) {
	sepidx := strings.LastIndex(remote, ":")
	addr := net.ParseIP(strings.Trim(remote[:sepidx], "[]"))

	port, err := strconv.ParseInt(remote[sepidx+1:len(remote)], 10, 64)
	if err != nil {
		port = 0
	}

	return addr, uint16(port)
}

func streamCopy(in io.Reader, out io.Writer) error {
	buf := make([]byte, 65536)
	for {
		n, err := in.Read(buf)
		if err == nil {
			if _, err = out.Write(buf[0:n]); err != nil {
				return err
			}
		} else if err == io.EOF {
			return nil
		} else {
			return err
		}
	}
}

type PingAPI struct {
	cachedir  string
	page      []byte
	waitgroup chan struct{}
}

func NewPingAPI(cachedir string, roothtml string, concurrency int) (*PingAPI, error) {
	pa := new(PingAPI)
	pa.cachedir = cachedir
	pa.waitgroup = make(chan struct{}, concurrency)

	var err error
	pa.page, err = ioutil.ReadFile(roothtml)
	if err != nil {
		return nil, err
	}

	return pa, nil
}

func (pa *PingAPI) AddRoutes(r *mux.Router) {
	r.HandleFunc("/", pa.HandleRoot).Methods("GET")
	r.HandleFunc("/ping", pa.HandlePingRequest).Methods("GET", "POST")
	r.HandleFunc("/data/{id}", pa.HandlePingRetrieve).Methods("GET")
}

func (pa *PingAPI) HandleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write(pa.page)
}

func (pa *PingAPI) HandlePingRequest(w http.ResponseWriter, r *http.Request) {
	addr, _ := extractRemote(r.RemoteAddr)

	// defaults
	period := 1 * time.Second
	duration := 30 * time.Second

	// get information from the query string
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("could not parse form: %s", err.Error()), http.StatusBadRequest)
		return
	}

	if r.Form.Get("period") != "" {
		periodF, err := strconv.ParseFloat(r.Form.Get("period"), 64)
		if err == nil {
			period = time.Duration(int64(periodF * 1e9))
		}
	}

	if r.Form.Get("duration") != "" {
		durationI, err := strconv.ParseInt(r.Form.Get("duration"), 10, 64)
		if err == nil {
			duration = time.Duration(int64(durationI * 1e9))
		}
	}

	// kick off the ping to get a UUID
	id, err := GoPingAndStore(pa.cachedir, r.Context(), addr, period, duration, pa.waitgroup)
	if err != nil {
		log.Printf("error pinging %s: %s", addr.String(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return a link in JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	b, _ := json.Marshal(PartialPingResultFile{Complete: false, Link: fmt.Sprintf("/data/%s", id.String())})
	w.Write(b)
}

func (pa *PingAPI) HandlePingRetrieve(w http.ResponseWriter, r *http.Request) {

	// get information from the request
	v := mux.Vars(r)

	if v["id"] == "" {
		http.Error(w, "missing ID to retrieve", http.StatusBadRequest)
		return
	}

	// open and download file
	inpath := filepath.Join(pa.cachedir, fmt.Sprintf("%s.json", v["id"]))

	pathok, err := filepath.Match(filepath.Join(pa.cachedir, "*"), inpath)
	if err != nil {
		log.Printf("error retrieving ping %s: %s", v["id"], err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !pathok {
		http.Error(w, fmt.Sprintf("path %s not ok", inpath), http.StatusBadRequest)
		return
	}

	infile, err := os.Open(inpath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "ping not found", http.StatusBadRequest)
			return
		} else {
			log.Printf("error retrieving ping %s: %s", v["id"], err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	streamCopy(infile, w)
}
