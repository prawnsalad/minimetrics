package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	goqu "github.com/doug-martin/goqu/v9"
	_ "github.com/mattn/go-sqlite3"
)

var logLevel = 2
var logError = 1
var logInfo = 2
var logDebug = 3
var logTrace = 4

var dbLock *sync.RWMutex

func out(level int, args ...interface{}) {
	if level <= logLevel {
		log.Println(args...)
	}
}

func main() {
	httpBind := "0.0.0.0:8070"
	statsdBind := "0.0.0.0:8125"
	dbFilename := "./metrics.db"

	ArgHttpdBind := flag.String("bind", httpBind, "The webserver bind address and port")
	ArgStatsdBind := flag.String("statsd", statsdBind, "The statsd bind address and port")
	ArgDbFilename := flag.String("db", dbFilename, `The sqlite database path. "memory" for in-memory`)

	flag.Parse()

	if *ArgHttpdBind != httpBind {
		httpBind = *ArgHttpdBind
	} else if os.Getenv("MM_BIND") != "" {
		httpBind = os.Getenv("MM_BIND")
	}

	if *ArgStatsdBind != statsdBind {
		statsdBind = *ArgStatsdBind
	} else if os.Getenv("MM_STATSD") != "" {
		statsdBind = os.Getenv("MM_STATSD")
	}

	if *ArgDbFilename != dbFilename {
		dbFilename = *ArgDbFilename
	} else if os.Getenv("MM_DB") != "" {
		dbFilename = os.Getenv("MM_DB")
	}

	db := initDb(dbFilename)
	metrics := metricsWriter(db)
	go trackInternalStats(metrics)
	go httpServer(httpBind, db, metrics)
	udpServerRunner(statsdBind, metrics)
}

func trackInternalStats(metrics chan string) {
	var m runtime.MemStats
	for {
		runtime.ReadMemStats(&m)
		metrics <- "minimetrics_mem:" + strconv.Itoa(int(m.Alloc)/1024) + "|g"

		metrics <- "minimetrics_routines:" + strconv.Itoa(runtime.NumGoroutine()) + "|g"

		time.Sleep(time.Second * 10)
	}

}

//go:embed public/*
var publicFiles embed.FS

func getPublicFs() (http.FileSystem, bool) {
	cwd, _ := os.Getwd()
	if _, err := os.Stat(cwd + "/public"); os.IsNotExist(err) {
		embedFs, _ := fs.Sub(publicFiles, "public")
		return http.FS(embedFs), false
	}

	return http.FS(os.DirFS("public")), true
}

