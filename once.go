package main // import "github.com/afq984/once"

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/handlers"
)

func OutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:8")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	return conn.LocalAddr().(*net.UDPAddr).IP
}

var HTMLTemplate = template.Must(template.New("page").Parse(
	`<title>Link expires after 1 day or downloading</title>
<h3>{{ .Basename }}</h3>
<p><a href=
"{{ .DownloadURL }}"
>Download</a></p>
<dl>
<dt>size</dt><dd>{{ .FileSize }}</dd>
<dt>sha1</dt><dd>{{ .Sha1Sum }}</dd>
<dt>sha256</dt><dd>{{ .Sha256Sum }}</dd>
`))

type Handler struct {
	Basename    string
	Filename    string
	FileSize    int64
	ModTime     time.Time
	Sha1Sum     string
	Sha256Sum   string
	EntryURL    string
	DownloadURL string
	Done        chan struct{}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	switch r.URL.Path {
	case h.EntryURL:
		HTMLTemplate.Execute(w, h)
	case h.DownloadURL:
		h.HandleDownload(w, r)
	default:
		http.Error(w, "Not Found", http.StatusNotFound)
	}
}

func (h *Handler) HandleDownload(w http.ResponseWriter, r *http.Request) {
	file, err := os.Open(h.Filename)
	if err != nil {
		http.Error(w, "Oops Open", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, h.Basename, h.ModTime, file)
	close(h.Done)
}

var _ http.Handler = &Handler{}

func makeHandler(filename string) *Handler {
	h := &Handler{
		Filename: filename,
		Done:     make(chan struct{}),
	}

	file, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		panic(err)
	}
	h.ModTime = stat.ModTime()

	sha1Hash := sha1.New()
	sha256Hash := sha256.New()
	{
		r := io.TeeReader(file, sha1Hash)
		h.FileSize, err = io.Copy(sha256Hash, r)
		if err != nil {
			panic(err)
		}
	}

	h.Sha1Sum = fmt.Sprintf("%x", sha1Hash.Sum(nil))
	h.Sha256Sum = fmt.Sprintf("%x", sha256Hash.Sum(nil))

	var randBytes [32]byte
	_, err = rand.Read(randBytes[:])
	if err != nil {
		panic(err)
	}
	h.Basename = filepath.Base(filename)
	h.EntryURL = "/" + base64.RawURLEncoding.EncodeToString(randBytes[:])
	h.DownloadURL = h.EntryURL + "/" + h.Basename

	return h
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Must pass exactly one argument")
		os.Exit(1)
	}

	handler := makeHandler(os.Args[1])

	lis, err := net.ListenTCP("tcp", nil)
	if err != nil {
		panic(err)
	}

	fmt.Printf("http://%s:%d%s\n",
		OutboundIP(),
		lis.Addr().(*net.TCPAddr).Port,
		handler.EntryURL,
	)

	server := http.Server{
		Handler: handlers.LoggingHandler(os.Stderr, handler),
	}
	time.AfterFunc(time.Hour*24, func() {
		log.Println("Shutting down automatically after configured timeout")
		close(handler.Done)
	})
	go func() {
		<-handler.Done
		server.Shutdown(context.Background())
	}()

	server.Serve(lis)
}
