package main

import (
  "fmt"
  "net/http"
  "database/sql"
  "time"
  "html/template"
  "strconv"
  "path/filepath"
  "os"
  "log"
  "io/ioutil"
  "strings"
  _ "github.com/lib/pq"
)

type Event struct {
  Start string
  Stop string
  Title string
  Description string
}

func add_to_channel(channel []interface{}, event Event, max int) []interface{} {
  if (len(channel) >= max) {
    return channel
  }
  return append(channel, event)
}

func handler(w http.ResponseWriter, r *http.Request, config map[string]string, db *sql.DB) {
  err := db.Ping()
  if err != nil {
    internal_error_handler(w, r, err)
    return
  }

  channels := make(map[string][]interface{})

  rows, err := db.Query(`SELECT start, stop, title, channel, description
                         FROM tvguide
                         WHERE stop > now() AT TIME ZONE 'GMT'
                         ORDER BY channel, start`)
  if err != nil {
    internal_error_handler(w, r, err)
    return
  }

  tlayout := "15:04"
  llayout := "2006-01-02 15:04:05 -0700"
  num_events, err := strconv.ParseInt(r.FormValue("num"), 10, 0)
  if err != nil || num_events == 0 {
    num_events = 3
  }

  tzone := r.FormValue("zone")
  if tzone == "" {
    tzone = "GMT"
  }
  loc, err := time.LoadLocation(tzone)
  if err != nil {
    loc, _ = time.LoadLocation("GMT")
  }

  defer rows.Close()
  for rows.Next() {
    var start, stop time.Time
    var title, channel, description string
    if err := rows.Scan(&start,&stop,&title,&channel,&description); err != nil {
      internal_error_handler(w, r, err)
    }

    if description == "" {
      description = "(none)"
    }

    channels[channel] = add_to_channel(channels[channel], Event{
      Start: start.In(loc).Format(tlayout),
      Stop: stop.In(loc).Format(tlayout),
      Title: title,
      Description: description,
    }, int(num_events))
  }

  params := make(map[string]interface{})
  params["TimeZone"] = tzone
  params["CurrentTime"] = time.Now().In(loc).Format(llayout)
  params["Channels"] = channels

  if err := rows.Err(); err != nil {
    internal_error_handler(w, r, err)
    return
  }

  tmpl, err := template.ParseFiles("guide.html")
  if err != nil {
    internal_error_handler(w, r, err)
    return
  }

  tmpl.Execute(w, params)
}

func internal_error_handler(w http.ResponseWriter, r *http.Request, err error) {
  http.Error(w, fmt.Sprintf("Internal server error: %s", err.Error()), http.StatusInternalServerError)
}

func parse_config(filename string) (map[string]string, error) {
  config := make(map[string]string)

  content, err := ioutil.ReadFile(filename)
  if err != nil {
    return nil, err
  }

  lines := strings.Split(string(content), "\n")
  for _, line := range lines {
    c := strings.Split(line, "=")
    if (len(c) > 1) {
      key := strings.TrimSpace(c[0])
      value := strings.TrimSpace(c[1])
      config[key] = value
    }
  }

  return config, nil
}

func get_db_conn(config map[string]string) (*sql.DB, error) {
  connect_string := fmt.Sprintf("dbname=%s user=%s password=%s sslmode=disable",
                                config["dbName"],
                                config["dbUser"],
                                config["dbPass"])
  db, err := sql.Open("postgres", connect_string)
  if err != nil {
    return nil, err
  }
  return db, nil
}

func main() {
  bin_path, _ := filepath.Abs(filepath.Dir(os.Args[0]))
  conf_file := bin_path + "/../app.conf"
  if (len(os.Args) > 1) {
    conf_file = os.Args[1]
  }

  config, err := parse_config(conf_file)
  if err != nil {
    log.Fatal("Could not parse config: " + err.Error())
  }

  conn, err := get_db_conn(config)
  if err != nil {
    log.Fatal("Could not open DB: " + err.Error())
  }

  http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { handler(w, r, config, conn) })
  http.ListenAndServe(":8080", nil)
}
