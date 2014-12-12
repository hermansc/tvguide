#!/usr/bin/env python
import json
import urllib
from datetime import datetime, timedelta
import sys
import os
import psycopg2
import time
import pytz

VALID_CHANNELS = (
    'BBC One London',
    'BBC Two England',
    'ITV London',
    'Channel 4',
    'Channel 5',
    'ITV2'
    'BBC Three',
    'BBC Four',
    'ITV3',
    'Dave',
    'Film4',
    'BBC News Channel HD',
    'BBC Parliament',
    'Sky News'
)

def parse_config(filename):
    config = {}
    fh = open(filename, 'r')
    for line in fh:
        c = line.split("=")
        key = c[0].strip()
        value = c[1].strip()
        config[key] = value
    return config

def find_publisher(avail_from):
    for avail in avail_from:
        if avail["key"] in ("bbc.co.uk", "pressassociation.com"):
            return avail["key"]
    return None

def execute_db_insert(cur, programmes, channel_name):
    # Insert all found programmes, with a multi-insert.
    insert_vals = []
    if not programmes:
        print "No events in %s" % (channel_name)
        return
    for p in programmes:
      insert = (p["start"],p["stop"],p["title"],channel_name,p["description"])
      insert_vals.append(insert)
    args_str = ",".join(cur.mogrify("(%s,%s,%s,%s,%s)", x) for x in insert_vals)
    cur.execute("INSERT INTO " + config["dbTable"] + "(start,stop,title,channel,description) VALUES" + args_str)
    print "Inserted %d events to %s" % (len(programmes), channel_name)

def parse_transmission_to_gmt(t):
    # Input format: 2014-12-11T23:35:00Z
    # Output: datetime object with tzinfo set to GMT
    p = pytz.utc.localize(datetime.strptime(t, "%Y-%m-%dT%H:%M:%SZ"))
    return p

def run(config):
    BASE_URL="http://atlas.metabroadcast.com/3.0"
    FREEVIEW_KEY="cbhh"
    API_KEY=config["apiKey"]

    # Connect to DB.
    conn = psycopg2.connect("dbname=%s user=%s password=%s" % (config["dbName"], config["dbUser"], config["dbPass"]))
    db_cur = conn.cursor()

    # Get all channels
    CHANNEL_GROUPS_URL="%s/channel_groups/%s.json?annotations=channels&apiKey=%s" % (BASE_URL,
                                                                                 FREEVIEW_KEY,
                                                                                 API_KEY)
    channels_json = json.loads(urllib.urlopen(CHANNEL_GROUPS_URL).read())
    channels = channels_json["channel_groups"][0]["channels"]

    dates = [datetime.today().replace(hour=0,minute=0,second=0,microsecond=0) + timedelta(days=i) for i in range(0,int(config["fetchDays"]))]

    # Get schedule for ITV
    for channel in channels:
        # Array holding all programmes found for this channel (added to DB at the end)
        programmes = []

        # Channel encapsulates it's own channel inside itself.
        channel = channel["channel"]
        channel_name  = channel["title"]

        # Delete all existing data for this channel in epg-db.
        db_cur.execute("DELETE FROM " + config["dbTable"] + " WHERE channel=%s", (channel_name,))
        conn.commit()

        if not channel_name in VALID_CHANNELS:
            continue

        # Dict holding URL params
        params = {}

        # Ensure the channel is from a recongnized publisher
        params["publisher"] = find_publisher(channel["available_from"])
        if not params["publisher"]:
            print "Could not find publisher for channel '%s'. Defaulting to 'bbc.co.uk'" % channel_name
            params["publisher"] = "bbc.co.uk"

        # Fix dates
        today = dates[0]
        end_of_period = dates[-1] + timedelta(days=1)

        params["channel_id"] = channel["id"]
        params["from"] = today.isoformat() + ".000Z"
        params["to"] = end_of_period.isoformat() + ".000Z"
        params["annotations"] = "description,broadcasts,brand_summary"
        params["apiKey"] = API_KEY

        schedule_url = "%s/schedule.json?%s" % (BASE_URL, urllib.urlencode(params))
        schedule_json = json.loads(urllib.urlopen(schedule_url).read())

        schedule = schedule_json.get("schedule", None)
        if not schedule:
            print "Could not find schedule for channel '%s'. JSON:\n%s" % (channel_name,schedule_json)
            break

        items = schedule[0]["items"]
        for item in items:
            broadcasts = item["broadcasts"]

            # Sometimes more info is inside a container.
            title = item["title"]
            if item.get("container", None):
                if item["container"].get("title"):
                    title = item["container"]["title"]

            description = item.get("description", "")
            for broadcast in broadcasts:
                programmes.extend([{'start': parse_transmission_to_gmt(broadcast["transmission_time"]),
                                   'stop': parse_transmission_to_gmt(broadcast["transmission_end_time"]),
                                   'title': title.strip(),
                                   'description': description.strip()}])
        execute_db_insert(db_cur,programmes,channel_name)
        conn.commit()
        time.sleep(3)

if __name__ == "__main__":
    conf_file = os.path.dirname(os.path.realpath(__file__)) + "/../app.conf"
    if len(sys.argv) > 1:
        conf_file = sys.argv[1]
    config = parse_config(conf_file)
    run(config)
