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

func is_fav(regexes [][]*regexp.Regexp, title, channel string) bool {
  for _, regex := range regexes {
    title_rx := regex[0]
    channel_rx := regex[1]
    if(title_rx.MatchString(title)){
      if(channel_rx.MatchString(channel)){
        return true
      }
    }
  }
  return false
}

func get_regexes(db *sql.DB) ([][]*regexp.Regexp, error) {
  // Create empty array of regexes
  regexes := make([][]*regexp.Regexp, 0)

  rows, err := db.Query("SELECT regex, channel_regex FROM tvguide_favorites")
  if err != nil {
    return nil, err
  }

  defer rows.Close()
  for rows.Next() {
    var title_regex, channel_regex string
    rows.Scan(&title_regex, &channel_regex)

    // Compile regexp, ignore if fails.
    title_rx, err := regexp.Compile(title_regex)
    if err != nil {
      fmt.Println(fmt.Sprintf("Warning: Could not compile: '%s'", title_regex))
      continue
    }

    // Compile regexp, ignore if fails.
    if channel_regex == "" {
      channel_regex = ".*"
    }
    channel_rx, err := regexp.Compile(channel_regex)
    if err != nil {
      fmt.Println(fmt.Sprintf("Warning: Could not compile: '%s'", channel_regex))
      continue
    }

    // Append on success.
    regexes = append(regexes, []*regexp.Regexp{title_rx, channel_rx})
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

func handler(w http.ResponseWriter, r *http.Request, config map[string]string, channel_groups map[string][]string, db *sql.DB) {
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

  chgroup := r.FormValue("chgroup")
  constraints := ""
  if (chgroup != "") {
    // Get string array of channels
    if channels, ok := channel_groups[chgroup]; ok {
      constraints = "AND channel IN (" + strings.Join(channels, ",") + ")"
    }
  }

  rows, err := db.Query(`SELECT start, stop, title, channel, description
                         FROM (
                           SELECT 
                           start::TIMESTAMP WITH TIME ZONE, stop::TIMESTAMP WITH TIME ZONE, title, channel, description, rank() OVER (PARTITION BY channel ORDER BY stop)
                          FROM tvguide
                          WHERE stop::TIMESTAMP WITH TIME ZONE AT TIME ZONE 'GMT' > now() AT TIME ZONE 'GMT'
                        ) as sq
                        WHERE sq.rank <= $1 ` + constraints + ` ORDER BY channel`, num_events)
  if err != nil {
    internal_error_handler(w, r, err)
    return
  }

  tlayout := "15:04"
  llayout := "2006-01-02 15:04:05 -0700"

  tzone := r.FormValue("zone")
  if tzone == "" {
    tzone = "Europe/London"
  }
  loc, err := time.LoadLocation(tzone)
  if err != nil {
    loc, _ = time.LoadLocation("Europe/London")
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
    if (is_fav(regexes, title, channel)) {
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
  params["ChannelGroups"] = channel_groups
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

func parse_channel_groups(filename string) (map[string][]string, error) {
  channel_groups := make(map[string][]string)

  content, err := ioutil.ReadFile(filename)
  if err != nil {
    return nil, err
  }

  lines := strings.Split(string(content), "\n")
  for _, line := range lines {
    c := strings.Split(line, "=")
    if (len(c) > 1) {
      key := strings.TrimSpace(c[0])
      channels := strings.Split(c[1], ",")
      for i, channel := range channels {
        // Channel name without space and with single quotes around.
        channels[i] = fmt.Sprintf("'%s'", strings.TrimSpace(channel))
      }
      channel_groups[key] = channels
    }
  }

  return channel_groups, nil
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

  // Read config file
  conf_file := bin_path + "/../app.conf"
  if (len(os.Args) > 1) {
    conf_file = os.Args[1]
  }
  config, err := parse_config(conf_file)
  if err != nil {
    log.Fatal("Could not parse config: " + err.Error())
  }

  // Get channel groups
  chgrp_file := bin_path + "/../channelgroups.conf"
  if (len(os.Args) > 2) {
    chgrp_file = os.Args[2]
  }
  channel_groups, err := parse_channel_groups(chgrp_file)
  if err != nil {
    log.Fatal("Could not parse channel group config: " + err.Error())
  }

  conn, err := get_db_conn(config)
  if err != nil {
    log.Fatal("Could not open DB: " + err.Error())
  }

  http.HandleFunc("/favicon.ico", noOpHandler)
  http.HandleFunc("/favicon.png", noOpHandler)
  http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
  http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { handler(w, r, config, channel_groups, conn) })

  err = http.ListenAndServe(":12300", nil)
  if err != nil {
    log.Fatal("Could not listen to port 12300/start server")
  }
}