func httpServer(bindStr string, db *sql.DB, metrics chan string) {
	fs, isOs := getPublicFs()
	if isOs {
		out(logInfo, "Serving public/ for public HTTP")
	}
	http.Handle("/", http.FileServer(fs))

	http.HandleFunc("/data/labels", func(w http.ResponseWriter, req *http.Request) {
		dbLock.RLock()
		queryStart := time.Now()
		rs, err := db.Query("SELECT label, type from labels")
		queryMs := time.Since(queryStart).Milliseconds()
		dbLock.RUnlock()

		if err != nil {
			out(logError, "Error reading labels:", err.Error())
			w.WriteHeader(503)
			return
		}

		type Row struct {
			Label string `json:"label"`
			Type  string `json:"type"`
		}
		defer rs.Close()
		rows := []Row{}
		for rs.Next() {
			row := Row{}
			rs.Scan(
				&row.Label,
				&row.Type,
			)
			rows = append(rows, row)
		}

		jsonBlob, _ := json.Marshal(rows)

		w.Header().Add("Content-Type", "application/json")
		fmt.Fprintf(w, string(jsonBlob))

		metrics <- "minimetrics_query,type=labels:" + fmt.Sprint(queryMs) + "|ms"
	})

	http.HandleFunc("/data/query/", func(w http.ResponseWriter, req *http.Request) {
		query := req.URL.Query()
		labelRaw := query.Get("label")
		labels := strings.Split(labelRaw, ",")

		groupBySec, _ := strconv.Atoi(query.Get("group"))
		if groupBySec == 0 {
			groupBySec = 10
		} else {
			groupBySec = int(groupBySec / 1000)
		}

		from, _ := strconv.Atoi(query.Get("from"))
		if from == 0 {
			from = int(time.Now().Add(time.Minute * -30).Unix())
		} else {
			from = int(from / 1000)
		}

		to, _ := strconv.Atoi(query.Get("to"))
		if to == 0 {
			to = int(time.Now().Unix())
		} else {
			to = int(to / 1000)
		}

		tags := make(map[string]string)
		for tagName, value := range query {
			if strings.HasPrefix(tagName, "tag_") {
				tags[tagName[4:]] = value[0]
			}
		}

		// Round down the from val to the lowest groupBySec group. This keeps the starting
		// point a clean multiple of groupBySec
		// eg. with groupBysec=10, from would be a clean multiple of 10seconds and then each
		//     point would be 10,20,30, etc. Without this we would get points like 12,22,32
		from = from - (from % groupBySec)

		qr := QueryRequest{}
		qr.From = from
		qr.To = to
		qr.GroupBySec = groupBySec
		qr.Labels = labels
		qr.Tags = tags

		queryStart := time.Now()
		result := queryMetrics(qr, db)
		queryMs := time.Since(queryStart).Milliseconds()
		metrics <- "minimetrics_query,type=metrics:" + fmt.Sprint(queryMs) + "|ms"

		var response = struct {
			Points []QueryResult       `json:"points"`
			Tags   map[string][]string `json:"tags"`
		}{
			Points: result,
			Tags:   make(map[string][]string),
		}

		// Get all the tags from each point and add each unique tag to an array
		for _, point := range result {
			for tagName, vals := range point.Tags {
				for _, val := range vals {
					if len(response.Tags[tagName]) < 100 && !contains(response.Tags[tagName], val) {
						response.Tags[tagName] = append(response.Tags[tagName], val)
					}
				}
			}
		}

		resultJsonBlob, _ := json.Marshal(response)

		w.Header().Add("Content-Type", "application/json")
		fmt.Fprintf(w, string(resultJsonBlob))
	})

	out(logInfo, "Web server listening", bindStr)
	http.ListenAndServe(bindStr, nil)
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

type QueryRequest struct {
	To         int
	From       int
	GroupBySec int
	Labels     []string
	Tags       map[string]string
}

type QueryResult struct {
	Bin   int                 `json:"bin"`
	Time  string              `json:"time"`
	Sum   int                 `json:"sum"`
	Count int                 `json:"count"`
	Avg   float64             `json:"avg"`
	Min   int                 `json:"min"`
	Max   int                 `json:"max"`
	Tags  map[string][]string `json:"-"`
}

func queryMetrics(request QueryRequest, db *sql.DB) []QueryResult {
	bins := []int{}
	for i := request.From; i <= request.To; i = i + request.GroupBySec {
		bins = append(bins, i)
	}
	binsJson, _ := json.Marshal(bins)

	data := goqu.From("counters")
	data = data.Select("*")
	data = data.Where(goqu.Ex{"counters.ts": goqu.Op{"gt": request.From}})
	data = data.Where(goqu.Ex{"counters.ts": goqu.Op{"lte": request.To}})
	data = data.Where(goqu.Ex{"counters.label": request.Labels})
	for tag, value := range request.Tags {
		data = data.Where(goqu.L("json_extract(tags, '$."+tag+"') = ?", value))
	}

	dataSql, _, _ := data.ToSQL()

	q := `
	WITH series AS (
		select value as time, value / :groupBySec as bin from json_each('` + string(binsJson) + `')
	),
	data AS (
		` + dataSql + `
	)
	SELECT
		bin,
		datetime(time, 'unixepoch') as time,
		COALESCE(sum(data.val), 0) as sum,
		COALESCE(count(data.val), 0) as count,
		COALESCE(avg(data.val), 0) as avg,
		COALESCE(min(data.val), 0) as min,
		COALESCE(max(data.val), 0) as max,
		COALESCE(GROUP_CONCAT(data.tags, '\n'), '') as tags
	FROM series
	LEFT JOIN data on series.bin = data.ts / :groupBySec
	GROUP BY label, bin
	ORDER BY bin
	`

	dbLock.RLock()
	defer dbLock.RUnlock()

	rs, err := db.Query(q, sql.Named("groupBySec", request.GroupBySec))
	if err != nil {
		log.Fatal("Metrics query error: " + err.Error())
	}

	res := []QueryResult{}
	defer rs.Close()
	for rs.Next() {
		bin := QueryResult{
			Tags: make(map[string][]string),
		}

		var rawTags string
		err := rs.Scan(
			&bin.Bin,
			&bin.Time,
			&bin.Sum,
			&bin.Count,
			&bin.Avg,
			&bin.Min,
			&bin.Max,
			&rawTags,
		)
		if err != nil {
			out(logError, "Error reading metrics data:", err.Error())
			return []QueryResult{}
		}

		tagLines := strings.Split(rawTags, "\\n")
		for _, jsonLine := range tagLines {
			if jsonLine == "" {
				continue
			}

			tags := make(map[string]string)
			err := json.Unmarshal([]byte(jsonLine), &tags)
			if err != nil {
				out(logError, "Error parsing tag data from database:", err.Error())
				continue
			}

			for tagName, value := range tags {
				bin.Tags[tagName] = append(bin.Tags[tagName], value)
			}
		}

		res = append(res, bin)
	}

	return res
}

func udpServerRunner(bindStr string, metrics chan string) {
	host, portStr, err := net.SplitHostPort(bindStr)
	port, _ := strconv.Atoi(portStr)

	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		Port: port,
		IP:   net.ParseIP(host),
	})
	if err != nil {
		panic(err)
	}

	defer conn.Close()
	out(logInfo, "Statsd server listening", conn.LocalAddr().String())

	receiving := false
	for {
		message := make([]byte, 512)
		rlen, remote, err := conn.ReadFromUDP(message[:])
		if err != nil {
			panic(err)
		}

		if !receiving {
			out(logInfo, "Receiving statsd data confirmed")
			receiving = true
		}

		data := strings.TrimSpace(string(message[:rlen]))
		out(logTrace, "Received: %s from %s\n", data, remote)
		metrics <- data
	}
}

