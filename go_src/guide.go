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
  "regexp"
  _ "github.com/lib/pq"
)

type Event struct {
  Start string
  Stop string
  Title string
  Description string

  // Extra CSS classes
  Class string
}

func is_fav(regexes []*regexp.Regexp, title string) bool {
  for _, regex := range regexes {
    if(regex.MatchString(title)){
      return true
    }
  }
  return false
}

func get_regexes(db *sql.DB) ([]*regexp.Regexp, error) {
  // Create empty array of regexes
  regexes := make([]*regexp.Regexp, 0)

  rows, err := db.Query("SELECT regex FROM tvguide_favorites")
  if err != nil {
    return nil, err
  }

  defer rows.Close()
  for rows.Next() {
    var regex string
    rows.Scan(&regex)

    // Compile regexp, ignore if fails.
    rx, err := regexp.Compile(regex)
    if err != nil {
      fmt.Println(fmt.Sprintf("Warning: Could not compile: '%s'", regex))
      continue
    }

    // Append on success.
    regexes = append(regexes, rx)
  }
  return regexes, nil
}

func isRunning(start,stop time.Time, loc *time.Location) bool {
  return time.Now().In(loc).Before(stop) && time.Now().In(loc).After(start)
}

func noOpHandler(w http.ResponseWriter, r *http.Request) {
  /* Do nothing on e.g. favicon */
  return
}

func handler(w http.ResponseWriter, r *http.Request, config map[string]string, db *sql.DB) {
  err := db.Ping()
  if err != nil {
    internal_error_handler(w, r, err)
    return
  }

  channels := make(map[string][]interface{})
  upcoming := []map[string]string{}

  num_events, err := strconv.ParseInt(r.FormValue("num"), 10, 0)
  if err != nil || num_events == 0 {
    num_events = 3
  }

  rows, err := db.Query(`SELECT start, stop, title, channel, description
                         FROM (
                           SELECT 
                           start::TIMESTAMP WITH TIME ZONE, stop::TIMESTAMP WITH TIME ZONE, title, channel, description, rank() OVER (PARTITION BY channel ORDER BY stop)
                          FROM tvguide
                          WHERE stop::TIMESTAMP WITH TIME ZONE AT TIME ZONE 'GMT' > now() AT TIME ZONE 'GMT'
                        ) as sq
                        WHERE sq.rank <= $1 ORDER BY channel`, num_events)
  if err != nil {
    internal_error_handler(w, r, err)
    return
  }

  tlayout := "15:04"
  llayout := "2006-01-02 15:04:05 -0700"

  tzone := r.FormValue("zone")
  if tzone == "" {
    tzone = "GMT"
  }
  loc, err := time.LoadLocation(tzone)
  if err != nil {
    loc, _ = time.LoadLocation("GMT")
  }

  // Get regexes to check titles for favorites
  regexes, err := get_regexes(db)
  if err != nil {
    internal_error_handler(w, r, err)
    return
  }

  defer rows.Close()
  for rows.Next() {
    var start, stop time.Time
    var title, channel, description string
    if err := rows.Scan(&start,&stop,&title,&channel,&description); err != nil {
      internal_error_handler(w, r, err)
      return
    }

    if description == "" {
      description = "(none)"
    }

    // Ensure GMT/correct zone
    start = start.In(loc)
    stop = stop.In(loc)

    start_string := start.Format(tlayout)
    stop_string := stop.Format(tlayout)

    run_str := ""
    if (isRunning(start,stop,loc)){
      run_str = "running"
    }

    extra_classes := ""
    if (is_fav(regexes, title)) {
      extra_classes = "favorite"
      upcoming = append(upcoming, map[string]string{
        "Title": title,
        "Start": start_string,
        "Stop": stop_string,
        "Channel": channel,
        "Running": run_str,
      })
    }

    channels[channel] = append(channels[channel], Event{
      Start: start_string,
      Stop: stop_string,
      Title: title,
      Description: description,
      Class: extra_classes,
    })
  }

  params := make(map[string]interface{})
  params["TimeZone"] = tzone
  params["CurrentTime"] = time.Now().In(loc).Format(llayout)
  params["Channels"] = channels
  params["Upcoming"] = upcoming

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

  http.HandleFunc("/favicon.ico", noOpHandler)
  http.HandleFunc("/favicon.png", noOpHandler)
  http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
  http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { handler(w, r, config, conn) })

  err = http.ListenAndServe(":12300", nil)
  if err != nil {
    log.Fatal("Could not listen to port 12300/start server")
  }
}