func initDb(dbFilename string) *sql.DB {
	if dbFilename == "memory" {
		dbFilename = ":memory:"
	}

	db, err := sql.Open("sqlite3", dbFilename+"?cache=shared&_busy_timeout=30000")
	if err != nil {
		log.Fatal("Error opening database:", err.Error())
	}

	db.Exec("PRAGMA busy_timeout = 30000")
	db.Exec(`CREATE TABLE IF NOT EXISTS counters (
		ts integer,
		label text,
		type text,
		tags text,
		val integer
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_counters_label_ts ON counters (label, ts)`)
	db.Exec(`create index if not exists idx_counters_label_type on counters (label, type);`)
	db.Exec(`CREATE TABLE IF NOT EXISTS labels (
		label text,
		type text
	)`)
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_labels_label_type ON labels (label, type)`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS create_label_tbl INSERT ON counters 
	BEGIN
	  INSERT OR IGNORE into labels (label, type) VALUES (new.label, new.type);
	END;
	`)

	dbLock = &sync.RWMutex{}
	return db
}

func metricsWriter(db *sql.DB) chan string {
	in := make(chan string)
	go func() {
		inserted := 0
		lastReported := time.Now()
		lastInserted := time.Now()
		insertBuf := []interface{}{}
		insertBufRows := 0
		maxInsertSize := 5000

		for {
			line := <-in
			metric, isOk := parseStatsdLine(line)
			if !isOk {
				out(logError, "Unable to parse incoming statsd data: "+line)
				continue
			}

			// Insert the insert values into the buffer ready to be inserted in batch
			tagsBlob, _ := json.Marshal(metric.Tags)
			insertBuf = append(
				insertBuf,
				time.Now().Unix(), metric.Label, metric.Type, string(tagsBlob), metric.Value,
			)
			insertBufRows++

			// After 100ms, isnert everything we have in the batch
			if (insertBufRows > 0 && time.Now().Add(time.Millisecond*-100).UnixNano() > lastInserted.UnixNano()) || insertBufRows >= maxInsertSize {
				if insertBufRows >= maxInsertSize {
					out(logError, "Metrics write buffer filled. Possibly receiving too much too fast")
				}

				sql := "INSERT INTO counters(ts, label, type, tags, val) VALUES "
				for i := 0; i < insertBufRows; i++ {
					sql += "(?, ?, ?, ?, ?)"
					if i < insertBufRows-1 {
						sql += ", "
					}
				}

				//print("lock..")
				//dbLock.Lock()
				_, err := db.Exec(sql, insertBuf...)
				//dbLock.Unlock()
				//print(" unlocked.\n")
				if err != nil {
					out(logError, "Error saving metric:", err.Error())
				}

				lastInserted = time.Now()
				inserted += insertBufRows
				insertBuf = []interface{}{}
				insertBufRows = 0
			}

			// Report every 5 seconds
			if time.Now().Add(time.Second*-5).Unix() > lastReported.Unix() {
				out(logDebug, int(inserted/5), "metric inserts /sec")
				lastReported = time.Now()
				inserted = 0
			}
		}
	}()

	return in
}

type Metric struct {
	Label string
	Type  string
	Value int
	Tags  map[string]string
}

var reStatsd = regexp.MustCompile(`(?i)^(?P<label>[a-z0-9_\-.]+)(,(?P<tags>[^:\|]*))?(:(?P<value>-?\d+))?(\|(?P<type>[a-z]+))?$`)

func parseStatsdLine(line string) (Metric, bool) {
	groups := make(map[string]string)
	groupNames := reStatsd.SubexpNames()
	match := reStatsd.FindAllStringSubmatch(line, -1)

	if match == nil {
		return Metric{}, false
	}

	for _, match := range match {
		for groupIdx, group := range match {
			name := groupNames[groupIdx]
			if name != "" {
				groups[name] = group
			}
		}
	}

	m := Metric{}
	m.Label = groups["label"]
	m.Type = groups["type"]
	m.Value, _ = strconv.Atoi(groups["value"])
	m.Tags = make(map[string]string)

	for _, rawTag := range strings.Split(groups["tags"], ",") {
		tag := strings.SplitN(rawTag, "=", 2)
		if len(tag) == 2 {
			m.Tags[tag[0]] = tag[1]
		}
	}

	return m, true
}
